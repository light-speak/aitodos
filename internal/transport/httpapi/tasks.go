// Package httpapi 提供项目本地 Daemon 的 REST 接口。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/domain/assessment"
	"github.com/light-speak/aitodos/internal/domain/discussion"
	"github.com/light-speak/aitodos/internal/domain/relation"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/taskfeedback"
	"github.com/light-speak/aitodos/internal/storage"
)

const maxRequestBodyBytes = 1 << 20

type taskHandler struct {
	store       *storage.TaskStore
	discussion  *storage.DiscussionStore
	relations   *storage.RelationStore
	assessments *storage.AssessmentStore
	feedback    *storage.TaskFeedbackStore
	branches    TargetBranchValidator
}

// TargetBranchValidator 校验 Task 创建时选择的本地目标分支。
type TargetBranchValidator interface {
	ValidateTargetBranch(context.Context, string) error
}

type createTaskRequest struct {
	Title              string `json:"title"`
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptance_criteria"`
	Priority           *int   `json:"priority"`
	TargetBranch       string `json:"target_branch,omitempty"`
}

type updateTaskTitleRequest struct {
	Title           string `json:"title"`
	ExpectedVersion int64  `json:"expected_version"`
}

type updateTaskDetailsRequest struct {
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptance_criteria"`
	Priority           int    `json:"priority"`
	ExpectedVersion    int64  `json:"expected_version"`
}

type retryTaskRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}

type taskFeedbackRequest struct {
	Intent              string   `json:"intent"`
	Content             string   `json:"content"`
	LinkedTaskIDs       []string `json:"linked_task_ids,omitempty"`
	ExpectedTaskVersion int64    `json:"expected_task_version"`
}

type taskFeedbackResponse struct {
	Message      discussion.Message     `json:"message"`
	Feedback     *taskfeedback.Feedback `json:"feedback,omitempty"`
	Task         *task.Task             `json:"task,omitempty"`
	FollowUpTask *task.Task             `json:"follow_up_task,omitempty"`
}

type taskListItem struct {
	task.Task
	Assessment      *assessment.Assessment `json:"assessment,omitempty"`
	AssessmentStale bool                   `json:"assessment_stale"`
}

type errorEnvelope struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RegisterTaskRoutes 注册 Task 查询和领域命令端点。
func RegisterTaskRoutes(
	mux *http.ServeMux,
	store *storage.TaskStore,
	discussionStore *storage.DiscussionStore,
	relationStore *storage.RelationStore,
	assessmentStore *storage.AssessmentStore,
	feedbackStore *storage.TaskFeedbackStore,
	branchValidator TargetBranchValidator,
) {
	handler := &taskHandler{
		store: store, discussion: discussionStore, relations: relationStore,
		assessments: assessmentStore, feedback: feedbackStore, branches: branchValidator,
	}
	mux.HandleFunc("POST /api/tasks", handler.create)
	mux.HandleFunc("GET /api/tasks", handler.list)
	mux.HandleFunc("GET /api/tasks/{taskID}", handler.get)
	mux.HandleFunc("PUT /api/tasks/{taskID}/title", handler.updateTitle)
	mux.HandleFunc("PUT /api/tasks/{taskID}/details", handler.updateDetails)
	mux.HandleFunc("POST /api/tasks/{taskID}/cancel", handler.cancelTask)
	mux.HandleFunc("POST /api/tasks/{taskID}/archive", handler.archiveTask)
	mux.HandleFunc("POST /api/tasks/{taskID}/retry", handler.retry)
	mux.HandleFunc("GET /api/tasks/{taskID}/messages", handler.listMessages)
	mux.HandleFunc("POST /api/tasks/{taskID}/messages", handler.createMessage)
	mux.HandleFunc("POST /api/tasks/{taskID}/feedback", handler.createFeedback)
	mux.HandleFunc("GET /api/tasks/{taskID}/feedback", handler.listFeedback)
	mux.HandleFunc("GET /api/tasks/{taskID}/feedback/events", handler.feedbackEvents)
	mux.HandleFunc("POST /api/task-feedback/{feedbackID}/retry", handler.retryFeedback)
	mux.HandleFunc("GET /api/tasks/{taskID}/relations", handler.listRelations)
	mux.HandleFunc("POST /api/tasks/{taskID}/relations", handler.createRelation)
	mux.HandleFunc("DELETE /api/tasks/{taskID}/relations/{relatedTaskID}", handler.deleteRelation)
	mux.HandleFunc("GET /api/tasks/{taskID}/topics", handler.listTopics)
	mux.HandleFunc("POST /api/tasks/{taskID}/topics", handler.createTopicRelation)
	mux.HandleFunc("DELETE /api/tasks/{taskID}/topics/{topicID}", handler.deleteTopicRelation)
}

