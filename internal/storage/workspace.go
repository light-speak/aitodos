package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/light-speak/aitodos/internal/domain/workspace"
)

var ErrWorkspaceNotFound = errors.New("workspace not found")

// WorkspaceStore 持久化 Task Workspace 身份和校验状态。
type WorkspaceStore struct {
	database *sql.DB
}

// NewWorkspaceStore 创建 Workspace 持久化服务。
func NewWorkspaceStore(database *sql.DB) *WorkspaceStore {
	return &WorkspaceStore{database: database}
}

// GetByTask 返回 Task 当前 Workspace。
func (store *WorkspaceStore) GetByTask(ctx context.Context, taskID string) (workspace.Workspace, error) {
	return getWorkspace(ctx, store.database, taskID)
}

// Reserve 幂等保留正在创建的 Workspace 记录。
func (store *WorkspaceStore) Reserve(
	ctx context.Context,
	taskID, path, branchName, targetBranch, baseCommitSHA string,
) (workspace.Workspace, error) {
	existing, err := store.GetByTask(ctx, taskID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrWorkspaceNotFound) {
		return workspace.Workspace{}, err
	}
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return workspace.Workspace{}, err
	}
	id, err := newID()
	if err != nil {
		return workspace.Workspace{}, err
	}
	now := time.Now().UTC()
	created := workspace.Workspace{
		ID: id, TaskID: taskID, Path: path, BranchName: branchName,
		TargetBranch: targetBranch, BaseCommitSHA: baseCommitSHA,
		State: workspace.StateProvisioning, CreatedAt: now, UpdatedAt: now,
	}
	_, err = store.database.ExecContext(ctx, `
INSERT INTO workspaces(
    id, task_id, path, branch_name, target_branch, base_commit_sha,
    head_sha, state, dirty, failure_message, last_verified_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, '', ?, 0, '', NULL, ?, ?)`,
		created.ID, created.TaskID, created.Path, created.BranchName,
		created.TargetBranch, created.BaseCommitSHA, created.State,
		formatTime(now), formatTime(now),
	)
	if err != nil {
		return workspace.Workspace{}, fmt.Errorf("reserve workspace: %w", err)
	}
	return created, nil
}

// MarkReady 原子保存校验后的 Git 状态并关联 Task。
func (store *WorkspaceStore) MarkReady(ctx context.Context, id, headSHA string, dirty bool) (workspace.Workspace, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return workspace.Workspace{}, fmt.Errorf("begin workspace ready: %w", err)
	}
	defer transaction.Rollback()
	current, err := getWorkspaceByID(ctx, transaction, id)
	if err != nil {
		return workspace.Workspace{}, err
	}
	now := time.Now().UTC()
	state := workspace.StateReady
	if dirty {
		state = workspace.StateDirty
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE workspaces
SET head_sha = ?, state = ?, dirty = ?, failure_message = '', last_verified_at = ?, updated_at = ?
WHERE id = ?`, headSHA, state, boolInt(dirty), formatTime(now), formatTime(now), id); err != nil {
		return workspace.Workspace{}, fmt.Errorf("mark workspace ready: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE tasks
SET target_branch = ?, base_commit_sha = ?, current_workspace_id = ?, version = version + 1, updated_at = ?
WHERE id = ?
  AND (target_branch != ? OR base_commit_sha != ? OR current_workspace_id != ?)`,
		current.TargetBranch, current.BaseCommitSHA, current.ID, formatTime(now), current.TaskID,
		current.TargetBranch, current.BaseCommitSHA, current.ID,
	); err != nil {
		return workspace.Workspace{}, fmt.Errorf("link task workspace: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return workspace.Workspace{}, fmt.Errorf("commit workspace ready: %w", err)
	}
	return store.GetByTask(ctx, current.TaskID)
}

// MarkError 保存 Workspace 创建失败，供人工诊断或安全恢复。
func (store *WorkspaceStore) MarkError(ctx context.Context, id, message string) error {
	_, err := store.database.ExecContext(ctx, `
UPDATE workspaces SET state = ?, failure_message = ?, updated_at = ? WHERE id = ?`,
		workspace.StateError, message, formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("mark workspace error: %w", err)
	}
	return nil
}

// MarkQuarantined 保存无法证明 Git 身份一致的 Workspace。
func (store *WorkspaceStore) MarkQuarantined(ctx context.Context, id, message string) error {
	_, err := store.database.ExecContext(ctx, `
UPDATE workspaces SET state = ?, failure_message = ?, updated_at = ? WHERE id = ?`,
		workspace.StateQuarantined, message, formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("quarantine workspace: %w", err)
	}
	return nil
}

func getWorkspace(ctx context.Context, queryer rowQueryer, taskID string) (workspace.Workspace, error) {
	return scanWorkspace(queryer.QueryRowContext(ctx, `
SELECT id, task_id, path, branch_name, target_branch, base_commit_sha, head_sha,
       state, dirty, failure_message, last_verified_at, created_at, updated_at
FROM workspaces WHERE task_id = ?`, taskID))
}

func getWorkspaceByID(ctx context.Context, queryer rowQueryer, id string) (workspace.Workspace, error) {
	return scanWorkspace(queryer.QueryRowContext(ctx, `
SELECT id, task_id, path, branch_name, target_branch, base_commit_sha, head_sha,
       state, dirty, failure_message, last_verified_at, created_at, updated_at
FROM workspaces WHERE id = ?`, id))
}

func scanWorkspace(scanner rowScanner) (workspace.Workspace, error) {
	var item workspace.Workspace
	var dirty int
	var lastVerifiedAt sql.NullString
	var createdAt, updatedAt string
	err := scanner.Scan(
		&item.ID, &item.TaskID, &item.Path, &item.BranchName, &item.TargetBranch,
		&item.BaseCommitSHA, &item.HeadSHA, &item.State, &dirty, &item.FailureMessage,
		&lastVerifiedAt, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return workspace.Workspace{}, ErrWorkspaceNotFound
	}
	if err != nil {
		return workspace.Workspace{}, fmt.Errorf("scan workspace: %w", err)
	}
	item.Dirty = dirty == 1
	if lastVerifiedAt.Valid {
		parsed, parseErr := parseTime(lastVerifiedAt.String)
		err = parseErr
		if err != nil {
			return workspace.Workspace{}, fmt.Errorf("parse workspace verification time: %w", err)
		}
		item.LastVerifiedAt = &parsed
	}
	item.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return workspace.Workspace{}, err
	}
	item.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return workspace.Workspace{}, err
	}
	return item, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
