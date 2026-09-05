// Package gitworkflow 管理 Task worktree 与本地 Release Tag 的安全 Git 操作。
package gitworkflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/light-speak/aitodos/internal/domain/release"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/workspace"
	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/storage"
)

var (
	ErrTagConflict          = errors.New("git tag already points to another commit or is not annotated")
	ErrWorkspaceIdentity    = errors.New("workspace git identity mismatch")
	ErrWorkspaceDirty       = errors.New("workspace has uncommitted changes")
	ErrWorkspaceClean       = errors.New("workspace has no changes to commit")
	ErrRepositoryUnborn     = errors.New("repository has no initial commit")
	ErrTargetBranchInvalid  = errors.New("target branch is invalid")
	ErrTargetBranchNotFound = errors.New("target branch does not exist locally")
	ErrTargetNeedsSync      = errors.New("task branch must sync with target branch")
	ErrTargetWorktreeBusy   = errors.New("target branch is checked out in another worktree")
	ErrRepositoryDirty      = errors.New("target working tree has uncommitted changes")
	ErrGitOperationActive   = errors.New("git operation is already in progress")
	ErrTaskNotAccepted      = errors.New("task is not accepted")
	ErrReviewCommitMissing  = errors.New("accepted task has no review commit")
	ErrReviewHeadMismatch   = errors.New("workspace head does not match accepted review")
	ErrTargetSyncNotNeeded  = errors.New("target branch does not need synchronization")
)

// Branch 保存本地 Branch 名称和当前 Commit。
type Branch struct {
	Name    string `json:"name"`
	HeadSHA string `json:"head_sha"`
}

// Remote 保存脱敏后的 Git Remote 地址。
type Remote struct {
	Name     string `json:"name"`
	FetchURL string `json:"fetch_url"`
	PushURL  string `json:"push_url"`
}

// SubmitTaskReview 将可执行 Task 送入人工验收，不伪造 Agent Run。
func (manager *Manager) SubmitTaskReview(ctx context.Context, taskID string, version int64) (task.Task, error) {
	return manager.tasks.ApplyCommand(ctx, taskID, version, task.CommandSubmitReview)
}

// CommitTaskWorkspace 显式提交当前 Task worktree 中的全部修改。
func (manager *Manager) CommitTaskWorkspace(ctx context.Context, taskID, message string) (workspace.Workspace, error) {
	message = strings.TrimSpace(message)
	if message == "" || len(message) > 500 {
		return workspace.Workspace{}, errors.New("commit message must contain 1 to 500 characters")
	}
	currentTask, err := manager.tasks.Get(ctx, taskID)
	if err != nil {
		return workspace.Workspace{}, err
	}
	if currentTask.Status != task.StatusReview {
		return workspace.Workspace{}, &task.TransitionError{Current: currentTask.Status, Command: task.CommandSubmitReview}
	}
	item, err := manager.TaskWorkspace(ctx, taskID)
	if err != nil {
		return workspace.Workspace{}, err
	}
	if item == nil || !item.Dirty {
		return workspace.Workspace{}, ErrWorkspaceClean
	}
	lock, err := acquireRepositoryLock(ctx, filepath.Join(manager.project.Paths.Runtime, "git.lock"))
	if err != nil {
		return workspace.Workspace{}, err
	}
	defer lock.Close()
	if _, err := manager.gitOutput(ctx, item.Path, "add", "--all"); err != nil {
		return workspace.Workspace{}, err
	}
	if _, err := manager.gitOutput(ctx, item.Path, "commit", "-m", message); err != nil {
		return workspace.Workspace{}, err
	}
	return manager.refreshWorkspace(ctx, *item)
}