func (handler *taskHandler) listFeedback(response http.ResponseWriter, request *http.Request) {
	items, err := handler.feedback.ListTask(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeTaskError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (handler *taskHandler) retryFeedback(response http.ResponseWriter, request *http.Request) {
	created, err := handler.feedback.Retry(request.Context(), request.PathValue("feedbackID"))
	if err != nil {
		writeTaskError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (handler *taskHandler) feedbackEvents(response http.ResponseWriter, request *http.Request) {
	after, err := runEventCursor(request)
	if err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_EVENT_CURSOR", err.Error())
		return
	}
	taskID := request.PathValue("taskID")
	if _, err := handler.store.Get(request.Context(), taskID); err != nil {
		writeTaskError(response, err)
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
	handler.streamFeedbackEvents(response, request, flusher, taskID, after)
}

func (handler *taskHandler) streamFeedbackEvents(
	response http.ResponseWriter,
	request *http.Request,
	flusher http.Flusher,
	taskID string,
	after int64,
) {
	poll := time.NewTicker(250 * time.Millisecond)
	heartbeat := time.NewTicker(15 * time.Second)
	defer poll.Stop()
	defer heartbeat.Stop()
	for {
		if err := handler.emitFeedbackEvents(request, response, flusher, taskID, &after); err != nil {
			return
		}
		pending, err := handler.feedback.HasPendingTask(request.Context(), taskID)
		if err != nil || !pending {
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

func (handler *taskHandler) emitFeedbackEvents(
	request *http.Request,
	response http.ResponseWriter,
	flusher http.Flusher,
	taskID string,
	after *int64,
) error {
	events, err := handler.feedback.ListEvents(request.Context(), taskID, *after, 100)
	if err != nil {
		return err
	}
	for _, event := range events {
		encoded, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return marshalErr
		}
		if _, writeErr := fmt.Fprintf(response, "id: %d\nevent: task-feedback\ndata: %s\n\n", event.Sequence, encoded); writeErr != nil {
			return writeErr
		}
		*after = event.Sequence
	}
	if len(events) > 0 {
		flusher.Flush()
	}
	return nil
}

func (handler *taskHandler) createFeedback(response http.ResponseWriter, request *http.Request) {
	var body taskFeedbackRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 Task 反馈")
		return
	}
	input := discussion.CreateMessageInput{Content: body.Content, LinkedTaskIDs: body.LinkedTaskIDs}.Normalized()
	if err := input.Validate(); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_MESSAGE", "反馈内容不能为空，且最多关联 20 个 Task")
		return
	}
	taskID := request.PathValue("taskID")
	switch body.Intent {
	case "NOTE":
		message, err := handler.discussion.AppendTaskMessage(request.Context(), taskID, input)
		if err != nil {
			writeTaskError(response, err)
			return
		}
		writeJSON(response, http.StatusCreated, taskFeedbackResponse{Message: message})
	case string(taskfeedback.IntentDiscuss):
		message, feedback, err := handler.feedback.Discuss(request.Context(), taskID, input)
		if err != nil {
			writeTaskError(response, err)
			return
		}
		writeJSON(response, http.StatusCreated, taskFeedbackResponse{Message: message, Feedback: &feedback})
	case string(taskfeedback.IntentRequestChanges):
		message, feedback, updated, followUp, err := handler.feedback.RequestChanges(
			request.Context(), taskID, body.ExpectedTaskVersion, input,
		)
		if err != nil {
			writeTaskError(response, err)
			return
		}
		writeJSON(response, http.StatusCreated, taskFeedbackResponse{
			Message: message, Feedback: &feedback, Task: &updated, FollowUpTask: followUp,
		})
	default:
		writeError(response, http.StatusBadRequest, "INVALID_FEEDBACK_INTENT", "请选择询问 Agent、要求修改或仅记录")
	}
}

func (handler *taskHandler) retry(response http.ResponseWriter, request *http.Request) {
	var body retryTaskRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "重新排队请求格式无效")
		return
	}
	updated, err := handler.store.RetryBlocked(request.Context(), request.PathValue("taskID"), body.ExpectedVersion)
	if err != nil {
		writeTaskError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, updated)
}

func (handler *taskHandler) updateTitle(response http.ResponseWriter, request *http.Request) {
	var body updateTaskTitleRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 Task 标题")
		return
	}
	updated, err := handler.store.UpdateTitle(request.Context(), request.PathValue("taskID"), body.ExpectedVersion, task.UpdateTitleInput{Title: body.Title})
	if err != nil {
		writeTaskError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, updated)
}

func (handler *taskHandler) updateDetails(response http.ResponseWriter, request *http.Request) {
	var body updateTaskDetailsRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "Task 修改请求无效")
		return
	}
	updated, err := handler.store.UpdateDetails(request.Context(), request.PathValue("taskID"), body.ExpectedVersion, task.UpdateDetailsInput{
		Description: body.Description, AcceptanceCriteria: body.AcceptanceCriteria, Priority: body.Priority,
	})
	if err != nil {
		writeTaskError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, updated)
}

