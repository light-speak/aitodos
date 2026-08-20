package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/light-speak/aitodos/internal/domain/capability"
	"github.com/light-speak/aitodos/internal/storage"
)

type capabilityService interface {
	List(context.Context) (capability.Catalog, error)
	AddSkill(context.Context, capability.SkillInput) (capability.Skill, error)
	RefreshSkill(context.Context, string, int64) (capability.Skill, error)
	AddMCPServer(context.Context, capability.MCPServerInput) (capability.MCPServer, error)
}

type capabilityHandler struct {
	service capabilityService
}

// RegisterCapabilityRoutes 注册项目 Skill/MCP 目录端点。
func RegisterCapabilityRoutes(mux *http.ServeMux, service capabilityService) {
	handler := &capabilityHandler{service: service}
	mux.HandleFunc("GET /api/project/capabilities", handler.list)
	mux.HandleFunc("POST /api/project/capabilities/skills", handler.addSkill)
	mux.HandleFunc("POST /api/project/capabilities/skills/{skillID}/refresh", handler.refreshSkill)
	mux.HandleFunc("POST /api/project/capabilities/mcp-servers", handler.addMCPServer)
}

func (handler *capabilityHandler) refreshSkill(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Version int64 `json:"version"`
	}
	if err := decodeJSON(response, request, &input); err != nil || input.Version < 1 {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "Skill 版本无效")
		return
	}
	refreshed, err := handler.service.RefreshSkill(request.Context(), request.PathValue("skillID"), input.Version)
	if err != nil {
		writeCapabilityError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, refreshed)
}

func (handler *capabilityHandler) list(response http.ResponseWriter, request *http.Request) {
	catalog, err := handler.service.List(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "CAPABILITIES_READ_FAILED", "读取项目能力失败")
		return
	}
	writeJSON(response, http.StatusOK, catalog)
}

func (handler *capabilityHandler) addSkill(response http.ResponseWriter, request *http.Request) {
	var input capability.SkillInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 Skill 配置")
		return
	}
	created, err := handler.service.AddSkill(request.Context(), input)
	if err != nil {
		writeCapabilityError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (handler *capabilityHandler) addMCPServer(response http.ResponseWriter, request *http.Request) {
	var input capability.MCPServerInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 MCP 配置")
		return
	}
	created, err := handler.service.AddMCPServer(request.Context(), input)
	if err != nil {
		writeCapabilityError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func writeCapabilityError(response http.ResponseWriter, err error) {
	if errors.Is(err, storage.ErrCapabilityNotFound) {
		writeError(response, http.StatusNotFound, "CAPABILITY_NOT_FOUND", "项目能力不存在")
		return
	}
	if errors.Is(err, storage.ErrCapabilityConflict) {
		writeError(response, http.StatusConflict, "CAPABILITY_CONFLICT", "该 Skill 路径或 MCP 配置名已经登记")
		return
	}
	writeError(response, http.StatusBadRequest, "INVALID_CAPABILITY", err.Error())
}
