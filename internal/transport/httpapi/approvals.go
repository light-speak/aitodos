package httpapi

import (
	"errors"
	"net/http"

	"github.com/light-speak/aitodos/internal/domain/approvalrequest"
	"github.com/light-speak/aitodos/internal/storage"
)

type approvalHandler struct {
	store *storage.RunStore
}

type approvalDecisionInput struct {
	Decision        approvalrequest.Decision `json:"decision"`
	ExpectedVersion int64                    `json:"expected_version"`
}

// RegisterApprovalRoutes 注册全局待处理权限和 Run 权限历史端点。
func RegisterApprovalRoutes(mux *http.ServeMux, store *storage.RunStore) {
	handler := &approvalHandler{store: store}
	mux.HandleFunc("GET /api/approvals", handler.listOpen)
	mux.HandleFunc("GET /api/runs/{runID}/approvals", handler.listRun)
	mux.HandleFunc("POST /api/approvals/{approvalID}/decision", handler.resolve)
}

func (handler *approvalHandler) listOpen(response http.ResponseWriter, request *http.Request) {
	items, err := handler.store.ListOpenApprovalRequests(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "APPROVAL_LIST_FAILED", "读取权限请求失败")
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (handler *approvalHandler) listRun(response http.ResponseWriter, request *http.Request) {
	items, err := handler.store.ListRunApprovalRequests(request.Context(), request.PathValue("runID"))
	if err != nil {
		writeError(response, http.StatusInternalServerError, "APPROVAL_LIST_FAILED", "读取 Run 权限历史失败")
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (handler *approvalHandler) resolve(response http.ResponseWriter, request *http.Request) {
	var input approvalDecisionInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "权限决定请求格式无效")
		return
	}
	if input.ExpectedVersion < 1 || input.Decision == "" {
		writeError(response, http.StatusBadRequest, "INVALID_APPROVAL_DECISION", "权限决定格式无效")
		return
	}
	resolved, err := handler.store.ResolveApprovalRequest(
		request.Context(), request.PathValue("approvalID"), input.ExpectedVersion, input.Decision,
	)
	if errors.Is(err, storage.ErrApprovalRequestNotFound) {
		writeError(response, http.StatusNotFound, "APPROVAL_NOT_FOUND", "权限请求不存在")
		return
	}
	if errors.Is(err, storage.ErrApprovalRequestConflict) {
		writeError(response, http.StatusConflict, "APPROVAL_CONFLICT", "权限请求已处理或页面已过期，请刷新")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "APPROVAL_DECISION_FAILED", "保存权限决定失败")
		return
	}
	writeJSON(response, http.StatusOK, resolved)
}