func (handler *taskHandler) cancelTask(response http.ResponseWriter, request *http.Request) {
	var body retryTaskRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "Task 取消请求无效")
		return
	}
	updated, err := handler.store.ApplyCommand(request.Context(), request.PathValue("taskID"), body.ExpectedVersion, task.CommandCancelTask)
	if err != nil {
		writeTaskError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, updated)
}

func (handler *taskHandler) archiveTask(response http.ResponseWriter, request *http.Request) {
	var body retryTaskRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "Task 归档请求无效")
		return
	}
	updated, err := handler.store.Archive(request.Context(), request.PathValue("taskID"), body.ExpectedVersion)
	if err != nil {
		writeTaskError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, updated)
}

func (handler *taskHandler) listTopics(response http.ResponseWriter, request *http.Request) {
	linked, err := handler.relations.ListTaskTopics(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeTaskError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, linked)
}

func (handler *taskHandler) createTopicRelation(response http.ResponseWriter, request *http.Request) {
	var input relation.LinkTopicInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的关联")
		return
	}
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_RELATION", "请选择要关联的 Topic")
		return
	}
	if err := handler.relations.LinkTopicTask(request.Context(), input.TopicID, request.PathValue("taskID")); err != nil {
		writeTaskError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *taskHandler) deleteTopicRelation(response http.ResponseWriter, request *http.Request) {
	if err := handler.relations.UnlinkTopicTask(
		request.Context(),
		request.PathValue("topicID"),
		request.PathValue("taskID"),
	); err != nil {
		writeTaskError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *taskHandler) listMessages(response http.ResponseWriter, request *http.Request) {
	messages, err := handler.discussion.ListTaskMessages(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeTaskError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, messages)
}

func (handler *taskHandler) createMessage(response http.ResponseWriter, request *http.Request) {
	var input discussion.CreateMessageInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的消息")
		return
	}
	if err := input.Normalized().Validate(); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_MESSAGE", "消息内容不能为空，且最多关联 20 个 Task")
		return
	}
	created, err := handler.discussion.AppendTaskMessage(request.Context(), request.PathValue("taskID"), input)
	if err != nil {
		writeTaskError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (handler *taskHandler) listRelations(response http.ResponseWriter, request *http.Request) {
	linked, err := handler.relations.ListTaskRelations(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeTaskError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, linked)
}

func (handler *taskHandler) createRelation(response http.ResponseWriter, request *http.Request) {
	var input relation.LinkTaskInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的关联")
		return
	}
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_RELATION", "请选择要关联的 Task")
		return
	}
	if err := handler.relations.LinkTasksTyped(request.Context(), request.PathValue("taskID"), input.TaskID, input.Type); err != nil {
		writeTaskError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *taskHandler) deleteRelation(response http.ResponseWriter, request *http.Request) {
	relationType := relation.Type(strings.ToUpper(strings.TrimSpace(request.URL.Query().Get("type"))))
	if relationType == "" {
		relationType = relation.TypeRelatesTo
	}
	sourceID, targetID := request.PathValue("taskID"), request.PathValue("relatedTaskID")
	if request.URL.Query().Get("direction") == string(relation.DirectionIncoming) {
		sourceID, targetID = targetID, sourceID
	}
	if err := handler.relations.UnlinkTaskRelation(request.Context(), sourceID, targetID, relationType); err != nil {
		writeTaskError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *taskHandler) create(response http.ResponseWriter, request *http.Request) {
	var body createTaskRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 Task")
		return
	}
	priority := 2
	if body.Priority != nil {
		priority = *body.Priority
	}
	input := task.CreateInput{
		Title: body.Title, Description: body.Description, AcceptanceCriteria: body.AcceptanceCriteria,
		Priority: priority, TargetBranch: body.TargetBranch,
	}
	if input.TargetBranch != "" {
		if handler.branches == nil || handler.branches.ValidateTargetBranch(request.Context(), input.TargetBranch) != nil {
			writeError(response, http.StatusBadRequest, "INVALID_TARGET_BRANCH", "目标分支必须是当前仓库中已有 Commit 的本地分支")
			return
		}
	}
	if err := input.Validate(); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_TASK", err.Error())
		return
	}
	created, err := handler.store.Create(request.Context(), input)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "TASK_CREATE_FAILED", "创建 Task 失败")
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (handler *taskHandler) list(response http.ResponseWriter, request *http.Request) {
	var tasks []task.Task
	var err error
	if request.URL.Query().Get("include_archived") == "true" {
		tasks, err = handler.store.ListIncludingArchived(request.Context())
	} else {
		tasks, err = handler.store.List(request.Context())
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "TASK_LIST_FAILED", "读取 Task 列表失败")
		return
	}
	assessments, err := handler.assessments.ListCurrent(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "TASK_LIST_FAILED", "读取 Task 评估失败")
		return
	}
	result := make([]taskListItem, 0, len(tasks))
	for _, item := range tasks {
		view := taskListItem{Task: item}
		if current, exists := assessments[item.ID]; exists {
			view.Assessment = &current
			view.AssessmentStale = current.TaskAssessmentVersion != item.AssessmentInputVersion
		}
		result = append(result, view)
	}
	writeJSON(response, http.StatusOK, result)
}

