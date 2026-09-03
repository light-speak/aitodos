package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/light-speak/aitodos/internal/domain/retrievaleval"
	"github.com/light-speak/aitodos/internal/storage"
)

type retrievalEvalHandler struct {
	store *storage.RetrievalEvalStore
}

// RegisterRetrievalEvalRoutes 注册检索评测集和不可变评测结果端点。
func RegisterRetrievalEvalRoutes(mux *http.ServeMux, store *storage.RetrievalEvalStore) {
	handler := &retrievalEvalHandler{store: store}
	mux.HandleFunc("GET /api/retrieval-evals/cases", handler.listCases)
	mux.HandleFunc("POST /api/retrieval-evals/cases", handler.addRelevant)
	mux.HandleFunc("DELETE /api/retrieval-evals/cases/{caseID}/relevances/{documentID}", handler.removeRelevant)
	mux.HandleFunc("GET /api/retrieval-evals/runs", handler.listRuns)
	mux.HandleFunc("POST /api/retrieval-evals/runs", handler.run)
	mux.HandleFunc("GET /api/retrieval-evals/runs/{runID}", handler.getRun)
}

func (handler *retrievalEvalHandler) listCases(response http.ResponseWriter, request *http.Request) {
	items, err := handler.store.ListCases(request.Context())
	if err != nil {
		writeRetrievalEvalError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (handler *retrievalEvalHandler) addRelevant(response http.ResponseWriter, request *http.Request) {
	var input retrievaleval.CreateCaseInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_RETRIEVAL_EVAL", "请求内容不是有效的检索评测标注")
		return
	}
	created, err := handler.store.AddRelevant(request.Context(), input)
	if err != nil {
		writeRetrievalEvalError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (handler *retrievalEvalHandler) removeRelevant(response http.ResponseWriter, request *http.Request) {
	if err := handler.store.RemoveRelevant(request.Context(), request.PathValue("caseID"), request.PathValue("documentID")); err != nil {
		writeRetrievalEvalError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *retrievalEvalHandler) listRuns(response http.ResponseWriter, request *http.Request) {
	limit := 20
	if value := strings.TrimSpace(request.URL.Query().Get("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			writeError(response, http.StatusBadRequest, "INVALID_RETRIEVAL_EVAL", "评测历史数量无效")
			return
		}
		limit = parsed
	}
	items, err := handler.store.ListRuns(request.Context(), limit)
	if err != nil {
		writeRetrievalEvalError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (handler *retrievalEvalHandler) run(response http.ResponseWriter, request *http.Request) {
	var input struct {
		K int `json:"k"`
	}
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_RETRIEVAL_EVAL", "请求内容不是有效的检索评测配置")
		return
	}
	result, err := handler.store.Run(request.Context(), input.K)
	if err != nil {
		writeRetrievalEvalError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, result)
}

func (handler *retrievalEvalHandler) getRun(response http.ResponseWriter, request *http.Request) {
	item, err := handler.store.GetRun(request.Context(), request.PathValue("runID"))
	if err != nil {
		writeRetrievalEvalError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func writeRetrievalEvalError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrSearchDocumentNotFound):
		writeError(response, http.StatusNotFound, "SEARCH_DOCUMENT_NOT_FOUND", "相关搜索结果已不存在，请重新搜索")
	case errors.Is(err, storage.ErrRetrievalEvalCaseNotFound):
		writeError(response, http.StatusNotFound, "RETRIEVAL_EVAL_CASE_NOT_FOUND", "检索评测用例不存在")
	case errors.Is(err, storage.ErrRetrievalEvalRunNotFound):
		writeError(response, http.StatusNotFound, "RETRIEVAL_EVAL_RUN_NOT_FOUND", "检索评测运行不存在")
	case errors.Is(err, storage.ErrRetrievalEvalSuiteEmpty):
		writeError(response, http.StatusConflict, "RETRIEVAL_EVAL_SUITE_EMPTY", "请先从搜索结果加入至少一个评测样本")
	default:
		if strings.Contains(err.Error(), "必须") || strings.Contains(err.Error(), "不能超过") || strings.Contains(err.Error(), "无效") {
			writeError(response, http.StatusBadRequest, "INVALID_RETRIEVAL_EVAL", err.Error())
			return
		}
		writeError(response, http.StatusInternalServerError, "RETRIEVAL_EVAL_FAILED", "检索评测操作失败")
	}
}
