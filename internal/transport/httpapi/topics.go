package httpapi

import (
	"errors"
	"net/http"

	"github.com/light-speak/aitodos/internal/domain/discussion"
	"github.com/light-speak/aitodos/internal/domain/relation"
	"github.com/light-speak/aitodos/internal/domain/topic"
	"github.com/light-speak/aitodos/internal/storage"
)

type topicHandler struct {
	store      *storage.TopicStore
	discussion *storage.DiscussionStore
	relations  *storage.RelationStore
}

type topicPlanningRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}

// RegisterTopicRoutes 注册 Topic 查询端点。
func RegisterTopicRoutes(
	mux *http.ServeMux,
	store *storage.TopicStore,
	discussionStore *storage.DiscussionStore,
	relationStore *storage.RelationStore,
) {
	handler := &topicHandler{store: store, discussion: discussionStore, relations: relationStore}
	mux.HandleFunc("POST /api/topics", handler.create)
	mux.HandleFunc("GET /api/topics", handler.list)
	mux.HandleFunc("GET /api/topics/{topicID}", handler.get)
	mux.HandleFunc("GET /api/topics/{topicID}/messages", handler.listMessages)
	mux.HandleFunc("POST /api/topics/{topicID}/messages", handler.createMessage)
	mux.HandleFunc("POST /api/topics/{topicID}/planning", handler.requestPlanning)
	mux.HandleFunc("GET /api/topics/{topicID}/relations", handler.listRelations)
	mux.HandleFunc("POST /api/topics/{topicID}/relations", handler.createRelation)
	mux.HandleFunc("DELETE /api/topics/{topicID}/relations/{taskID}", handler.deleteRelation)
}

func (handler *topicHandler) requestPlanning(response http.ResponseWriter, request *http.Request) {
	var input topicPlanningRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的规划请求")
		return
	}
	updated, err := handler.store.RequestPlanning(request.Context(), request.PathValue("topicID"), input.ExpectedVersion)
	if err != nil {
		writeTopicError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, updated)
}

func (handler *topicHandler) listRelations(response http.ResponseWriter, request *http.Request) {
	linked, err := handler.relations.ListTopicTasks(request.Context(), request.PathValue("topicID"))
	if err != nil {
		writeTopicError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, linked)
}

func (handler *topicHandler) createRelation(response http.ResponseWriter, request *http.Request) {
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
	if err := handler.relations.LinkTopicTask(request.Context(), request.PathValue("topicID"), input.TaskID); err != nil {
		writeTopicError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *topicHandler) deleteRelation(response http.ResponseWriter, request *http.Request) {
	if err := handler.relations.UnlinkTopicTask(request.Context(), request.PathValue("topicID"), request.PathValue("taskID")); err != nil {
		writeTopicError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *topicHandler) listMessages(response http.ResponseWriter, request *http.Request) {
	messages, err := handler.discussion.ListTopicMessages(request.Context(), request.PathValue("topicID"))
	if err != nil {
		writeTopicError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, messages)
}

func (handler *topicHandler) createMessage(response http.ResponseWriter, request *http.Request) {
	var input discussion.CreateMessageInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的消息")
		return
	}
	if err := input.Normalized().Validate(); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_MESSAGE", "消息内容不能为空，且最多关联 20 个 Task")
		return
	}
	created, err := handler.discussion.AppendTopicMessage(request.Context(), request.PathValue("topicID"), input)
	if err != nil {
		writeTopicError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (handler *topicHandler) create(response http.ResponseWriter, request *http.Request) {
	var input topic.CreateInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 Topic")
		return
	}
	if err := input.Validate(); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_TOPIC", err.Error())
		return
	}
	created, err := handler.store.Create(request.Context(), input)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "TOPIC_CREATE_FAILED", "创建 Topic 失败")
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (handler *topicHandler) list(response http.ResponseWriter, request *http.Request) {
	topics, err := handler.store.List(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "TOPIC_LIST_FAILED", "读取 Topic 列表失败")
		return
	}
	writeJSON(response, http.StatusOK, topics)
}

func (handler *topicHandler) get(response http.ResponseWriter, request *http.Request) {
	loaded, err := handler.store.Get(request.Context(), request.PathValue("topicID"))
	if err != nil {
		writeTopicError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, loaded)
}

func writeTopicError(response http.ResponseWriter, err error) {
	if errors.Is(err, storage.ErrTopicNotFound) {
		writeError(response, http.StatusNotFound, "TOPIC_NOT_FOUND", "Topic 不存在")
		return
	}
	if errors.Is(err, storage.ErrTaskNotFound) {
		writeError(response, http.StatusNotFound, "TASK_NOT_FOUND", "Task 不存在")
		return
	}
	if errors.Is(err, storage.ErrTopicVersionConflict) {
		writeError(response, http.StatusConflict, "TOPIC_VERSION_CONFLICT", "Topic 已被更新，请刷新后重试")
		return
	}
	writeError(response, http.StatusInternalServerError, "TOPIC_COMMAND_FAILED", "执行 Topic 命令失败")
}
