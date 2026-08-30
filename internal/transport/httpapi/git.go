package httpapi

import (
	"errors"
	"net/http"

	"github.com/light-speak/aitodos/internal/domain/integration"
	"github.com/light-speak/aitodos/internal/domain/release"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/gitworkflow"
	"github.com/light-speak/aitodos/internal/storage"
)

type gitWorkflowHandler struct {
	manager *gitworkflow.Manager
}

type commandRequest struct {
	Version int64 `json:"version"`
}

// RegisterGitWorkflowRoutes 注册 Task Workspace 和本地 Release 端点。
func RegisterGitWorkflowRoutes(mux *http.ServeMux, manager *gitworkflow.Manager) {
	handler := &gitWorkflowHandler{manager: manager}
	mux.HandleFunc("GET /api/git", handler.repositoryInfo)
	mux.HandleFunc("GET /api/releases", handler.listReleases)
	mux.HandleFunc("POST /api/releases", handler.createRelease)
	mux.HandleFunc("GET /api/tasks/{taskID}/workspace", handler.taskWorkspace)
	mux.HandleFunc("POST /api/tasks/{taskID}/workspace", handler.createTaskWorkspace)
	mux.HandleFunc("PUT /api/tasks/{taskID}/target-branch", handler.updateTaskTargetBranch)
	mux.HandleFunc("GET /api/tasks/{taskID}/changes", handler.taskChanges)
	mux.HandleFunc("GET /api/tasks/{taskID}/changes/file", handler.taskFileDiff)
	mux.HandleFunc("POST /api/tasks/{taskID}/submit-review", handler.submitReview)
	mux.HandleFunc("POST /api/tasks/{taskID}/workspace/commit", handler.commitWorkspace)
	mux.HandleFunc("GET /api/tasks/{taskID}/reviews", handler.listReviews)
	mux.HandleFunc("POST /api/tasks/{taskID}/reviews", handler.reviewTask)
	mux.HandleFunc("GET /api/tasks/{taskID}/integration", handler.taskIntegration)
	mux.HandleFunc("POST /api/tasks/{taskID}/integration", handler.integrateTask)
	mux.HandleFunc("POST /api/tasks/{taskID}/integration/sync", handler.syncTaskTarget)
}