// ReviewTask 保存人工验收，并把验收时的 workspace HEAD 固化到 Review。
func (manager *Manager) ReviewTask(
	ctx context.Context,
	taskID string,
	version int64,
	input task.ReviewInput,
) (task.Task, task.Review, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return task.Task{}, task.Review{}, err
	}
	current, err := manager.tasks.Get(ctx, taskID)
	if err != nil {
		return task.Task{}, task.Review{}, err
	}
	if current.Version != version {
		return task.Task{}, task.Review{}, storage.ErrTaskVersionConflict
	}
	if _, err := task.Transition(current.Status, input.Command()); err != nil {
		return task.Task{}, task.Review{}, err
	}
	if input.Decision == task.ReviewAccepted {
		if err := manager.quality.EnsureRequiredTestsPassed(ctx, taskID); err != nil {
			return task.Task{}, task.Review{}, err
		}
	}
	commitSHA := ""
	item, err := manager.TaskWorkspace(ctx, taskID)
	if err != nil {
		return task.Task{}, task.Review{}, err
	}
	if item != nil {
		if input.Decision == task.ReviewAccepted && item.Dirty {
			committed, commitErr := manager.CommitTaskWorkspace(ctx, taskID, defaultTaskCommitMessage(current))
			if commitErr != nil {
				return task.Task{}, task.Review{}, commitErr
			}
			item = &committed
		}
		commitSHA = item.HeadSHA
	}
	return manager.tasks.ApplyReview(ctx, taskID, version, input, commitSHA)
}

func defaultTaskCommitMessage(item task.Task) string {
	return fmt.Sprintf("feat: complete %s / 完成 %s", item.Key, item.Key)
}

// ListTaskReviews 返回 Task 的人工验收历史。
func (manager *Manager) ListTaskReviews(ctx context.Context, taskID string) ([]task.Review, error) {
	return manager.tasks.ListReviews(ctx, taskID)
}

// RepositoryInfo 是 UI 创建 Release 前需要的当前 Git 事实。
type RepositoryInfo struct {
	Root                string   `json:"root"`
	GitCommonDir        string   `json:"git_common_dir"`
	GitVersion          string   `json:"git_version"`
	DefaultBranch       string   `json:"default_branch"`
	RemoteDefaultBranch string   `json:"remote_default_branch,omitempty"`
	CurrentBranch       string   `json:"current_branch"`
	HeadSHA             string   `json:"head_sha"`
	HasHead             bool     `json:"has_head"`
	Dirty               bool     `json:"dirty"`
	ExactTag            string   `json:"exact_tag,omitempty"`
	Upstream            string   `json:"upstream,omitempty"`
	Ahead               *int     `json:"ahead,omitempty"`
	Behind              *int     `json:"behind,omitempty"`
	UserName            string   `json:"user_name,omitempty"`
	UserEmail           string   `json:"user_email,omitempty"`
	IdentityConfigured  bool     `json:"identity_configured"`
	Branches            []Branch `json:"branches"`
	Remotes             []Remote `json:"remotes"`
}

// Manager 串行执行当前仓库共享 Git 元数据操作。
type Manager struct {
	project      *project.Project
	tasks        *storage.TaskStore
	quality      *storage.QualityStore
	workspaces   *storage.WorkspaceStore
	releases     *storage.ReleaseStore
	integrations *storage.IntegrationStore
}

// New 创建 Git Workflow Manager。
func New(currentProject *project.Project, database *sql.DB) *Manager {
	return &Manager{
		project: currentProject, tasks: storage.NewTaskStore(database), quality: storage.NewQualityStore(database),
		workspaces: storage.NewWorkspaceStore(database), releases: storage.NewReleaseStore(database),
		integrations: storage.NewIntegrationStore(database),
	}
}

// CreateTaskWorkspace 为 Task 创建或恢复一个长期 linked worktree。
func (manager *Manager) CreateTaskWorkspace(ctx context.Context, taskID string) (workspace.Workspace, error) {
	lock, err := acquireRepositoryLock(ctx, filepath.Join(manager.project.Paths.Runtime, "git.lock"))
	if err != nil {
		return workspace.Workspace{}, err
	}
	defer lock.Close()
	item, err := manager.tasks.Get(ctx, taskID)
	if err != nil {
		return workspace.Workspace{}, err
	}
	if _, hasHead, headErr := manager.optionalHead(ctx); headErr != nil {
		return workspace.Workspace{}, headErr
	} else if !hasHead {
		return workspace.Workspace{}, ErrRepositoryUnborn
	}
	targetBranch := item.TargetBranch
	if targetBranch == "" {
		targetBranch = manager.project.Config.DefaultBranch
	}
	baseSHA, err := manager.resolveBranch(ctx, targetBranch)
	if err != nil {
		return workspace.Workspace{}, err
	}
	path, err := manager.workspacePath(taskID)
	if err != nil {
		return workspace.Workspace{}, err
	}
	branchName := taskBranchName(manager.project.Config.Name, item.Key, taskID)
	reserved, err := manager.workspaces.Reserve(ctx, taskID, path, branchName, targetBranch, baseSHA)
	if err != nil {
		return workspace.Workspace{}, err
	}
	if reserved.State == workspace.StateReady || reserved.State == workspace.StateDirty {
		return manager.refreshWorkspace(ctx, reserved)
	}
	return manager.provisionWorkspace(ctx, reserved)
}

