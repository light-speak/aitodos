package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/light-speak/aitodos/internal/domain/experience"
	"github.com/light-speak/aitodos/internal/storage"
)

type experienceHandler struct {
	store *storage.ExperienceStore
}

// RegisterExperienceRoutes 注册经验资产和召回反馈端点。
func RegisterExperienceRoutes(mux *http.ServeMux, store *storage.ExperienceStore) {
	handler := &experienceHandler{store: store}
	mux.HandleFunc("GET /api/topics/{topicID}/experiences", handler.listTopic)
	mux.HandleFunc("POST /api/topics/{topicID}/experiences", handler.createTopic)
	mux.HandleFunc("GET /api/tasks/{taskID}/experiences", handler.listTask)
	mux.HandleFunc("POST /api/tasks/{taskID}/experiences", handler.createTask)
	mux.HandleFunc("GET /api/experiences/{experienceID}", handler.get)
	mux.HandleFunc("POST /api/experiences/{experienceID}/pin", handler.pin)
	mux.HandleFunc("POST /api/experiences/{experienceID}/confirm", handler.confirm)
	mux.HandleFunc("POST /api/experiences/{experienceID}/challenge", handler.challenge)
	mux.HandleFunc("POST /api/experience-recalls/{recallID}/outcome", handler.outcome)
	mux.HandleFunc("GET /api/runs/{runID}/experiences", handler.listRunRecalls)
}

func (handler *experienceHandler) createTopic(response http.ResponseWriter, request *http.Request) {
	handler.create(response, request, request.PathValue("topicID"), "")
}

func (handler *experienceHandler) createTask(response http.ResponseWriter, request *http.Request) {
	handler.create(response, request, "", request.PathValue("taskID"))
}

func (handler *experienceHandler) create(response http.ResponseWriter, request *http.Request, topicID, taskID string) {
	var input experience.Input
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的经验")
		return
	}
	input.TopicID = topicID
	input.TaskID = taskID
	// HTTP 创建表示人工固化，不允许调用方伪造 Agent Run 来源。
	input.SourceRunID = ""
	created, err := handler.store.CreateVerified(request.Context(), input)
	if err != nil {
		writeExperienceError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (handler *experienceHandler) listTopic(response http.ResponseWriter, request *http.Request) {
	items, err := handler.store.ListTopic(request.Context(), request.PathValue("topicID"), includeInactive(request))
	if err != nil {
		writeExperienceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (handler *experienceHandler) listTask(response http.ResponseWriter, request *http.Request) {
	items, err := handler.store.ListTask(request.Context(), request.PathValue("taskID"), includeInactive(request))
	if err != nil {
		writeExperienceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func includeInactive(request *http.Request) bool {
	value, _ := strconv.ParseBool(request.URL.Query().Get("include_inactive"))
	return value
}

func (handler *experienceHandler) get(response http.ResponseWriter, request *http.Request) {
	item, err := handler.store.Get(request.Context(), request.PathValue("experienceID"))
	if err != nil {
		writeExperienceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (handler *experienceHandler) pin(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Pinned bool `json:"pinned"`
	}
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的固定设置")
		return
	}
	item, err := handler.store.SetPinned(request.Context(), request.PathValue("experienceID"), input.Pinned)
	if err != nil {
		writeExperienceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (handler *experienceHandler) confirm(response http.ResponseWriter, request *http.Request) {
	item, err := handler.store.ConfirmCandidate(request.Context(), request.PathValue("experienceID"))
	if err != nil {
		writeExperienceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (handler *experienceHandler) challenge(response http.ResponseWriter, request *http.Request) {
	item, err := handler.store.Challenge(request.Context(), request.PathValue("experienceID"))
	if err != nil {
		writeExperienceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (handler *experienceHandler) outcome(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Outcome experience.Outcome `json:"outcome"`
	}
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的经验反馈")
		return
	}
	if err := handler.store.RecordOutcome(request.Context(), request.PathValue("recallID"), input.Outcome); err != nil {
		writeExperienceError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *experienceHandler) listRunRecalls(response http.ResponseWriter, request *http.Request) {
	items, err := handler.store.ListRunRecalls(request.Context(), request.PathValue("runID"))
	if err != nil {
		writeExperienceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func writeExperienceError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrTopicNotFound):
		writeError(response, http.StatusNotFound, "TOPIC_NOT_FOUND", "Topic 不存在")
	case errors.Is(err, storage.ErrTaskNotFound):
		writeError(response, http.StatusNotFound, "TASK_NOT_FOUND", "Task 不存在")
	case errors.Is(err, storage.ErrExperienceNotFound):
		writeError(response, http.StatusNotFound, "EXPERIENCE_NOT_FOUND", "经验不存在")
	case errors.Is(err, storage.ErrExperienceRecallNotFound):
		writeError(response, http.StatusNotFound, "EXPERIENCE_RECALL_NOT_FOUND", "经验召回记录不存在")
	case errors.Is(err, storage.ErrExperienceSubjectMismatch):
		writeError(response, http.StatusConflict, "EXPERIENCE_SUBJECT_MISMATCH", "只能替代同一 Topic 或 Task 的经验")
	case errors.Is(err, storage.ErrExperienceRunSubjectMismatch):
		writeError(response, http.StatusConflict, "EXPERIENCE_RUN_SUBJECT_MISMATCH", "经验候选必须绑定同一 Task 的实现或修订 Run")
	default:
		writeError(response, http.StatusBadRequest, "EXPERIENCE_COMMAND_FAILED", err.Error())
	}
}
