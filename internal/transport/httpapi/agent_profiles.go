package httpapi

import (
	"errors"
	"net/http"
	"os/exec"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/storage"
)

type agentProfileHandler struct {
	store    *storage.AgentProfileStore
	lookPath func(string) (string, error)
}

// RegisterAgentProfileRoutes 注册 Agent 配置与不可变修订端点。
func RegisterAgentProfileRoutes(mux *http.ServeMux, store *storage.AgentProfileStore, probes ...func(string) (string, error)) {
	probe := exec.LookPath
	if len(probes) > 0 && probes[0] != nil {
		probe = probes[0]
	}
	handler := &agentProfileHandler{store: store, lookPath: probe}
	mux.HandleFunc("GET /api/agent-profiles", handler.list)
	mux.HandleFunc("POST /api/agent-profiles/configure-codex", handler.configureCodex)
	mux.HandleFunc("GET /api/agent-profiles/{profileID}/revisions", handler.listRevisions)
	mux.HandleFunc("POST /api/agent-profiles/{profileID}/revisions", handler.createRevision)
}

func (handler *agentProfileHandler) configureCodex(response http.ResponseWriter, request *http.Request) {
	command, err := handler.lookPath("codex")
	if err != nil {
		writeError(response, http.StatusConflict, "CODEX_NOT_FOUND", "当前 PATH 中找不到 codex 命令")
		return
	}
	profiles, err := handler.store.ConfigureCodexDefaults(request.Context(), command)
	if err != nil {
		writeAgentProfileError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, profiles)
}

func (handler *agentProfileHandler) list(response http.ResponseWriter, request *http.Request) {
	profiles, err := handler.store.List(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "AGENT_PROFILES_READ_FAILED", "读取 Agent 配置失败")
		return
	}
	writeJSON(response, http.StatusOK, profiles)
}

func (handler *agentProfileHandler) listRevisions(response http.ResponseWriter, request *http.Request) {
	revisions, err := handler.store.ListRevisions(request.Context(), request.PathValue("profileID"))
	if err != nil {
		writeAgentProfileError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, revisions)
}

func (handler *agentProfileHandler) createRevision(response http.ResponseWriter, request *http.Request) {
	var input agentprofile.RevisionInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 Agent 配置")
		return
	}
	updated, err := handler.store.CreateRevision(request.Context(), request.PathValue("profileID"), input)
	if err != nil {
		writeAgentProfileError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, updated)
}

func writeAgentProfileError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrAgentProfileNotFound):
		writeError(response, http.StatusNotFound, "AGENT_PROFILE_NOT_FOUND", "Agent Profile 不存在")
	case errors.Is(err, storage.ErrAgentProfileRevisionConflict):
		writeError(response, http.StatusConflict, "AGENT_PROFILE_REVISION_CONFLICT", "Agent 配置已更新，请重新加载")
	default:
		writeError(response, http.StatusBadRequest, "INVALID_AGENT_PROFILE", err.Error())
	}
}