// ValidateTargetBranch 校验目标是当前仓库中已有 Commit 的本地 Branch。
func (manager *Manager) ValidateTargetBranch(ctx context.Context, targetBranch string) error {
	_, err := manager.resolveBranch(ctx, strings.TrimSpace(targetBranch))
	return err
}

// UpdateTaskTargetBranch 在 Workspace 创建前更新并锁定下一次执行的基线分支。
func (manager *Manager) UpdateTaskTargetBranch(
	ctx context.Context,
	taskID string,
	expectedVersion int64,
	targetBranch string,
) (task.Task, error) {
	input := task.UpdateTargetBranchInput{TargetBranch: targetBranch}.Normalized()
	if err := input.Validate(); err != nil {
		return task.Task{}, err
	}
	lock, err := acquireRepositoryLock(ctx, filepath.Join(manager.project.Paths.Runtime, "git.lock"))
	if err != nil {
		return task.Task{}, err
	}
	defer lock.Close()
	if err := manager.ValidateTargetBranch(ctx, input.TargetBranch); err != nil {
		return task.Task{}, err
	}
	return manager.tasks.UpdateTargetBranch(ctx, taskID, expectedVersion, input.TargetBranch)
}

// TaskWorkspace 返回并重新校验 Task 当前 Workspace；尚未创建时返回 nil。
func (manager *Manager) TaskWorkspace(ctx context.Context, taskID string) (*workspace.Workspace, error) {
	item, err := manager.workspaces.GetByTask(ctx, taskID)
	if errors.Is(err, storage.ErrWorkspaceNotFound) {
		if _, taskErr := manager.tasks.Get(ctx, taskID); taskErr != nil {
			return nil, taskErr
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if item.State != workspace.StateReady && item.State != workspace.StateDirty {
		return &item, nil
	}
	lock, err := acquireRepositoryLock(ctx, filepath.Join(manager.project.Paths.Runtime, "git.lock"))
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	refreshed, err := manager.refreshWorkspace(ctx, item)
	if err != nil {
		return nil, err
	}
	return &refreshed, nil
}

// RepositoryInfo 读取当前仓库分支、HEAD、dirty 和精确 Tag。
func (manager *Manager) RepositoryInfo(ctx context.Context) (RepositoryInfo, error) {
	currentBranch, err := manager.gitOutput(ctx, manager.project.Root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		currentBranch = ""
	}
	headSHA, hasHead, err := manager.optionalHead(ctx)
	if err != nil {
		return RepositoryInfo{}, err
	}
	status, err := manager.gitOutput(ctx, manager.project.Root, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return RepositoryInfo{}, err
	}
	branches, err := manager.listBranches(ctx)
	if err != nil {
		return RepositoryInfo{}, err
	}
	remotes, err := manager.listRemotes(ctx)
	if err != nil {
		return RepositoryInfo{}, err
	}
	upstream, ahead, behind := manager.trackingInfo(ctx)
	exactTag, _ := manager.gitOutput(ctx, manager.project.Root, "describe", "--tags", "--exact-match", "HEAD")
	gitVersion, _ := manager.gitOutput(ctx, manager.project.Root, "--version")
	userName, _ := manager.gitOutput(ctx, manager.project.Root, "config", "--get", "user.name")
	userEmail, _ := manager.gitOutput(ctx, manager.project.Root, "config", "--get", "user.email")
	return RepositoryInfo{
		Root: manager.project.Root, GitCommonDir: manager.project.GitCommonDir, GitVersion: gitVersion,
		DefaultBranch: manager.project.Config.DefaultBranch, RemoteDefaultBranch: remoteDefaultBranch(ctx, manager, remotes),
		CurrentBranch: currentBranch, HeadSHA: headSHA, HasHead: hasHead, Dirty: status != "",
		ExactTag: exactTag, Upstream: upstream, Ahead: ahead, Behind: behind,
		UserName: userName, UserEmail: userEmail, IdentityConfigured: userName != "" && userEmail != "",
		Branches: branches, Remotes: remotes,
	}, nil
}

func (manager *Manager) trackingInfo(ctx context.Context) (string, *int, *int) {
	upstream, err := manager.gitOutput(ctx, manager.project.Root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return "", nil, nil
	}
	counts, err := manager.gitOutput(ctx, manager.project.Root, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err != nil {
		return upstream, nil, nil
	}
	var ahead, behind int
	if _, err := fmt.Sscan(counts, &ahead, &behind); err != nil {
		return upstream, nil, nil
	}
	return upstream, &ahead, &behind
}

func (manager *Manager) listRemotes(ctx context.Context) ([]Remote, error) {
	output, err := manager.gitOutput(ctx, manager.project.Root, "remote")
	if err != nil {
		return nil, err
	}
	remotes := make([]Remote, 0)
	for _, name := range strings.Fields(output) {
		fetchURL, fetchErr := manager.gitOutput(ctx, manager.project.Root, "remote", "get-url", name)
		pushURL, pushErr := manager.gitOutput(ctx, manager.project.Root, "remote", "get-url", "--push", name)
		if fetchErr != nil || pushErr != nil {
			return nil, fmt.Errorf("read remote %q urls", name)
		}
		remotes = append(remotes, Remote{Name: name, FetchURL: sanitizeRemoteURL(fetchURL), PushURL: sanitizeRemoteURL(pushURL)})
	}
	return remotes, nil
}

func remoteDefaultBranch(ctx context.Context, manager *Manager, remotes []Remote) string {
	names := make([]string, 0, len(remotes))
	for _, remote := range remotes {
		names = append(names, remote.Name)
	}
	sort.SliceStable(names, func(left, right int) bool { return names[left] == "origin" && names[right] != "origin" })
	for _, name := range names {
		ref, err := manager.gitOutput(ctx, manager.project.Root, "symbolic-ref", "--quiet", "--short", "refs/remotes/"+name+"/HEAD")
		if err == nil {
			return strings.TrimPrefix(ref, name+"/")
		}
	}
	return ""
}

func sanitizeRemoteURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return value
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func (manager *Manager) optionalHead(ctx context.Context) (string, bool, error) {
	command := exec.CommandContext(ctx, "git", "-C", manager.project.Root, "rev-parse", "--verify", "--quiet", "HEAD")
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(output)), true, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return "", false, nil
	}
	return "", false, fmt.Errorf("read repository HEAD: %w: %s", err, strings.TrimSpace(string(output)))
}

// CreateRelease 创建可恢复的 Release 记录和本地 annotated tag。
func (manager *Manager) CreateRelease(ctx context.Context, input release.CreateInput) (release.Release, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return release.Release{}, err
	}
	lock, err := acquireRepositoryLock(ctx, filepath.Join(manager.project.Paths.Runtime, "git.lock"))
	if err != nil {
		return release.Release{}, err
	}
	defer lock.Close()
	commitSHA, err := manager.resolveBranch(ctx, input.SourceBranch)
	if err != nil {
		return release.Release{}, err
	}
	inferredTaskIDs, err := manager.inferReleaseTaskIDs(ctx, commitSHA)
	if err != nil {
		return release.Release{}, err
	}
	input.TaskIDs = append(input.TaskIDs, inferredTaskIDs...)
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return release.Release{}, err
	}
	reserved, err := manager.releases.Reserve(ctx, input, commitSHA)
	if err != nil {
		return release.Release{}, err
	}
	if err := manager.ensureAnnotatedTag(ctx, reserved.TagName, reserved.CommitSHA); err != nil {
		_ = manager.releases.MarkFailed(ctx, reserved.ID, err.Error())
		return release.Release{}, err
	}
	if reserved.Status == release.StatusTagged {
		return reserved, nil
	}
	return manager.releases.MarkTagged(ctx, reserved.ID)
}

