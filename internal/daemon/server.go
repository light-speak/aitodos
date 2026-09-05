package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/buildinfo"
	"github.com/light-speak/aitodos/internal/capabilitycatalog"
	"github.com/light-speak/aitodos/internal/gitworkflow"
	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/recovery"
	"github.com/light-speak/aitodos/internal/scheduler"
	"github.com/light-speak/aitodos/internal/storage"
	"github.com/light-speak/aitodos/internal/transport/httpapi"
	"github.com/light-speak/aitodos/internal/webui"
)

// Serve 启动当前项目的 loopback HTTP 服务并阻塞到上下文取消。
func Serve(
	ctx context.Context,
	currentProject *project.Project,
	nonce string,
	port int,
	onReady func(Metadata),
) error {
	lock, err := acquireFileLock(currentProject.Paths.DaemonLock)
	if err != nil {
		return err
	}
	defer lock.Close()

	database, err := storage.OpenExisting(ctx, currentProject.Paths.Database)
	if err != nil {
		return fmt.Errorf("open project database: %w", err)
	}
	defer database.Close()
	if err := gitworkflow.New(currentProject, database).RecoverIntegrations(ctx); err != nil {
		return fmt.Errorf("recover task integrations: %w", err)
	}
	if err := recovery.New(currentProject, database).Start(ctx); err != nil {
		return fmt.Errorf("recover project runs: %w", err)
	}
	projectScheduler := scheduler.New(currentProject, database)
	go projectScheduler.Run(ctx)

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", address, err)
	}
	defer listener.Close()

	boundPort := listener.Addr().(*net.TCPAddr).Port
	metadata := Metadata{
		PID:               os.Getpid(),
		Port:              boundPort,
		URL:               "http://127.0.0.1:" + strconv.Itoa(boundPort),
		ProjectInstanceID: currentProject.InstanceID,
		Nonce:             nonce,
		StartedAt:         time.Now().UTC(),
	}
	if err := writeMetadata(currentProject.Paths.DaemonState, metadata); err != nil {
		return err
	}
	defer removeCurrentMetadata(currentProject, nonce)

	handler, err := newHandler(currentProject, metadata, database, projectScheduler)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.Serve(listener)
	}()
	if onReady != nil {
		onReady(metadata)
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown project server: %w", err)
		}
		err := <-serverErrors
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve project: %w", err)
		}
		return nil
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve project: %w", err)
		}
		return nil
	}
}

func newHandler(currentProject *project.Project, metadata Metadata, database *sql.DB, projectScheduler *scheduler.Scheduler) (http.Handler, error) {
	mux := http.NewServeMux()
	discussionStore := storage.NewDiscussionStore(database)
	relationStore := storage.NewRelationStore(database)
	gitManager := gitworkflow.New(currentProject, database)
	httpapi.RegisterTopicRoutes(mux, storage.NewTopicStore(database), discussionStore, relationStore)
	httpapi.RegisterObjectiveRoutes(mux, storage.NewObjectiveStore(database))
	httpapi.RegisterPlanRoutes(mux, storage.NewPlanStore(database))
	taskStore := storage.NewTaskStore(database)
	assessmentStore := storage.NewAssessmentStore(database)
	httpapi.RegisterTaskRoutes(mux, taskStore, discussionStore, relationStore, assessmentStore, storage.NewTaskFeedbackStore(database), gitManager)
	httpapi.RegisterAssessmentRoutes(mux, taskStore, assessmentStore)
	httpapi.RegisterArtifactRoutes(mux, storage.NewArtifactStore(database, currentProject.Paths.Artifacts))
	httpapi.RegisterGitWorkflowRoutes(mux, gitManager)
	httpapi.RegisterProjectRoutes(mux, currentProject, func() httpapi.SchedulerStatus {
		status := projectScheduler.Health()
		return httpapi.SchedulerStatus{ConsecutiveFailures: status.ConsecutiveFailures, LastError: status.LastError}
	})
	httpapi.RegisterAgentProfileRoutes(mux, storage.NewAgentProfileStore(database))
	httpapi.RegisterCapabilityRoutes(mux, capabilitycatalog.New(
		currentProject.Root, "codex", storage.NewCapabilityStore(database),
	))
	httpapi.RegisterQualityRoutes(mux, storage.NewQualityStore(database))
	httpapi.RegisterClarificationRoutes(mux, storage.NewClarificationStore(database))
	searchStore := storage.NewSearchStore(database)
	httpapi.RegisterSearchRoutes(mux, searchStore)
	httpapi.RegisterRetrievalEvalRoutes(mux, storage.NewRetrievalEvalStore(database, searchStore))
	httpapi.RegisterMCPRoutes(mux, storage.NewMCPAuditStore(database))
	httpapi.RegisterKnowledgeRoutes(mux, storage.NewKnowledgeStore(database))
	httpapi.RegisterExperienceRoutes(mux, storage.NewExperienceStore(database))
	runStore := storage.NewRunStore(database)
	httpapi.RegisterRunRoutes(mux, runStore, currentProject.Paths.Artifacts)
	httpapi.RegisterApprovalRoutes(mux, runStore)
	mux.HandleFunc("GET /api/health", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(Health{
			Status:            "ok",
			ProjectInstanceID: currentProject.InstanceID,
			Nonce:             metadata.Nonce,
			PID:               metadata.PID,
			Version:           buildinfo.Version,
			Commit:            buildinfo.Commit,
		})
	})
	uiHandler, err := webui.NewHandler()
	if err != nil {
		return nil, err
	}
	mux.Handle("GET /", uiHandler)
	return localRequestGuard(mux), nil
}

func localRequestGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !isLoopbackHost(request.Host) || !isAllowedBrowserRequest(request) {
			response.Header().Set("Content-Type", "application/json; charset=utf-8")
			response.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(response).Encode(map[string]any{
				"error": map[string]string{"code": "LOCAL_REQUEST_FORBIDDEN", "message": "仅允许当前本地页面访问"},
			})
			return
		}
		next.ServeHTTP(response, request)
	})
}

func isLoopbackHost(value string) bool {
	host := value
	if parsed, _, err := net.SplitHostPort(value); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func isAllowedBrowserRequest(request *http.Request) bool {
	if request.Method == http.MethodGet || request.Method == http.MethodHead || request.Method == http.MethodOptions {
		return true
	}
	if strings.EqualFold(request.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Scheme == "http" && strings.EqualFold(parsed.Host, request.Host)
}
