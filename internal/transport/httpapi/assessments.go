package httpapi

import (
	"errors"
	"net/http"

	"github.com/light-speak/aitodos/internal/domain/assessment"
	"github.com/light-speak/aitodos/internal/storage"
)

type assessmentHandler struct {
	tasks       *storage.TaskStore
	assessments *storage.AssessmentStore
}

type taskAssessmentResponse struct {
	Current *assessment.Assessment  `json:"current"`
	History []assessment.Assessment `json:"history"`
	Stale   bool                    `json:"stale"`
}

// RegisterAssessmentRoutes 注册 Task 复杂度评估只读端点。
func RegisterAssessmentRoutes(
	mux *http.ServeMux,
	tasks *storage.TaskStore,
	assessments *storage.AssessmentStore,
) {
	handler := &assessmentHandler{tasks: tasks, assessments: assessments}
	mux.HandleFunc("GET /api/tasks/{taskID}/assessment", handler.get)
}

func (handler *assessmentHandler) get(response http.ResponseWriter, request *http.Request) {
	taskID := request.PathValue("taskID")
	currentTask, err := handler.tasks.Get(request.Context(), taskID)
	if err != nil {
		writeTaskError(response, err)
		return
	}
	history, err := handler.assessments.List(request.Context(), taskID)
	if err != nil {
		writeAssessmentError(response, err)
		return
	}
	result := taskAssessmentResponse{History: history}
	if len(history) > 0 {
		result.Current = &history[0]
		result.Stale = history[0].TaskAssessmentVersion != currentTask.AssessmentInputVersion
	}
	writeJSON(response, http.StatusOK, result)
}

func writeAssessmentError(response http.ResponseWriter, err error) {
	if errors.Is(err, storage.ErrTaskNotFound) {
		writeTaskError(response, err)
		return
	}
	writeError(response, http.StatusInternalServerError, "ASSESSMENT_READ_FAILED", "读取 Task 评估失败")
}