func (manager *Manager) inferReleaseTaskIDs(ctx context.Context, commitSHA string) ([]string, error) {
	tasks, err := manager.tasks.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0)
	for _, item := range tasks {
		if item.Status != task.StatusAccepted {
			continue
		}
		reviews, reviewErr := manager.tasks.ListReviews(ctx, item.ID)
		if reviewErr != nil {
			return nil, reviewErr
		}
		for _, review := range reviews {
			if review.Decision != task.ReviewAccepted || review.CommitSHA == "" {
				continue
			}
			reachable, ancestorErr := manager.isAncestor(ctx, review.CommitSHA, commitSHA)
			if ancestorErr != nil {
				return nil, ancestorErr
			}
			if reachable {
				result = append(result, item.ID)
			}
			break
		}
	}
	sort.Strings(result)
	return result, nil
}

func (manager *Manager) isAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	command := exec.CommandContext(ctx, "git", "-C", manager.project.Root, "merge-base", "--is-ancestor", ancestor, descendant)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	err := command.Run()
	if err == nil {
		return true, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("verify release commit ancestry: %w", err)
}

// ListReleases 返回当前项目发布历史。
func (manager *Manager) ListReleases(ctx context.Context) ([]release.Release, error) {
	return manager.releases.List(ctx)
}

