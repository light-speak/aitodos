// Package httpapi 提供项目本地 Daemon 的 REST 接口。
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/light-speak/aitodos/internal/domain/discussion"
	"github.com/light-speak/aitodos/internal/domain/relation"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/storage"
)

const maxRequestBodyBytes = 1 << 20

type taskHandler struct {
	store      *storage.TaskStore
	discussion *storage.DiscussionStore
	relations  *storage.RelationStore
}

type commandRequest struct {
	Version int64 `json:"version"`
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
) {
	handler := &taskHandler{store: store, discussion: discussionStore, relations: relationStore}
	mux.HandleFunc("POST /api/tasks", handler.create)
	mux.HandleFunc("GET /api/tasks", handler.list)
	mux.HandleFunc("GET /api/tasks/{taskID}", handler.get)
	mux.HandleFunc("POST /api/tasks/{taskID}/queue", handler.queue)
	mux.HandleFunc("GET /api/tasks/{taskID}/messages", handler.listMessages)
	mux.HandleFunc("POST /api/tasks/{taskID}/messages", handler.createMessage)
	mux.HandleFunc("GET /api/tasks/{taskID}/relations", handler.listRelations)
	mux.HandleFunc("POST /api/tasks/{taskID}/relations", handler.createRelation)
	mux.HandleFunc("DELETE /api/tasks/{taskID}/relations/{relatedTaskID}", handler.deleteRelation)
	mux.HandleFunc("GET /api/tasks/{taskID}/topics", handler.listTopics)
	mux.HandleFunc("POST /api/tasks/{taskID}/topics", handler.createTopicRelation)
	mux.HandleFunc("DELETE /api/tasks/{taskID}/topics/{topicID}", handler.deleteTopicRelation)
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
	if err := handler.relations.LinkTasks(request.Context(), request.PathValue("taskID"), input.TaskID); err != nil {
		writeTaskError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *taskHandler) deleteRelation(response http.ResponseWriter, request *http.Request) {
	if err := handler.relations.UnlinkTasks(
		request.Context(),
		request.PathValue("taskID"),
		request.PathValue("relatedTaskID"),
	); err != nil {
		writeTaskError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *taskHandler) create(response http.ResponseWriter, request *http.Request) {
	var input task.CreateInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 Task")
		return
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
	tasks, err := handler.store.List(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "TASK_LIST_FAILED", "读取 Task 列表失败")
		return
	}
	writeJSON(response, http.StatusOK, tasks)
}

func (handler *taskHandler) get(response http.ResponseWriter, request *http.Request) {
	loaded, err := handler.store.Get(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeTaskError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, loaded)
}

func (handler *taskHandler) queue(response http.ResponseWriter, request *http.Request) {
	var input commandRequest
	if err := decodeJSON(response, request, &input); err != nil || input.Version < 1 {
		writeError(response, http.StatusBadRequest, "INVALID_COMMAND", "version 必须是正整数")
		return
	}
	updated, err := handler.store.ApplyCommand(
		request.Context(),
		request.PathValue("taskID"),
		input.Version,
		task.CommandQueue,
	)
	if err != nil {
		writeTaskError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, updated)
}

func writeTaskError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrTaskNotFound):
		writeError(response, http.StatusNotFound, "TASK_NOT_FOUND", "Task 不存在")
	case errors.Is(err, storage.ErrTopicNotFound):
		writeError(response, http.StatusNotFound, "TOPIC_NOT_FOUND", "Topic 不存在")
	case errors.Is(err, storage.ErrTaskVersionConflict):
		writeError(response, http.StatusConflict, "TASK_VERSION_CONFLICT", "Task 已被更新，请刷新后重试")
	case errors.Is(err, storage.ErrSelfTaskLink):
		writeError(response, http.StatusBadRequest, "INVALID_RELATION", "Task 不能关联自身")
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
