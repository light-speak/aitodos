package httpapi

import (
	"net/http"

	"github.com/light-speak/aitodos/internal/storage"
)

type runHandler struct {
	store *storage.RunStore
}

// RegisterRunRoutes 注册 Run 实际用量等只读可观测性端点。
func RegisterRunRoutes(mux *http.ServeMux, store *storage.RunStore) {
	handler := &runHandler{store: store}
	mux.HandleFunc("GET /api/runs/usage", handler.usageSummary)
}

func (handler *runHandler) usageSummary(response http.ResponseWriter, request *http.Request) {
	summary, err := handler.store.UsageSummary(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "RUN_USAGE_READ_FAILED", "读取 Run 用量失败")
		return
	}
	writeJSON(response, http.StatusOK, summary)
}