func (manager *Manager) provisionWorkspace(ctx context.Context, item workspace.Workspace) (workspace.Workspace, error) {
	if _, err := os.Stat(item.Path); err == nil {
		return manager.refreshWorkspace(ctx, item)
	} else if !errors.Is(err, os.ErrNotExist) {
		return workspace.Workspace{}, fmt.Errorf("inspect workspace path: %w", err)
	}
	branchExists, err := manager.refExists(ctx, "refs/heads/"+item.BranchName)
	if err != nil {
		return workspace.Workspace{}, err
	}
	if branchExists {
		err := fmt.Errorf("workspace branch %q exists without managed worktree", item.BranchName)
		_ = manager.workspaces.MarkError(ctx, item.ID, err.Error())
		return workspace.Workspace{}, err
	}
	if _, err := manager.gitOutput(
		ctx, manager.project.Root, "worktree", "add", "-b", item.BranchName, item.Path, item.BaseCommitSHA,
	); err != nil {
		_ = manager.workspaces.MarkError(ctx, item.ID, err.Error())
		return workspace.Workspace{}, err
	}
	return manager.refreshWorkspace(ctx, item)
}

func (manager *Manager) refreshWorkspace(ctx context.Context, item workspace.Workspace) (workspace.Workspace, error) {
	if err := manager.verifyWorkspaceIdentity(ctx, item); err != nil {
		_ = manager.workspaces.MarkQuarantined(ctx, item.ID, err.Error())
		return workspace.Workspace{}, err
	}
	headSHA, err := manager.gitOutput(ctx, item.Path, "rev-parse", "HEAD")
	if err != nil {
		return workspace.Workspace{}, err
	}
	status, err := manager.gitOutput(ctx, item.Path, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return workspace.Workspace{}, err
	}
	return manager.workspaces.MarkReady(ctx, item.ID, headSHA, status != "")
}

func (manager *Manager) verifyWorkspaceIdentity(ctx context.Context, item workspace.Workspace) error {
	expectedPath, err := manager.workspacePath(item.TaskID)
	if err != nil || filepath.Clean(item.Path) != expectedPath {
		return fmt.Errorf("%w: unexpected managed path", ErrWorkspaceIdentity)
	}
	resolvedPath, err := filepath.EvalSymlinks(item.Path)
	if err != nil || filepath.Clean(resolvedPath) != expectedPath {
		return fmt.Errorf("%w: managed path resolves outside workspace root", ErrWorkspaceIdentity)
	}
	commonDir, err := manager.gitOutput(ctx, item.Path, "rev-parse", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWorkspaceIdentity, err)
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(item.Path, commonDir)
	}
	resolvedCommonDir, err := filepath.EvalSymlinks(commonDir)
	if err != nil || filepath.Clean(resolvedCommonDir) != manager.project.GitCommonDir {
		return fmt.Errorf("%w: unexpected git common dir", ErrWorkspaceIdentity)
	}
	branch, err := manager.gitOutput(ctx, item.Path, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch != item.BranchName {
		return fmt.Errorf("%w: expected branch %q", ErrWorkspaceIdentity, item.BranchName)
	}
	return nil
}

