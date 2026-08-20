package httpapi

import (
	"errors"
	"net/http"

	"github.com/light-speak/aitodos/internal/domain/plan"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/storage"
)

type planHandler struct {
	store *storage.PlanStore
}

type createPlanRevisionRequest struct {
	ExpectedTopicVersion int64                 `json:"expected_topic_version"`
	Summary              string                `json:"summary"`
	Rationale            string                `json:"rationale"`
	Risks                string                `json:"risks"`
	Drafts               []plan.TaskDraftInput `json:"drafts"`
}

type approvePlanResponse struct {
	Plan  plan.View   `json:"plan"`
	Tasks []task.Task `json:"tasks"`
}

// RegisterPlanRoutes 注册 Plan Revision 与人工审核命令。
func RegisterPlanRoutes(mux *http.ServeMux, store *storage.PlanStore) {
	handler := &planHandler{store: store}
	mux.HandleFunc("GET /api/topics/{topicID}/plans/current", handler.getCurrent)
	mux.HandleFunc("POST /api/topics/{topicID}/plans", handler.createRevision)
	mux.HandleFunc("POST /api/plans/{planID}/request-changes", handler.requestChanges)
	mux.HandleFunc("POST /api/plans/{planID}/approve", handler.approve)
}

func (handler *planHandler) getCurrent(response http.ResponseWriter, request *http.Request) {
	view, err := handler.store.GetByTopic(request.Context(), request.PathValue("topicID"))
	if errors.Is(err, storage.ErrPlanNotFound) {
		writeJSON(response, http.StatusOK, nil)
		return
	}
	if err != nil {
		writePlanError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, view)
}

func (handler *planHandler) createRevision(response http.ResponseWriter, request *http.Request) {
	var body createPlanRevisionRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 Plan Revision")
		return
	}
	view, err := handler.store.CreateRevision(request.Context(), request.PathValue("topicID"), body.ExpectedTopicVersion, plan.RevisionInput{
		Summary: body.Summary, Rationale: body.Rationale, Risks: body.Risks,
		Drafts: body.Drafts,
	})
	if err != nil {
		writePlanError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, view)
}

func (handler *planHandler) requestChanges(response http.ResponseWriter, request *http.Request) {
	var input plan.ReviewInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 Plan Review")
		return
	}
	view, err := handler.store.RequestChanges(request.Context(), request.PathValue("planID"), input)
	if err != nil {
		writePlanError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, view)
}

func (handler *planHandler) approve(response http.ResponseWriter, request *http.Request) {
	var input plan.ReviewInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 Plan Review")
		return
	}
	view, tasks, err := handler.store.Approve(request.Context(), request.PathValue("planID"), input)
	if err != nil {
		writePlanError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, approvePlanResponse{Plan: view, Tasks: tasks})
}

func writePlanError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrPlanNotFound):
		writeError(response, http.StatusNotFound, "PLAN_NOT_FOUND", "Plan 不存在")
	case errors.Is(err, storage.ErrTopicNotFound):
		writeError(response, http.StatusNotFound, "TOPIC_NOT_FOUND", "Topic 不存在")
	case errors.Is(err, storage.ErrPlanConflict), errors.Is(err, storage.ErrTopicVersionConflict):
		writeError(response, http.StatusConflict, "PLAN_CONFLICT", "Plan 或 Topic 已更新，请重新加载")
	default:
		writeError(response, http.StatusBadRequest, "INVALID_PLAN", err.Error())
	}
}
