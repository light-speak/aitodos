package gitworkflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/light-speak/aitodos/internal/domain/integration"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/workspace"
)

type integrationInput struct {
	Task      task.Task
	Review    task.Review
	Workspace workspace.Workspace
	TargetSHA string
}

// TaskIntegration 返回最近一次目标分支集成或同步尝试。
func (manager *Manager) TaskIntegration(ctx context.Context, taskID string) (*integration.Attempt, error) {
	if _, err := manager.tasks.Get(ctx, taskID); err != nil {
		return nil, err
	}
	return manager.integrations.LatestForTask(ctx, taskID)
}

// RecoverIntegrations 对账 daemon 崩溃时遗留的短 Git 操作。
func (manager *Manager) RecoverIntegrations(ctx context.Context) error {
	running, err := manager.integrations.ListRunning(ctx)
	if err != nil {
		return err
	}
	for _, attempt := range running {
		if err := manager.recoverIntegration(ctx, attempt); err != nil {
			return err
		}
	}
	return nil
}

func (manager *Manager) recoverIntegration(ctx context.Context, attempt integration.Attempt) error {
	lock, err := acquireRepositoryLock(ctx, filepath.Join(manager.project.Paths.Runtime, "git.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	if attempt.Operation == integration.OperationIntegrate {
		targetSHA, resolveErr := manager.resolveBranch(ctx, attempt.TargetBranch)
		if resolveErr != nil {
			_, _ = manager.integrations.Complete(ctx, attempt.ID, integration.StatusFailed, "", "", "RECOVERY_FAILED", resolveErr.Error())
			return nil
		}
		contained, ancestorErr := manager.isAncestor(ctx, attempt.SourceCommitSHA, targetSHA)
		if ancestorErr != nil {
			return ancestorErr
		}
		if contained {
			_, err = manager.integrations.Complete(ctx, attempt.ID, integration.StatusSucceeded, targetSHA, attempt.SourceCommitSHA, "", "")
			return err
		}
		_, err = manager.integrations.Complete(ctx, attempt.ID, integration.StatusFailed, targetSHA, "", "INTERRUPTED", "集成中断且目标分支未包含验收 Commit")
		return err
	}
	item, workspaceErr := manager.workspaces.GetByTask(ctx, attempt.TaskID)
	if workspaceErr != nil {
		_, _ = manager.integrations.Complete(ctx, attempt.ID, integration.StatusFailed, "", "", "RECOVERY_FAILED", workspaceErr.Error())
		return nil
	}
	if manager.gitOperationExists(ctx, item.Path, "MERGE_HEAD") {
		if _, abortErr := manager.gitOutput(ctx, item.Path, "merge", "--abort"); abortErr != nil {
			_ = manager.workspaces.MarkQuarantined(ctx, item.ID, abortErr.Error())
			_, _ = manager.integrations.Complete(ctx, attempt.ID, integration.StatusFailed, "", "", "ABORT_FAILED", abortErr.Error())
			return nil
		}
		_, _, err = manager.integrations.CompleteSync(ctx, attempt.ID, integration.StatusConflict, attempt.SourceCommitSHA, "MERGE_CONFLICT", "恢复时撤销未完成同步，等待 Revision Agent")
		return err
	}
	headSHA, headErr := manager.gitOutput(ctx, item.Path, "rev-parse", "HEAD")
	if headErr != nil {
		return headErr
	}
	targetIncluded, ancestorErr := manager.isAncestor(ctx, attempt.TargetBeforeSHA, headSHA)
	if ancestorErr != nil {
		return ancestorErr
	}
	if targetIncluded && headSHA != attempt.SourceCommitSHA {
		_, _, err = manager.integrations.CompleteSync(ctx, attempt.ID, integration.StatusSynced, headSHA, "", "")
		return err
	}
	_, err = manager.integrations.Complete(ctx, attempt.ID, integration.StatusFailed, "", headSHA, "INTERRUPTED", "同步中断且 Workspace 未形成可证明的合并 Commit")
	return err
}

// IntegrateTask 把已验收 Commit 显式 fast-forward 到目标分支。
func (manager *Manager) IntegrateTask(ctx context.Context, taskID string) (integration.Attempt, error) {
	input, err := manager.integrationInput(ctx, taskID)
	if err != nil {
		return integration.Attempt{}, err
	}
	lock, err := acquireRepositoryLock(ctx, filepath.Join(manager.project.Paths.Runtime, "git.lock"))
	if err != nil {
		return integration.Attempt{}, err
	}
	defer lock.Close()
	input.TargetSHA, err = manager.resolveBranch(ctx, input.Workspace.TargetBranch)
	if err != nil {
		return integration.Attempt{}, err
	}
	containsSource, err := manager.isAncestor(ctx, input.Review.CommitSHA, input.TargetSHA)
	if err != nil {
		return integration.Attempt{}, err
	}
	if containsSource {
		if latest, latestErr := manager.integrations.LatestForTask(ctx, taskID); latestErr == nil &&
			latest != nil && latest.Status == integration.StatusSucceeded &&
			latest.SourceCommitSHA == input.Review.CommitSHA {
			return *latest, nil
		}
		return manager.recordContainedIntegration(ctx, input)
	}
	targetIsAncestor, err := manager.isAncestor(ctx, input.TargetSHA, input.Review.CommitSHA)
	if err != nil {
		return integration.Attempt{}, err
	}
	if !targetIsAncestor {
		attempt, reserveErr := manager.reserveIntegration(ctx, input, integration.OperationIntegrate)
		if reserveErr != nil {
			return integration.Attempt{}, reserveErr
		}
		completed, completeErr := manager.integrations.Complete(
			ctx, attempt.ID, integration.StatusNeedsSync, input.TargetSHA,
			input.Workspace.HeadSHA, "TARGET_DIVERGED", ErrTargetNeedsSync.Error(),
		)
		if completeErr != nil {
			return integration.Attempt{}, completeErr
		}
		return completed, ErrTargetNeedsSync
	}
	attempt, err := manager.reserveIntegration(ctx, input, integration.OperationIntegrate)
	if err != nil {
		return integration.Attempt{}, err
	}
	if err := manager.fastForwardTarget(ctx, input); err != nil {
		_, _ = manager.integrations.Complete(ctx, attempt.ID, integration.StatusFailed, "", input.Workspace.HeadSHA, "GIT_FAILED", err.Error())
		return integration.Attempt{}, err
	}
	return manager.integrations.Complete(
		ctx, attempt.ID, integration.StatusSucceeded, input.Review.CommitSHA,
		input.Workspace.HeadSHA, "", "",
	)
}

// SyncTaskTarget 把分叉后的目标分支合入 Task Workspace，并要求重新 Revision 和验收。
func (manager *Manager) SyncTaskTarget(ctx context.Context, taskID string) (task.Task, integration.Attempt, error) {
	input, err := manager.integrationInput(ctx, taskID)
	if err != nil {
		return task.Task{}, integration.Attempt{}, err
	}
	lock, err := acquireRepositoryLock(ctx, filepath.Join(manager.project.Paths.Runtime, "git.lock"))
	if err != nil {
		return task.Task{}, integration.Attempt{}, err
	}
	defer lock.Close()
	input.TargetSHA, err = manager.resolveBranch(ctx, input.Workspace.TargetBranch)
	if err != nil {
		return task.Task{}, integration.Attempt{}, err
	}
	targetIsAncestor, err := manager.isAncestor(ctx, input.TargetSHA, input.Review.CommitSHA)
	if err != nil {
		return task.Task{}, integration.Attempt{}, err
	}
	sourceIsAncestor, err := manager.isAncestor(ctx, input.Review.CommitSHA, input.TargetSHA)
	if err != nil {
		return task.Task{}, integration.Attempt{}, err
	}
	if targetIsAncestor || sourceIsAncestor {
		return task.Task{}, integration.Attempt{}, ErrTargetSyncNotNeeded
	}
	attempt, err := manager.reserveIntegration(ctx, input, integration.OperationSync)
	if err != nil {
		return task.Task{}, integration.Attempt{}, err
	}
	if _, err := manager.gitOutput(ctx, input.Workspace.Path, "merge", "--no-edit", input.TargetSHA); err != nil {
		if manager.gitOperationExists(ctx, input.Workspace.Path, "MERGE_HEAD") {
			if _, abortErr := manager.gitOutput(ctx, input.Workspace.Path, "merge", "--abort"); abortErr != nil {
				_, _ = manager.integrations.Complete(ctx, attempt.ID, integration.StatusFailed, "", "", "ABORT_FAILED", abortErr.Error())
				_ = manager.workspaces.MarkQuarantined(ctx, input.Workspace.ID, abortErr.Error())
				return task.Task{}, integration.Attempt{}, abortErr
			}
			updated, completed, completeErr := manager.integrations.CompleteSync(
				ctx, attempt.ID, integration.StatusConflict, input.Workspace.HeadSHA,
				"MERGE_CONFLICT", "目标分支存在冲突，Revision Agent 必须重新合并并解决冲突",
			)
			return updated, completed, completeErr
		}
		_, _ = manager.integrations.Complete(ctx, attempt.ID, integration.StatusFailed, "", input.Workspace.HeadSHA, "GIT_FAILED", err.Error())
		return task.Task{}, integration.Attempt{}, err
	}
	workspaceAfter, err := manager.gitOutput(ctx, input.Workspace.Path, "rev-parse", "HEAD")
	if err != nil {
		_, _ = manager.integrations.Complete(ctx, attempt.ID, integration.StatusFailed, "", "", "HEAD_READ_FAILED", err.Error())
		return task.Task{}, integration.Attempt{}, err
	}
	updated, completed, err := manager.integrations.CompleteSync(
		ctx, attempt.ID, integration.StatusSynced, workspaceAfter, "", "",
	)
	if err != nil {
		return task.Task{}, integration.Attempt{}, err
	}
	if _, err := manager.refreshWorkspace(ctx, input.Workspace); err != nil {
		return task.Task{}, integration.Attempt{}, err
	}
	return updated, completed, nil
}

func (manager *Manager) integrationInput(ctx context.Context, taskID string) (integrationInput, error) {
	current, err := manager.tasks.Get(ctx, taskID)
	if err != nil {
		return integrationInput{}, err
	}
	if current.Status != task.StatusAccepted {
		return integrationInput{}, ErrTaskNotAccepted
	}
	reviews, err := manager.tasks.ListReviews(ctx, taskID)
	if err != nil {
		return integrationInput{}, err
	}
	var accepted task.Review
	for _, review := range reviews {
		if review.Decision == task.ReviewAccepted {
			accepted = review
			break
		}
	}
	if accepted.ID == "" || accepted.CommitSHA == "" {
		return integrationInput{}, ErrReviewCommitMissing
	}
	item, err := manager.TaskWorkspace(ctx, taskID)
	if err != nil {
		return integrationInput{}, err
	}
	if item == nil {
		return integrationInput{}, ErrReviewCommitMissing
	}
	if item.Dirty {
		return integrationInput{}, ErrWorkspaceDirty
	}
	if item.HeadSHA != accepted.CommitSHA {
		return integrationInput{}, ErrReviewHeadMismatch
	}
	targetSHA, err := manager.resolveBranch(ctx, item.TargetBranch)
	if err != nil {
		return integrationInput{}, err
	}
	return integrationInput{Task: current, Review: accepted, Workspace: *item, TargetSHA: targetSHA}, nil
}

func (manager *Manager) reserveIntegration(ctx context.Context, input integrationInput, operation integration.Operation) (integration.Attempt, error) {
	return manager.integrations.Reserve(ctx, integration.ReserveInput{
		TaskID: input.Task.ID, ReviewID: input.Review.ID, Operation: operation,
		TargetBranch: input.Workspace.TargetBranch, SourceCommitSHA: input.Review.CommitSHA,
		TargetBeforeSHA: input.TargetSHA,
	})
}

func (manager *Manager) recordContainedIntegration(ctx context.Context, input integrationInput) (integration.Attempt, error) {
	attempt, err := manager.reserveIntegration(ctx, input, integration.OperationIntegrate)
	if err != nil {
		return integration.Attempt{}, err
	}
	return manager.integrations.Complete(
		ctx, attempt.ID, integration.StatusSucceeded, input.TargetSHA,
		input.Workspace.HeadSHA, "", "",
	)
}

func (manager *Manager) fastForwardTarget(ctx context.Context, input integrationInput) error {
	path, err := manager.checkedOutBranchPath(ctx, input.Workspace.TargetBranch)
	if err != nil {
		return err
	}
	if path == "" {
		_, err = manager.gitOutput(
			ctx, manager.project.Root, "update-ref", "refs/heads/"+input.Workspace.TargetBranch,
			input.Review.CommitSHA, input.TargetSHA,
		)
		return err
	}
	if filepath.Clean(path) != filepath.Clean(manager.project.Root) {
		return ErrTargetWorktreeBusy
	}
	status, err := manager.gitOutput(ctx, path, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return err
	}
	if status != "" {
		return ErrRepositoryDirty
	}
	if manager.anyGitOperationExists(ctx, path) {
		return ErrGitOperationActive
	}
	_, err = manager.gitOutput(ctx, path, "merge", "--ff-only", input.Review.CommitSHA)
	return err
}

func (manager *Manager) checkedOutBranchPath(ctx context.Context, branch string) (string, error) {
	output, err := manager.gitOutput(ctx, manager.project.Root, "worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}
	path := ""
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			path = strings.TrimPrefix(line, "worktree ")
			continue
		}
		if line == "branch refs/heads/"+branch {
			return path, nil
		}
	}
	return "", nil
}

func (manager *Manager) anyGitOperationExists(ctx context.Context, path string) bool {
	for _, name := range []string{"MERGE_HEAD", "REBASE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG"} {
		if manager.gitOperationExists(ctx, path, name) {
			return true
		}
	}
	return false
}

func (manager *Manager) gitOperationExists(ctx context.Context, path, name string) bool {
	gitPath, err := manager.gitOutput(ctx, path, "rev-parse", "--git-path", name)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(gitPath) {
		gitPath = filepath.Join(path, gitPath)
	}
	_, err = os.Stat(gitPath)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

func integrationFailure(kind string, err error) error {
	return fmt.Errorf("%s: %w", kind, err)
}
