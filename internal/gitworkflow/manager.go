// Package gitworkflow 管理 Task worktree 与本地 Release Tag 的安全 Git 操作。
package gitworkflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/light-speak/aitodos/internal/domain/release"
	"github.com/light-speak/aitodos/internal/domain/workspace"
	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/storage"
)

var (
	ErrTagConflict       = errors.New("git tag already points to another commit or is not annotated")
	ErrWorkspaceIdentity = errors.New("workspace git identity mismatch")
)

// Branch 保存本地 Branch 名称和当前 Commit。
type Branch struct {
	Name    string `json:"name"`
	HeadSHA string `json:"head_sha"`
}

// RepositoryInfo 是 UI 创建 Release 前需要的当前 Git 事实。
type RepositoryInfo struct {
	CurrentBranch string   `json:"current_branch"`
	HeadSHA       string   `json:"head_sha"`
	Dirty         bool     `json:"dirty"`
	ExactTag      string   `json:"exact_tag,omitempty"`
	Branches      []Branch `json:"branches"`
}

// Manager 串行执行当前仓库共享 Git 元数据操作。
type Manager struct {
	project    *project.Project
	tasks      *storage.TaskStore
	workspaces *storage.WorkspaceStore
	releases   *storage.ReleaseStore
}

// New 创建 Git Workflow Manager。
func New(currentProject *project.Project, database *sql.DB) *Manager {
	return &Manager{
		project: currentProject, tasks: storage.NewTaskStore(database),
		workspaces: storage.NewWorkspaceStore(database), releases: storage.NewReleaseStore(database),
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
	headSHA, err := manager.gitOutput(ctx, manager.project.Root, "rev-parse", "HEAD")
	if err != nil {
		return RepositoryInfo{}, fmt.Errorf("read repository HEAD: %w", err)
	}
	status, err := manager.gitOutput(ctx, manager.project.Root, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return RepositoryInfo{}, err
	}
	branches, err := manager.listBranches(ctx)
	if err != nil {
		return RepositoryInfo{}, err
	}
	exactTag, _ := manager.gitOutput(ctx, manager.project.Root, "describe", "--tags", "--exact-match", "HEAD")
	return RepositoryInfo{
		CurrentBranch: currentBranch, HeadSHA: headSHA, Dirty: status != "",
		ExactTag: exactTag, Branches: branches,
	}, nil
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
		return "", errors.New("target branch is not configured")
	}
	if _, err := manager.gitOutput(ctx, manager.project.Root, "check-ref-format", "--branch", branch); err != nil {
		return "", fmt.Errorf("invalid branch %q: %w", branch, err)
	}
	commit, err := manager.gitOutput(ctx, manager.project.Root, "rev-parse", "--verify", "refs/heads/"+branch+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve branch %q: %w", branch, err)
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
