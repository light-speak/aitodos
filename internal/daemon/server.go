package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

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

	handler, err := newHandler(currentProject, metadata, database)
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

func newHandler(currentProject *project.Project, metadata Metadata, database *sql.DB) (http.Handler, error) {
	mux := http.NewServeMux()
	discussionStore := storage.NewDiscussionStore(database)
	relationStore := storage.NewRelationStore(database)
	gitManager := gitworkflow.New(currentProject, database)
	httpapi.RegisterTopicRoutes(mux, storage.NewTopicStore(database), discussionStore, relationStore)
	httpapi.RegisterPlanRoutes(mux, storage.NewPlanStore(database))
	taskStore := storage.NewTaskStore(database)
	assessmentStore := storage.NewAssessmentStore(database)
	httpapi.RegisterTaskRoutes(mux, taskStore, discussionStore, relationStore, assessmentStore, storage.NewTaskFeedbackStore(database), gitManager)
	httpapi.RegisterAssessmentRoutes(mux, taskStore, assessmentStore)
	httpapi.RegisterArtifactRoutes(mux, storage.NewArtifactStore(database, currentProject.Paths.Artifacts))
	httpapi.RegisterGitWorkflowRoutes(mux, gitManager)
	httpapi.RegisterProjectRoutes(mux, currentProject)
	httpapi.RegisterAgentProfileRoutes(mux, storage.NewAgentProfileStore(database))
	httpapi.RegisterCapabilityRoutes(mux, capabilitycatalog.New(
		currentProject.Root, "codex", storage.NewCapabilityStore(database),
	))
	httpapi.RegisterQualityRoutes(mux, storage.NewQualityStore(database))
	httpapi.RegisterClarificationRoutes(mux, storage.NewClarificationStore(database))
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
		})
	})
	uiHandler, err := webui.NewHandler()
	if err != nil {
		return nil, err
	}
	mux.Handle("GET /", uiHandler)
	return mux, nil
}
