package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/light-speak/aitodos/internal/domain/knowledge"
	"github.com/light-speak/aitodos/internal/storage"
)

type knowledgeHandler struct {
	store *storage.KnowledgeStore
}

type createDecisionRequest struct {
	Title                string `json:"title"`
	Content              string `json:"content"`
	SupersedesDecisionID string `json:"supersedes_decision_id"`
}

// RegisterKnowledgeRoutes 注册 Decision、Label、Run Summary 和 CI Snapshot 接口。
func RegisterKnowledgeRoutes(mux *http.ServeMux, store *storage.KnowledgeStore) {
	handler := &knowledgeHandler{store: store}
	mux.HandleFunc("GET /api/labels", handler.listLabels)
	mux.HandleFunc("POST /api/labels", handler.createLabel)
	mux.HandleFunc("GET /api/topics/{topicID}/labels", handler.listTopicLabels)
	mux.HandleFunc("POST /api/topics/{topicID}/labels/{labelID}", handler.attachTopicLabel)
	mux.HandleFunc("DELETE /api/topics/{topicID}/labels/{labelID}", handler.detachTopicLabel)
	mux.HandleFunc("GET /api/tasks/{taskID}/labels", handler.listTaskLabels)
	mux.HandleFunc("POST /api/tasks/{taskID}/labels/{labelID}", handler.attachTaskLabel)
	mux.HandleFunc("DELETE /api/tasks/{taskID}/labels/{labelID}", handler.detachTaskLabel)
	mux.HandleFunc("GET /api/topics/{topicID}/decisions", handler.listTopicDecisions)
	mux.HandleFunc("POST /api/topics/{topicID}/decisions", handler.createTopicDecision)
	mux.HandleFunc("GET /api/tasks/{taskID}/decisions", handler.listTaskDecisions)
	mux.HandleFunc("POST /api/tasks/{taskID}/decisions", handler.createTaskDecision)
	mux.HandleFunc("GET /api/runs/{runID}/summary", handler.getRunSummary)
	mux.HandleFunc("GET /api/tasks/{taskID}/ci-snapshots", handler.listCISnapshots)
	mux.HandleFunc("POST /api/tasks/{taskID}/ci-snapshots", handler.createCISnapshot)
}

