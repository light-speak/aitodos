package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	domainrun "github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/storage"
)

const maxRunLogReadBytes = 8 << 20

type runHandler struct {
	store        *storage.RunStore
	artifactRoot string
}

type runDetail struct {
	Run               domainrun.Run                `json:"run"`
	Usage             *domainrun.Usage             `json:"usage,omitempty"`
	Artifacts         []domainrun.Artifact         `json:"artifacts"`
	WorkspaceSnapshot *domainrun.WorkspaceSnapshot `json:"workspace_snapshot,omitempty"`
}

type runLog struct {
	Stream    string `json:"stream"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
}

type runPage struct {
	Items      []domainrun.Run `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type runCursor struct {
	CreatedAt string `json:"created_at"`
	RunID     string `json:"run_id"`
}

// RegisterRunRoutes 注册 Run 历史、日志和实际用量等只读可观测性端点。
func RegisterRunRoutes(mux *http.ServeMux, store *storage.RunStore, artifactRoot string) {
	handler := &runHandler{store: store, artifactRoot: artifactRoot}
	mux.HandleFunc("GET /api/runs/usage", handler.usageSummary)
	mux.HandleFunc("GET /api/runs", handler.list)
	mux.HandleFunc("GET /api/tasks/{taskID}/runs", handler.listTaskRuns)
	mux.HandleFunc("GET /api/runs/{runID}", handler.detail)
	mux.HandleFunc("GET /api/runs/{runID}/logs", handler.log)
	mux.HandleFunc("GET /api/runs/{runID}/events", handler.events)
	mux.HandleFunc("POST /api/runs/{runID}/cancel", handler.cancel)
}

func (handler *runHandler) list(response http.ResponseWriter, request *http.Request) {
	query, err := parseRunQuery(request)
	if err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_RUN_QUERY", err.Error())
		return
	}
	page, err := handler.store.Query(request.Context(), query)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "RUN_LIST_FAILED", "读取 Run 列表失败")
		return
	}
	result := runPage{Items: page.Items}
	if page.HasMore && len(page.Items) > 0 {
		result.NextCursor, err = encodeRunCursor(page.Items[len(page.Items)-1])
		if err != nil {
			writeError(response, http.StatusInternalServerError, "RUN_CURSOR_FAILED", "创建 Run 分页游标失败")
			return
		}
	}
	writeJSON(response, http.StatusOK, result)
}

func parseRunQuery(request *http.Request) (storage.RunQuery, error) {
	values := request.URL.Query()
	query := storage.RunQuery{TaskID: values.Get("task_id"), TopicID: values.Get("topic_id"), Limit: 50}
	if text := values.Get("limit"); text != "" {
		limit, err := strconv.Atoi(text)
		if err != nil || limit < 1 || limit > 100 {
			return storage.RunQuery{}, errors.New("limit 必须是 1 到 100")
		}
		query.Limit = limit
	}
	if text := values.Get("active"); text != "" {
		active, err := strconv.ParseBool(text)
		if err != nil {
			return storage.RunQuery{}, errors.New("active 必须是 true 或 false")
		}
		query.ActiveOnly = active
	}
	if text := values.Get("status"); text != "" {
		if query.ActiveOnly {
			return storage.RunQuery{}, errors.New("active 不能和 status 同时使用")
		}
		query.Status = domainrun.Status(text)
		if !validRunStatus(query.Status) {
			return storage.RunQuery{}, errors.New("status 无效")
		}
	}
	if text := values.Get("purpose"); text != "" {
		query.Purpose = domainrun.Purpose(text)
		if !validRunPurpose(query.Purpose) {
			return storage.RunQuery{}, errors.New("purpose 无效")
		}
	}
	if text := values.Get("cursor"); text != "" {
		cursor, err := decodeRunCursor(text)
		if err != nil {
			return storage.RunQuery{}, errors.New("cursor 无效")
		}
		query.BeforeTime = cursor.CreatedAt
		query.BeforeRunID = cursor.RunID
	}
	return query, nil
}

func encodeRunCursor(current domainrun.Run) (string, error) {
	content, err := json.Marshal(runCursor{CreatedAt: current.CreatedAt.Format(time.RFC3339Nano), RunID: current.ID})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(content), nil
}

