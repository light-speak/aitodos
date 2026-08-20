package httpapi

import (
	"errors"
	"net/http"

	"github.com/light-speak/aitodos/internal/domain/clarification"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/storage"
)

type clarificationHandler struct {
	store *storage.ClarificationStore
}

type clarificationAnswerResponse struct {
	Clarification clarification.Clarification `json:"clarification"`
	Task          task.Task                   `json:"task"`
}

// RegisterClarificationRoutes 注册 Agent 阻塞问题的读取与人工回答命令。
func RegisterClarificationRoutes(mux *http.ServeMux, store *storage.ClarificationStore) {
	handler := &clarificationHandler{store: store}
	mux.HandleFunc("GET /api/clarifications", handler.listOpen)
	mux.HandleFunc("GET /api/tasks/{taskID}/clarifications", handler.listTask)
	mux.HandleFunc("POST /api/clarifications/{clarificationID}/answer", handler.answer)
}

func (handler *clarificationHandler) listOpen(response http.ResponseWriter, request *http.Request) {
	items, err := handler.store.ListOpen(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "CLARIFICATIONS_READ_FAILED", "读取待回答问题失败")
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (handler *clarificationHandler) listTask(response http.ResponseWriter, request *http.Request) {
	items, err := handler.store.ListTask(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeClarificationError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (handler *clarificationHandler) answer(response http.ResponseWriter, request *http.Request) {
	var input clarification.AnswerInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的回答")
		return
	}
	answered, resumedTask, err := handler.store.Answer(request.Context(), request.PathValue("clarificationID"), input)
	if err != nil {
		writeClarificationError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, clarificationAnswerResponse{Clarification: answered, Task: resumedTask})
}

func writeClarificationError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrClarificationNotFound):
		writeError(response, http.StatusNotFound, "CLARIFICATION_NOT_FOUND", "待回答问题不存在")
	case errors.Is(err, storage.ErrClarificationConflict):
		writeError(response, http.StatusConflict, "CLARIFICATION_CONFLICT", "问题已被回答，请重新加载")
	case errors.Is(err, storage.ErrTaskNotFound):
		writeTaskError(response, err)
	default:
		writeError(response, http.StatusBadRequest, "INVALID_CLARIFICATION_ANSWER", err.Error())
	}
}
