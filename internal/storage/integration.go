package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/light-speak/aitodos/internal/domain/integration"
	"github.com/light-speak/aitodos/internal/domain/task"
)

// IntegrationStore 持久化目标分支集成和同步尝试。
type IntegrationStore struct {
	database *sql.DB
}

// NewIntegrationStore 创建集成尝试 Store。
func NewIntegrationStore(database *sql.DB) *IntegrationStore {
	return &IntegrationStore{database: database}
}

// Reserve 在 Git 操作前固化不可变输入。
func (store *IntegrationStore) Reserve(ctx context.Context, input integration.ReserveInput) (integration.Attempt, error) {
	id, err := newID()
	if err != nil {
		return integration.Attempt{}, err
	}
	now := time.Now().UTC()
	created := integration.Attempt{
		ID: id, TaskID: input.TaskID, ReviewID: input.ReviewID, Operation: input.Operation,
		Status: integration.StatusRunning, TargetBranch: input.TargetBranch,
		SourceCommitSHA: input.SourceCommitSHA, TargetBeforeSHA: input.TargetBeforeSHA,
		CreatedAt: now, UpdatedAt: now,
	}
	_, err = store.database.ExecContext(ctx, `
INSERT INTO task_integration_attempts(
    id, task_id, review_id, operation, status, target_branch,
    source_commit_sha, target_before_sha, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		created.ID, created.TaskID, created.ReviewID, created.Operation, created.Status,
		created.TargetBranch, created.SourceCommitSHA, created.TargetBeforeSHA,
		formatTime(now), formatTime(now),
	)
	if err != nil {
		return integration.Attempt{}, fmt.Errorf("reserve task integration: %w", err)
	}
	return created, nil
}

// Complete 保存不改变 Task 状态的终态结果。
func (store *IntegrationStore) Complete(
	ctx context.Context,
	id string,
	status integration.Status,
	targetAfterSHA string,
	workspaceAfterSHA string,
	failureKind string,
	failureMessage string,
) (integration.Attempt, error) {
	_, err := store.database.ExecContext(ctx, `
UPDATE task_integration_attempts
SET status = ?, target_after_sha = ?, workspace_after_sha = ?,
    failure_kind = ?, failure_message = ?, updated_at = ?
WHERE id = ? AND status = 'RUNNING'`, status, targetAfterSHA, workspaceAfterSHA,
		failureKind, failureMessage, formatTime(time.Now().UTC()), id)
	if err != nil {
		return integration.Attempt{}, fmt.Errorf("complete task integration: %w", err)
	}
	return store.Get(ctx, id)
}

// CompleteSync 原子保存同步结果，并使已验收 Task 进入重新修订流程。
func (store *IntegrationStore) CompleteSync(
	ctx context.Context,
	id string,
	status integration.Status,
	workspaceAfterSHA string,
	failureKind string,
	failureMessage string,
) (task.Task, integration.Attempt, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return task.Task{}, integration.Attempt{}, fmt.Errorf("begin target sync completion: %w", err)
	}
	defer transaction.Rollback()
	attempt, err := getIntegrationAttempt(ctx, transaction, id)
	if err != nil {
		return task.Task{}, integration.Attempt{}, err
	}
	current, err := getTask(ctx, transaction, attempt.TaskID)
	if err != nil {
		return task.Task{}, integration.Attempt{}, err
	}
	next, err := task.Transition(current.Status, task.CommandSyncTarget)
	if err != nil {
		return task.Task{}, integration.Attempt{}, err
	}
	updated, event, err := prepareTransition(current, next, task.CommandSyncTarget)
	if err != nil {
		return task.Task{}, integration.Attempt{}, err
	}
	now := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE task_integration_attempts
SET status = ?, workspace_after_sha = ?, failure_kind = ?, failure_message = ?, updated_at = ?
WHERE id = ? AND status = 'RUNNING'`, status, workspaceAfterSHA, failureKind,
		failureMessage, formatTime(now), id)
	if err != nil {
		return task.Task{}, integration.Attempt{}, fmt.Errorf("complete target sync: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return task.Task{}, integration.Attempt{}, fmt.Errorf("target sync attempt is not running")
	}
	if err := updateTaskStatus(ctx, transaction, current, updated); err != nil {
		return task.Task{}, integration.Attempt{}, err
	}
	if err := insertTaskEvent(ctx, transaction, event); err != nil {
		return task.Task{}, integration.Attempt{}, err
	}
	if err := transaction.Commit(); err != nil {
		return task.Task{}, integration.Attempt{}, fmt.Errorf("commit target sync: %w", err)
	}
	completed, err := store.Get(ctx, id)
	return updated, completed, err
}

// Get 返回一个集成尝试。
func (store *IntegrationStore) Get(ctx context.Context, id string) (integration.Attempt, error) {
	return getIntegrationAttempt(ctx, store.database, id)
}

// LatestForTask 返回 Task 最近一次集成或同步尝试。
func (store *IntegrationStore) LatestForTask(ctx context.Context, taskID string) (*integration.Attempt, error) {
	row := store.database.QueryRowContext(ctx, `
SELECT id, task_id, review_id, operation, status, target_branch,
       source_commit_sha, target_before_sha, target_after_sha, workspace_after_sha,
       failure_kind, failure_message, created_at, updated_at
FROM task_integration_attempts
WHERE task_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, taskID)
	attempt, err := scanIntegrationAttempt(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &attempt, nil
}

// ListRunning 返回需要在启动时对账的未完成尝试。
func (store *IntegrationStore) ListRunning(ctx context.Context) ([]integration.Attempt, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, task_id, review_id, operation, status, target_branch,
       source_commit_sha, target_before_sha, target_after_sha, workspace_after_sha,
       failure_kind, failure_message, created_at, updated_at
FROM task_integration_attempts WHERE status = 'RUNNING' ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list running task integrations: %w", err)
	}
	defer rows.Close()
	result := make([]integration.Attempt, 0)
	for rows.Next() {
		item, scanErr := scanIntegrationAttempt(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

type integrationQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getIntegrationAttempt(ctx context.Context, queryer integrationQueryer, id string) (integration.Attempt, error) {
	return scanIntegrationAttempt(queryer.QueryRowContext(ctx, `
SELECT id, task_id, review_id, operation, status, target_branch,
       source_commit_sha, target_before_sha, target_after_sha, workspace_after_sha,
       failure_kind, failure_message, created_at, updated_at
FROM task_integration_attempts WHERE id = ?`, id))
}

func scanIntegrationAttempt(scanner rowScanner) (integration.Attempt, error) {
	var item integration.Attempt
	var createdAt, updatedAt string
	err := scanner.Scan(
		&item.ID, &item.TaskID, &item.ReviewID, &item.Operation, &item.Status,
		&item.TargetBranch, &item.SourceCommitSHA, &item.TargetBeforeSHA,
		&item.TargetAfterSHA, &item.WorkspaceAfterSHA, &item.FailureKind,
		&item.FailureMessage, &createdAt, &updatedAt,
	)
	if err != nil {
		return integration.Attempt{}, err
	}
	item.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return integration.Attempt{}, err
	}
	item.UpdatedAt, err = parseTime(updatedAt)
	return item, err
}