func decodeRunCursor(value string) (struct {
	CreatedAt time.Time
	RunID     string
}, error) {
	var result struct {
		CreatedAt time.Time
		RunID     string
	}
	content, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return result, err
	}
	var cursor runCursor
	if err := json.Unmarshal(content, &cursor); err != nil || cursor.RunID == "" {
		return result, errors.New("invalid cursor")
	}
	result.CreatedAt, err = time.Parse(time.RFC3339Nano, cursor.CreatedAt)
	result.RunID = cursor.RunID
	return result, err
}

func validRunStatus(status domainrun.Status) bool {
	switch status {
	case domainrun.StatusClaimed, domainrun.StatusStarting, domainrun.StatusRunning,
		domainrun.StatusFinalizing, domainrun.StatusNeedsInput, domainrun.StatusSucceeded,
		domainrun.StatusFailed, domainrun.StatusCancelled, domainrun.StatusTimedOut, domainrun.StatusLost:
		return true
	default:
		return false
	}
}

func validRunPurpose(purpose domainrun.Purpose) bool {
	switch purpose {
	case domainrun.PurposePlanning, domainrun.PurposeTriage, domainrun.PurposeImplementation,
		domainrun.PurposeRevision, domainrun.PurposeReview:
		return true
	default:
		return false
	}
}

func (handler *runHandler) cancel(response http.ResponseWriter, request *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "取消请求格式无效")
		return
	}
	if len([]rune(strings.TrimSpace(body.Reason))) > 1000 {
		writeError(response, http.StatusBadRequest, "INVALID_CANCEL_REASON", "取消原因不能超过 1000 个字符")
		return
	}
	current, err := handler.store.RequestCancel(request.Context(), request.PathValue("runID"), body.Reason)
	if err != nil {
		if errors.Is(err, storage.ErrRunNotFound) {
			writeError(response, http.StatusNotFound, "RUN_NOT_FOUND", "Run 不存在")
			return
		}
		if errors.Is(err, storage.ErrRunStateConflict) {
			writeError(response, http.StatusConflict, "RUN_CANCEL_TOO_LATE", "Run 已进入收尾或终态，不能再取消")
			return
		}
		writeError(response, http.StatusInternalServerError, "RUN_CANCEL_FAILED", "请求取消 Run 失败")
		return
	}
	writeJSON(response, http.StatusAccepted, current)
}