func (handler *taskHandler) get(response http.ResponseWriter, request *http.Request) {
	loaded, err := handler.store.Get(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeTaskError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, loaded)
}

func writeTaskError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrTaskNotFound):
		writeError(response, http.StatusNotFound, "TASK_NOT_FOUND", "Task 不存在")
	case errors.Is(err, storage.ErrTaskFeedbackNotFound):
		writeError(response, http.StatusNotFound, "TASK_FEEDBACK_NOT_FOUND", "Task 反馈不存在")
	case errors.Is(err, storage.ErrTopicNotFound):
		writeError(response, http.StatusNotFound, "TOPIC_NOT_FOUND", "Topic 不存在")
	case errors.Is(err, storage.ErrTaskVersionConflict):
		writeError(response, http.StatusConflict, "TASK_VERSION_CONFLICT", "Task 已被更新，请刷新后重试")
	case errors.Is(err, storage.ErrTaskRetryRequiresAnswer):
		writeError(response, http.StatusConflict, "CLARIFICATION_ANSWER_REQUIRED", "请先回答 Agent 的结构化问题，再继续执行")
	case errors.Is(err, storage.ErrSelfTaskLink):
		writeError(response, http.StatusBadRequest, "INVALID_RELATION", "Task 不能关联自身")
	case errors.Is(err, storage.ErrTaskFeedbackConflict):
		writeError(response, http.StatusConflict, "TASK_FEEDBACK_CONFLICT", "当前反馈状态不允许该操作，请刷新后重试")
	case errors.Is(err, storage.ErrTaskArchiveState), errors.Is(err, storage.ErrTaskEditState):
		writeError(response, http.StatusConflict, "TASK_COMMAND_CONFLICT", err.Error())
	default:
		var transitionErr *task.TransitionError
		if errors.As(err, &transitionErr) {
			writeError(response, http.StatusConflict, "INVALID_TASK_TRANSITION", transitionErr.Error())
			return
		}
		writeError(response, http.StatusInternalServerError, "TASK_COMMAND_FAILED", "执行 Task 命令失败")
	}
}

func decodeJSON(response http.ResponseWriter, request *http.Request, destination any) error {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("content type must be application/json")
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, errorEnvelope{Error: apiError{Code: code, Message: message}})
}