func (handler *gitWorkflowHandler) taskIntegration(response http.ResponseWriter, request *http.Request) {
	item, err := handler.manager.TaskIntegration(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeGitWorkflowError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (handler *gitWorkflowHandler) integrateTask(response http.ResponseWriter, request *http.Request) {
	item, err := handler.manager.IntegrateTask(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeGitWorkflowError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (handler *gitWorkflowHandler) syncTaskTarget(response http.ResponseWriter, request *http.Request) {
	updated, attempt, err := handler.manager.SyncTaskTarget(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeGitWorkflowError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, struct {
		Task        task.Task           `json:"task"`
		Integration integration.Attempt `json:"integration"`
	}{Task: updated, Integration: attempt})
}

func (handler *gitWorkflowHandler) updateTaskTargetBranch(response http.ResponseWriter, request *http.Request) {
	var input struct {
		TargetBranch    string `json:"target_branch"`
		ExpectedVersion int64  `json:"expected_version"`
	}
	if err := decodeJSON(response, request, &input); err != nil || input.ExpectedVersion < 1 {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的目标分支更新")
		return
	}
	updated, err := handler.manager.UpdateTaskTargetBranch(
		request.Context(), request.PathValue("taskID"), input.ExpectedVersion, input.TargetBranch,
	)
	if err != nil {
		writeGitWorkflowError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, updated)
}

func (handler *gitWorkflowHandler) taskChanges(response http.ResponseWriter, request *http.Request) {
	changes, err := handler.manager.TaskChanges(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeGitWorkflowError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, changes)
}

func (handler *gitWorkflowHandler) taskFileDiff(response http.ResponseWriter, request *http.Request) {
	patch, err := handler.manager.TaskFileDiff(request.Context(), request.PathValue("taskID"), request.URL.Query().Get("path"))
	if err != nil {
		writeGitWorkflowError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, patch)
}

func (handler *gitWorkflowHandler) submitReview(response http.ResponseWriter, request *http.Request) {
	var input commandRequest
	if err := decodeJSON(response, request, &input); err != nil || input.Version < 1 {
		writeError(response, http.StatusBadRequest, "INVALID_COMMAND", "version 必须是正整数")
		return
	}
	updated, err := handler.manager.SubmitTaskReview(request.Context(), request.PathValue("taskID"), input.Version)
	if err != nil {
		writeGitWorkflowError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, updated)
}

func (handler *gitWorkflowHandler) commitWorkspace(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Message string `json:"message"`
	}
	if err := decodeJSON(response, request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 Commit")
		return
	}
	updated, err := handler.manager.CommitTaskWorkspace(request.Context(), request.PathValue("taskID"), input.Message)
	if err != nil {
		writeGitWorkflowError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, updated)
}

func (handler *gitWorkflowHandler) listReviews(response http.ResponseWriter, request *http.Request) {
	reviews, err := handler.manager.ListTaskReviews(request.Context(), request.PathValue("taskID"))
	if err != nil {
		writeGitWorkflowError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, reviews)
}

func (handler *gitWorkflowHandler) reviewTask(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Version  int64               `json:"version"`
		Decision task.ReviewDecision `json:"decision"`
		Comment  string              `json:"comment"`
	}
	if err := decodeJSON(response, request, &input); err != nil || input.Version < 1 {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "请求内容不是有效的 Review")
		return
	}
	reviewInput := task.ReviewInput{Decision: input.Decision, Comment: input.Comment}.Normalized()
	if err := reviewInput.Validate(); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REVIEW", "要求修改时必须填写原因；说明最多 5000 字符")
		return
	}
	updated, review, err := handler.manager.ReviewTask(request.Context(), request.PathValue("taskID"), input.Version, reviewInput)
	if err != nil {
		writeGitWorkflowError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, struct {
		Task   task.Task   `json:"task"`
		Review task.Review `json:"review"`
	}{Task: updated, Review: review})
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
	case errors.Is(err, gitworkflow.ErrWorkspaceDirty):
		writeError(response, http.StatusConflict, "WORKSPACE_DIRTY", "Workspace 还有未提交修改，请先查看并创建 Commit")
	case errors.Is(err, gitworkflow.ErrWorkspaceClean):
		writeError(response, http.StatusConflict, "WORKSPACE_CLEAN", "Workspace 没有可提交的修改")
	case errors.Is(err, gitworkflow.ErrRepositoryUnborn):
		writeError(response, http.StatusConflict, "REPOSITORY_UNBORN", "仓库尚无 Commit；请先创建首个 Commit")
	case errors.Is(err, gitworkflow.ErrTargetBranchInvalid), errors.Is(err, gitworkflow.ErrTargetBranchNotFound):
		writeError(response, http.StatusBadRequest, "INVALID_TARGET_BRANCH", "目标分支必须是当前仓库中已有 Commit 的本地分支")
	case errors.Is(err, storage.ErrTaskWorkspaceExists):
		writeError(response, http.StatusConflict, "TARGET_BRANCH_LOCKED", "Task 已创建 Workspace，目标分支不可再修改")
	case errors.Is(err, gitworkflow.ErrChangeNotFound):
		writeError(response, http.StatusNotFound, "CHANGE_NOT_FOUND", "该文件不在 Task 变更清单中")
	case errors.Is(err, storage.ErrTaskVersionConflict):
		writeError(response, http.StatusConflict, "TASK_VERSION_CONFLICT", "Task 已被更新，请刷新后重试")
	case errors.Is(err, storage.ErrRequiredTestsNotPassed):
		writeError(response, http.StatusConflict, "REQUIRED_TESTS_NOT_PASSED", "必测项尚未全部获得可验证的通过证据")
	case errors.Is(err, gitworkflow.ErrTargetNeedsSync):
		writeError(response, http.StatusConflict, "TARGET_NEEDS_SYNC", "目标分支已经前进，请先同步 Task Workspace 并重新验证")
	case errors.Is(err, gitworkflow.ErrTargetWorktreeBusy):
		writeError(response, http.StatusConflict, "TARGET_WORKTREE_BUSY", "目标分支正在其他 Worktree 中使用，AiTodos 不会修改不受管工作目录")
	case errors.Is(err, gitworkflow.ErrRepositoryDirty):
		writeError(response, http.StatusConflict, "TARGET_WORKTREE_DIRTY", "目标分支工作目录存在未提交修改，请先处理后重试")
	case errors.Is(err, gitworkflow.ErrGitOperationActive):
		writeError(response, http.StatusConflict, "GIT_OPERATION_ACTIVE", "仓库存在未完成的 Git 操作，请先处理后重试")
	case errors.Is(err, gitworkflow.ErrTaskNotAccepted), errors.Is(err, gitworkflow.ErrReviewCommitMissing), errors.Is(err, gitworkflow.ErrReviewHeadMismatch):
		writeError(response, http.StatusConflict, "TASK_NOT_INTEGRATABLE", "Task 必须已验收，且 Workspace HEAD 必须与最新通过 Review 一致")
	case errors.Is(err, gitworkflow.ErrTargetSyncNotNeeded):
		writeError(response, http.StatusConflict, "TARGET_SYNC_NOT_NEEDED", "目标分支当前不需要同步，请直接执行集成或刷新状态")
	default:
		var transitionErr *task.TransitionError
		if errors.As(err, &transitionErr) {
			writeError(response, http.StatusConflict, "INVALID_TASK_TRANSITION", transitionErr.Error())
			return
		}
		writeError(response, http.StatusConflict, "GIT_OPERATION_FAILED", "Git 操作失败；请确认分支存在，且本地已配置 Git 用户信息")
	}
}