func (manager *Manager) workspacePath(taskID string) (string, error) {
	root, err := filepath.EvalSymlinks(manager.project.Paths.Worktrees)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	path := filepath.Join(root, taskID)
	if filepath.Dir(path) != root {
		return "", errors.New("workspace path escapes managed root")
	}
	return path, nil
}

func (manager *Manager) resolveBranch(ctx context.Context, branch string) (string, error) {
	if branch == "" {
		return "", fmt.Errorf("%w: branch is not configured", ErrTargetBranchInvalid)
	}
	if _, err := manager.gitOutput(ctx, manager.project.Root, "check-ref-format", "--branch", branch); err != nil {
		return "", fmt.Errorf("%w: %q: %v", ErrTargetBranchInvalid, branch, err)
	}
	commit, err := manager.gitOutput(ctx, manager.project.Root, "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%w: %q: %v", ErrTargetBranchNotFound, branch, err)
	}
	return commit, nil
}

func (manager *Manager) listBranches(ctx context.Context) ([]Branch, error) {
	output, err := manager.gitOutput(
		ctx, manager.project.Root, "for-each-ref", "--format=%(refname:short)%00%(objectname)", "refs/heads",
	)
	if err != nil {
		return nil, err
	}
	branches := make([]Branch, 0)
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 2)
		if len(parts) == 2 {
			branches = append(branches, Branch{Name: parts[0], HeadSHA: parts[1]})
		}
	}
	sort.Slice(branches, func(left, right int) bool { return branches[left].Name < branches[right].Name })
	return branches, nil
}

func (manager *Manager) ensureAnnotatedTag(ctx context.Context, tagName, commitSHA string) error {
	exists, err := manager.refExists(ctx, "refs/tags/"+tagName)
	if err != nil {
		return err
	}
	if exists {
		objectType, typeErr := manager.gitOutput(ctx, manager.project.Root, "cat-file", "-t", "refs/tags/"+tagName)
		tagCommit, commitErr := manager.gitOutput(ctx, manager.project.Root, "rev-parse", "refs/tags/"+tagName+"^{commit}")
		if typeErr != nil || commitErr != nil || objectType != "tag" || tagCommit != commitSHA {
			return ErrTagConflict
		}
		return nil
	}
	_, err = manager.gitOutput(
		ctx, manager.project.Root, "tag", "-a", tagName, commitSHA, "-m", "Release "+tagName,
	)
	return err
}

func (manager *Manager) refExists(ctx context.Context, ref string) (bool, error) {
	command := exec.CommandContext(ctx, "git", "-C", manager.project.Root, "show-ref", "--verify", "--quiet", ref)
	err := command.Run()
	if err == nil {
		return true, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("inspect git ref %q: %w", ref, err)
}

func (manager *Manager) gitOutput(ctx context.Context, directory string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", directory}, args...)
	command := exec.CommandContext(ctx, "git", commandArgs...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func taskBranchName(projectName, taskKey, taskID string) string {
	projectSlug := slug(projectName)
	if projectSlug == "" {
		projectSlug = "project"
	}
	return fmt.Sprintf("aitodos/%s/%s-%s", projectSlug, taskKey, shortIdentifier(taskID))
}

func shortIdentifier(value string) string {
	compact := strings.ReplaceAll(value, "-", "")
	if len(compact) > 8 {
		return compact[:8]
	}
	return compact
}

func slug(value string) string {
	var result strings.Builder
	for _, character := range strings.ToLower(value) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			result.WriteRune(character)
			continue
		}
		if result.Len() > 0 && !strings.HasSuffix(result.String(), "-") {
			result.WriteByte('-')
		}
	}
	return strings.Trim(result.String(), "-")
}