func (handler *knowledgeHandler) createLabel(response http.ResponseWriter, request *http.Request) {
	var input knowledge.LabelInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的标签")
		return
	}
	created, err := handler.store.CreateLabel(request.Context(), input)
	if err != nil {
		writeKnowledgeError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (handler *knowledgeHandler) listLabels(response http.ResponseWriter, request *http.Request) {
	items, err := handler.store.ListLabels(request.Context())
	if err != nil {
		writeKnowledgeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (handler *knowledgeHandler) attachTopicLabel(response http.ResponseWriter, request *http.Request) {
	if err := handler.store.AttachTopicLabel(request.Context(), request.PathValue("topicID"), request.PathValue("labelID")); err != nil {
		writeKnowledgeError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *knowledgeHandler) detachTopicLabel(response http.ResponseWriter, request *http.Request) {
	if err := handler.store.DetachTopicLabel(request.Context(), request.PathValue("topicID"), request.PathValue("labelID")); err != nil {
		writeKnowledgeError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *knowledgeHandler) listTopicLabels(response http.ResponseWriter, request *http.Request) {
	items, err := handler.store.ListTopicLabels(request.Context(), request.PathValue("topicID"))
	if err != nil {
		writeKnowledgeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (handler *knowledgeHandler) attachTaskLabel(response http.ResponseWriter, request *http.Request) {
	if err := handler.store.AttachTaskLabel(request.Context(), request.PathValue("taskID"), request.PathValue("labelID")); err != nil {
		writeKnowledgeError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *knowledgeHandler) detachTaskLabel(response http.ResponseWriter, request *http.Request) {
	if err := handler.store.DetachTaskLabel(request.Context(), request.PathValue("taskID"), request.PathValue("labelID")); err != nil {
		writeKnowledgeError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *knowledgeHandler) listTaskLabels(response http.ResponseWriter, request *http.Request) {
	items, err := handler.store.ListTaskLabels(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeKnowledgeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (handler *knowledgeHandler) createTopicDecision(response http.ResponseWriter, request *http.Request) {
	handler.createDecision(response, request, request.PathValue("topicID"), "")
}

func (handler *knowledgeHandler) createTaskDecision(response http.ResponseWriter, request *http.Request) {
	handler.createDecision(response, request, "", request.PathValue("taskID"))
}

func (handler *knowledgeHandler) createDecision(response http.ResponseWriter, request *http.Request, topicID, taskID string) {
	var body createDecisionRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的决策")
		return
	}
	created, err := handler.store.CreateDecision(request.Context(), knowledge.DecisionInput{
		TopicID: topicID, TaskID: taskID, Title: body.Title, Content: body.Content,
		SupersedesDecisionID: body.SupersedesDecisionID,
	})
	if err != nil {
		writeKnowledgeError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (handler *knowledgeHandler) listTopicDecisions(response http.ResponseWriter, request *http.Request) {
	items, err := handler.store.ListTopicDecisions(request.Context(), request.PathValue("topicID"), includeSuperseded(request))
	if err != nil {
		writeKnowledgeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (handler *knowledgeHandler) listTaskDecisions(response http.ResponseWriter, request *http.Request) {
	items, err := handler.store.ListTaskDecisions(request.Context(), request.PathValue("taskID"), includeSuperseded(request))
	if err != nil {
		writeKnowledgeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func includeSuperseded(request *http.Request) bool {
	value, _ := strconv.ParseBool(request.URL.Query().Get("include_superseded"))
	return value
}

func (handler *knowledgeHandler) getRunSummary(response http.ResponseWriter, request *http.Request) {
	summary, err := handler.store.GetRunSummary(request.Context(), request.PathValue("runID"))
	if err != nil {
		writeKnowledgeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, summary)
}

func (handler *knowledgeHandler) createCISnapshot(response http.ResponseWriter, request *http.Request) {
	var input knowledge.CISnapshotInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 CI 快照")
		return
	}
	created, err := handler.store.CreateCISnapshot(request.Context(), request.PathValue("taskID"), input)
	if err != nil {
		writeKnowledgeError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (handler *knowledgeHandler) listCISnapshots(response http.ResponseWriter, request *http.Request) {
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	items, err := handler.store.ListCISnapshots(request.Context(), request.PathValue("taskID"), limit)
	if err != nil {
		writeKnowledgeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func writeKnowledgeError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrTopicNotFound):
		writeError(response, http.StatusNotFound, "TOPIC_NOT_FOUND", "Topic 不存在")
	case errors.Is(err, storage.ErrTaskNotFound):
		writeError(response, http.StatusNotFound, "TASK_NOT_FOUND", "Task 不存在")
	case errors.Is(err, storage.ErrLabelNotFound):
		writeError(response, http.StatusNotFound, "LABEL_NOT_FOUND", "标签不存在")
	case errors.Is(err, storage.ErrDecisionNotFound):
		writeError(response, http.StatusNotFound, "DECISION_NOT_FOUND", "决策不存在")
	case errors.Is(err, storage.ErrRunSummaryNotFound):
		writeError(response, http.StatusNotFound, "RUN_SUMMARY_NOT_FOUND", "Run 尚无摘要")
	case errors.Is(err, storage.ErrDecisionSubjectMismatch):
		writeError(response, http.StatusConflict, "DECISION_SUBJECT_MISMATCH", "只能替代同一 Topic 或 Task 的决策")
	default:
		writeError(response, http.StatusBadRequest, "KNOWLEDGE_COMMAND_FAILED", err.Error())
	}
}