func (handler *runHandler) events(response http.ResponseWriter, request *http.Request) {
	after, err := runEventCursor(request)
	if err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_EVENT_CURSOR", err.Error())
		return
	}
	runID := request.PathValue("runID")
	if _, err := handler.store.Get(request.Context(), runID); err != nil {
		writeRunReadError(response, err)
		return
	}
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, http.StatusInternalServerError, "SSE_UNSUPPORTED", "当前 HTTP Writer 不支持 SSE")
		return
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache")
	response.Header().Set("X-Accel-Buffering", "no")
	_, _ = io.WriteString(response, "retry: 1000\n\n")
	flusher.Flush()

	poll := time.NewTicker(250 * time.Millisecond)
	heartbeat := time.NewTicker(15 * time.Second)
	defer poll.Stop()
	defer heartbeat.Stop()
	for {
		emitted, streamErr := handler.emitRunEvents(request, response, flusher, runID, &after)
		if streamErr != nil {
			return
		}
		current, getErr := handler.store.Get(request.Context(), runID)
		if getErr != nil || (isTerminalRunStatus(current.Status) && !emitted) {
			return
		}
		select {
		case <-request.Context().Done():
			return
		case <-poll.C:
		case <-heartbeat.C:
			_, _ = io.WriteString(response, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (handler *runHandler) emitRunEvents(
	request *http.Request,
	response http.ResponseWriter,
	flusher http.Flusher,
	runID string,
	after *int64,
) (bool, error) {
	events, err := handler.store.ListEvents(request.Context(), runID, *after, 100)
	if err != nil {
		return false, err
	}
	for _, event := range events {
		encoded, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return false, marshalErr
		}
		if _, writeErr := fmt.Fprintf(response, "id: %d\ndata: %s\n\n", event.Sequence, encoded); writeErr != nil {
			return false, writeErr
		}
		*after = event.Sequence
	}
	if len(events) > 0 {
		flusher.Flush()
	}
	return len(events) > 0, nil
}

func runEventCursor(request *http.Request) (int64, error) {
	value := request.URL.Query().Get("after")
	if value == "" {
		value = request.Header.Get("Last-Event-ID")
	}
	if value == "" {
		return 0, nil
	}
	after, err := strconv.ParseInt(value, 10, 64)
	if err != nil || after < 0 {
		return 0, errors.New("after 或 Last-Event-ID 必须是非负整数")
	}
	return after, nil
}

func isTerminalRunStatus(status domainrun.Status) bool {
	return status == domainrun.StatusNeedsInput || status == domainrun.StatusSucceeded ||
		status == domainrun.StatusFailed || status == domainrun.StatusCancelled ||
		status == domainrun.StatusTimedOut || status == domainrun.StatusLost
}

func (handler *runHandler) listTaskRuns(response http.ResponseWriter, request *http.Request) {
	runs, err := handler.store.ListTaskRuns(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeError(response, http.StatusInternalServerError, "RUN_LIST_FAILED", "读取 Task Run 历史失败")
		return
	}
	writeJSON(response, http.StatusOK, runs)
}

func (handler *runHandler) detail(response http.ResponseWriter, request *http.Request) {
	current, err := handler.store.Get(request.Context(), request.PathValue("runID"))
	if err != nil {
		writeRunReadError(response, err)
		return
	}
	artifacts, err := handler.store.ListArtifacts(request.Context(), current.ID)
	if err != nil {
		writeRunReadError(response, err)
		return
	}
	result := runDetail{Run: current, Artifacts: artifacts}
	if usage, usageErr := handler.store.GetUsage(request.Context(), current.ID); usageErr == nil {
		result.Usage = &usage
	} else if !errors.Is(usageErr, storage.ErrRunUsageNotFound) {
		writeRunReadError(response, usageErr)
		return
	}
	if snapshot, snapshotErr := handler.store.GetWorkspaceSnapshot(request.Context(), current.ID); snapshotErr == nil {
		result.WorkspaceSnapshot = &snapshot
	} else if !errors.Is(snapshotErr, storage.ErrRunWorkspaceSnapshotNotFound) {
		writeRunReadError(response, snapshotErr)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (handler *runHandler) log(response http.ResponseWriter, request *http.Request) {
	stream := strings.ToLower(request.URL.Query().Get("stream"))
	kind := map[string]string{"stdout": "STDOUT", "stderr": "STDERR"}[stream]
	if kind == "" {
		writeError(response, http.StatusBadRequest, "INVALID_LOG_STREAM", "stream 必须是 stdout 或 stderr")
		return
	}
	artifact, err := handler.store.GetArtifact(request.Context(), request.PathValue("runID"), kind)
	if err != nil {
		writeRunReadError(response, err)
		return
	}
	content, err := readManagedRunArtifact(handler.artifactRoot, artifact)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "RUN_LOG_READ_FAILED", "Run 日志文件缺失或校验失败")
		return
	}
	writeJSON(response, http.StatusOK, runLog{Stream: stream, Content: string(content), Size: artifact.Size, Truncated: artifact.Truncated})
}

func readManagedRunArtifact(root string, artifact domainrun.Artifact) ([]byte, error) {
	path := filepath.Join(root, filepath.FromSlash(artifact.RelativePath))
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("run artifact path escapes managed root")
	}
	if artifact.Size < 0 || artifact.Size > maxRunLogReadBytes {
		return nil, errors.New("run artifact is too large for inline reading")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxRunLogReadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read run artifact: %w", err)
	}
	if int64(len(content)) != artifact.Size {
		return nil, errors.New("run artifact size mismatch")
	}
	hash := sha256.Sum256(content)
	if hex.EncodeToString(hash[:]) != artifact.SHA256 {
		return nil, errors.New("run artifact hash mismatch")
	}
	return content, nil
}

func writeRunReadError(response http.ResponseWriter, err error) {
	if errors.Is(err, storage.ErrRunNotFound) || errors.Is(err, storage.ErrRunArtifactNotFound) {
		writeError(response, http.StatusNotFound, "RUN_NOT_FOUND", "Run 或日志不存在")
		return
	}
	writeError(response, http.StatusInternalServerError, "RUN_READ_FAILED", "读取 Run 信息失败")
}

func (handler *runHandler) usageSummary(response http.ResponseWriter, request *http.Request) {
	summary, err := handler.store.UsageSummary(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "RUN_USAGE_READ_FAILED", "读取 Run 用量失败")
		return
	}
	writeJSON(response, http.StatusOK, summary)
}
