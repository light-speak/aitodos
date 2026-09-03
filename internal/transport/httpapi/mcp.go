package httpapi

import (
	"net/http"
	"strconv"

	"github.com/light-speak/aitodos/internal/storage"
)

// RegisterMCPRoutes 注册 MCP 审计只读端点。
func RegisterMCPRoutes(mux *http.ServeMux, audit *storage.MCPAuditStore) {
	mux.HandleFunc("GET /api/mcp/audit", func(response http.ResponseWriter, request *http.Request) {
		limit := 100
		if value := request.URL.Query().Get("limit"); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				writeError(response, http.StatusBadRequest, "INVALID_LIMIT", "limit 必须是 1 到 500 的整数")
				return
			}
			limit = parsed
		}
		items, err := audit.List(request.Context(), limit)
		if err != nil {
			writeError(response, http.StatusBadRequest, "INVALID_LIMIT", err.Error())
			return
		}
		writeJSON(response, http.StatusOK, items)
	})
	mux.HandleFunc("GET /api/runs/{runID}/mcp-calls", func(response http.ResponseWriter, request *http.Request) {
		items, err := audit.ListRun(request.Context(), request.PathValue("runID"), auditLimit(request))
		if err != nil {
			writeError(response, http.StatusBadRequest, "INVALID_LIMIT", err.Error())
			return
		}
		writeJSON(response, http.StatusOK, items)
	})
	mux.HandleFunc("GET /api/runs/{runID}/resources", func(response http.ResponseWriter, request *http.Request) {
		items, err := audit.ListRunResourceLeases(request.Context(), request.PathValue("runID"))
		if err != nil {
			writeError(response, http.StatusInternalServerError, "RESOURCE_LIST_FAILED", err.Error())
			return
		}
		writeJSON(response, http.StatusOK, items)
	})
}

func auditLimit(request *http.Request) int {
	value := request.URL.Query().Get("limit")
	if value == "" {
		return 100
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}
