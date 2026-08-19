package httpapi

import (
	"errors"
	"net/http"

	"github.com/light-speak/aitodos/internal/domain/release"
	"github.com/light-speak/aitodos/internal/gitworkflow"
	"github.com/light-speak/aitodos/internal/storage"
)

type gitWorkflowHandler struct {
	manager *gitworkflow.Manager
}

// RegisterGitWorkflowRoutes 注册 Task Workspace 和本地 Release 端点。
func RegisterGitWorkflowRoutes(mux *http.ServeMux, manager *gitworkflow.Manager) {
	handler := &gitWorkflowHandler{manager: manager}
	mux.HandleFunc("GET /api/git", handler.repositoryInfo)
	mux.HandleFunc("GET /api/releases", handler.listReleases)
	mux.HandleFunc("POST /api/releases", handler.createRelease)
	mux.HandleFunc("GET /api/tasks/{taskID}/workspace", handler.taskWorkspace)
	mux.HandleFunc("POST /api/tasks/{taskID}/workspace", handler.createTaskWorkspace)
}

func (handler *gitWorkflowHandler) repositoryInfo(response http.ResponseWriter, request *http.Request) {
	info, err := handler.manager.RepositoryInfo(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "GIT_READ_FAILED", "读取 Git 仓库状态失败")
		return
	}
	writeJSON(response, http.StatusOK, info)
}

func (handler *gitWorkflowHandler) listReleases(response http.ResponseWriter, request *http.Request) {
	releases, err := handler.manager.ListReleases(request.Context())
	if err != nil {
		writeError(response, http.StatusInternalServerError, "RELEASE_LIST_FAILED", "读取 Release 历史失败")
		return
	}
	writeJSON(response, http.StatusOK, releases)
}

func (handler *gitWorkflowHandler) createRelease(response http.ResponseWriter, request *http.Request) {
	var input release.CreateInput
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 Release")
		return
	}
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_RELEASE", "版本必须是 SemVer，且必须选择本地来源分支")
		return
	}
	created, err := handler.manager.CreateRelease(request.Context(), input)
	if err != nil {
		writeGitWorkflowError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (handler *gitWorkflowHandler) taskWorkspace(response http.ResponseWriter, request *http.Request) {
	item, err := handler.manager.TaskWorkspace(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeGitWorkflowError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (handler *gitWorkflowHandler) createTaskWorkspace(response http.ResponseWriter, request *http.Request) {
	item, err := handler.manager.CreateTaskWorkspace(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeGitWorkflowError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func writeGitWorkflowError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrTaskNotFound):
		writeError(response, http.StatusNotFound, "TASK_NOT_FOUND", "Task 不存在")
	case errors.Is(err, storage.ErrReleaseConflict), errors.Is(err, gitworkflow.ErrTagConflict):
		writeError(response, http.StatusConflict, "RELEASE_CONFLICT", "该版本已绑定其他分支、Commit、Task 或 Git Tag")
	case errors.Is(err, gitworkflow.ErrWorkspaceIdentity):
		writeError(response, http.StatusConflict, "WORKSPACE_QUARANTINED", "Workspace 的路径、仓库或分支身份不一致，已隔离")
	default:
		writeError(response, http.StatusConflict, "GIT_OPERATION_FAILED", "Git 操作失败；请确认分支存在，且本地已配置 Git 用户信息")
	}
}
