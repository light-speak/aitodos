package httpapi

import (
	"errors"
	"net/http"

	"github.com/light-speak/aitodos/internal/domain/objective"
	"github.com/light-speak/aitodos/internal/storage"
)

type objectiveHandler struct {
	store *storage.ObjectiveStore
}

type objectiveCommandRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}

type objectiveCheckpointRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
	objective.CheckpointInput
}

// RegisterObjectiveRoutes 注册长期目标的显式命令和只读查询端点。
func RegisterObjectiveRoutes(mux *http.ServeMux, store *storage.ObjectiveStore) {
	handler := &objectiveHandler{store: store}
	mux.HandleFunc("GET /api/objective", handler.getCurrent)
	mux.HandleFunc("POST /api/objectives", handler.create)
	mux.HandleFunc("GET /api/objectives/{objectiveID}", handler.get)
	mux.HandleFunc("GET /api/objectives/{objectiveID}/checkpoints", handler.listCheckpoints)
	mux.HandleFunc("POST /api/objectives/{objectiveID}/checkpoints", handler.appendCheckpoint)
	mux.HandleFunc("POST /api/objectives/{objectiveID}/{command}", handler.command)
}

func (handler *objectiveHandler) getCurrent(response http.ResponseWriter, request *http.Request) {
	current, err := handler.store.GetCurrent(request.Context())
	if errors.Is(err, storage.ErrObjectiveNotFound) {
		writeJSON(response, http.StatusOK, nil)
		return
	}
	if err != nil {
		writeObjectiveError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, current)
}

func (handler *objectiveHandler) create(response http.ResponseWriter, request *http.Request) {
	var input objective.CreateInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的长期目标")
		return
	}
	created, err := handler.store.Create(request.Context(), input)
	if err != nil {
		writeObjectiveError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (handler *objectiveHandler) get(response http.ResponseWriter, request *http.Request) {
	loaded, err := handler.store.Get(request.Context(), request.PathValue("objectiveID"))
	if err != nil {
		writeObjectiveError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, loaded)
}

func (handler *objectiveHandler) listCheckpoints(response http.ResponseWriter, request *http.Request) {
	items, err := handler.store.ListCheckpoints(request.Context(), request.PathValue("objectiveID"))
	if err != nil {
		writeObjectiveError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (handler *objectiveHandler) appendCheckpoint(response http.ResponseWriter, request *http.Request) {
	var input objectiveCheckpointRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的目标检查点")
		return
	}
	updated, err := handler.store.AppendCheckpoint(
		request.Context(), request.PathValue("objectiveID"), input.ExpectedVersion, input.CheckpointInput,
	)
	if err != nil {
		writeObjectiveError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, updated)
}

func (handler *objectiveHandler) command(response http.ResponseWriter, request *http.Request) {
	command, valid := objectiveCommand(request.PathValue("command"))
	if !valid {
		writeError(response, http.StatusNotFound, "OBJECTIVE_COMMAND_NOT_FOUND", "长期目标命令不存在")
		return
	}
	var input objectiveCommandRequest
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的长期目标命令")
		return
	}
	updated, err := handler.store.ApplyCommand(request.Context(), request.PathValue("objectiveID"), input.ExpectedVersion, command)
	if err != nil {
		writeObjectiveError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, updated)
}

func objectiveCommand(value string) (objective.Command, bool) {
	switch value {
	case "pause":
		return objective.CommandPause, true
	case "resume":
		return objective.CommandResume, true
	case "achieve":
		return objective.CommandAchieve, true
	case "cancel":
		return objective.CommandCancel, true
	default:
		return "", false
	}
}

func writeObjectiveError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrObjectiveNotFound):
		writeError(response, http.StatusNotFound, "OBJECTIVE_NOT_FOUND", "长期目标不存在")
	case errors.Is(err, storage.ErrTopicNotFound):
		writeError(response, http.StatusNotFound, "TOPIC_NOT_FOUND", "根 Topic 不存在")
	case errors.Is(err, storage.ErrActiveObjectiveExists):
		writeError(response, http.StatusConflict, "ACTIVE_OBJECTIVE_EXISTS", "当前项目已有活跃或暂停的长期目标")
	case errors.Is(err, storage.ErrObjectiveVersionConflict):
		writeError(response, http.StatusConflict, "OBJECTIVE_VERSION_CONFLICT", "长期目标已更新，请刷新后重试")
	case errors.Is(err, storage.ErrObjectiveNotReady):
		writeError(response, http.StatusConflict, "OBJECTIVE_NOT_READY", "完成条件、待处理问题或关联 Task 尚未全部满足")
	case errors.Is(err, storage.ErrObjectiveStateConflict):
		writeError(response, http.StatusConflict, "OBJECTIVE_STATE_CONFLICT", "当前状态不能执行该命令")
	case errors.Is(err, storage.ErrInvalidObjectiveInput):
		writeError(response, http.StatusBadRequest, "INVALID_OBJECTIVE", "长期目标或检查点内容无效")
	default:
		writeError(response, http.StatusInternalServerError, "OBJECTIVE_OPERATION_FAILED", "长期目标操作失败")
	}
}
