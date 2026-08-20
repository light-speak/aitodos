package httpapi

import (
	"errors"
	"net/http"

	"github.com/light-speak/aitodos/internal/domain/quality"
	"github.com/light-speak/aitodos/internal/storage"
)

type qualityHandler struct {
	store *storage.QualityStore
}

type createHumanEstimateRequest struct {
	Points          int     `json:"points"`
	RemainingPoints int     `json:"remaining_points"`
	Confidence      float64 `json:"confidence"`
	Rationale       string  `json:"rationale"`
}

type createHumanTestCaseRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	SortOrder   int    `json:"sort_order"`
}

// RegisterQualityRoutes 注册 Task 估算、测试证据和项目进度端点。
func RegisterQualityRoutes(mux *http.ServeMux, store *storage.QualityStore) {
	handler := &qualityHandler{store: store}
	mux.HandleFunc("GET /api/progress", handler.progress)
	mux.HandleFunc("GET /api/tasks/{taskID}/quality", handler.taskQuality)
	mux.HandleFunc("POST /api/tasks/{taskID}/estimates", handler.createEstimate)
	mux.HandleFunc("POST /api/tasks/{taskID}/test-cases", handler.createTestCase)
	mux.HandleFunc("POST /api/tasks/{taskID}/test-cases/{testCaseID}/results", handler.addTestResult)
}

func (handler *qualityHandler) progress(response http.ResponseWriter, request *http.Request) {
	progress, err := handler.store.ProjectProgress(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "PROGRESS_READ_FAILED", "读取项目进度失败")
		return
	}
	writeJSON(response, http.StatusOK, progress)
}

func (handler *qualityHandler) taskQuality(response http.ResponseWriter, request *http.Request) {
	result, err := handler.store.GetTaskQuality(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeTaskError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (handler *qualityHandler) createEstimate(response http.ResponseWriter, request *http.Request) {
	var body createHumanEstimateRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 Task 估算")
		return
	}
	created, err := handler.store.CreateEstimate(request.Context(), request.PathValue("taskID"), quality.EstimateInput{
		Points: body.Points, RemainingPoints: body.RemainingPoints,
		Confidence: body.Confidence, Rationale: body.Rationale, Source: quality.EstimateHuman,
	})
	if err != nil {
		writeQualityError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (handler *qualityHandler) createTestCase(response http.ResponseWriter, request *http.Request) {
	var body createHumanTestCaseRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的测试项")
		return
	}
	created, err := handler.store.CreateTestCase(request.Context(), request.PathValue("taskID"), quality.TestCaseInput{
		Title: body.Title, Description: body.Description, Required: body.Required,
		SortOrder: body.SortOrder, CreatedBy: quality.TestCreatorHuman,
	})
	if err != nil {
		writeQualityError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (handler *qualityHandler) addTestResult(response http.ResponseWriter, request *http.Request) {
	var input quality.TestResultInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的测试结果")
		return
	}
	if input.EvidenceKind == quality.EvidenceAgentReport {
		writeError(response, http.StatusBadRequest, "INVALID_TEST_EVIDENCE", "Agent 报告只能由对应 Run 写入")
		return
	}
	created, err := handler.store.AddTestResult(
		request.Context(), request.PathValue("taskID"), request.PathValue("testCaseID"), input,
	)
	if err != nil {
		writeQualityError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func writeQualityError(response http.ResponseWriter, err error) {
	if errors.Is(err, storage.ErrTaskNotFound) {
		writeTaskError(response, err)
		return
	}
	writeError(response, http.StatusBadRequest, "INVALID_QUALITY_DATA", err.Error())
}
