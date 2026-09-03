package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domainrun "github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
)

var (
	// ErrTaskNotFound 表示指定 Task 不存在。
	ErrTaskNotFound = errors.New("task not found")
	// ErrTaskVersionConflict 表示写入基于过期的 Task 版本。
	ErrTaskVersionConflict = errors.New("task version conflict")
	// ErrRequiredTestsNotPassed 表示必测项尚无可验证的通过证据。
	ErrRequiredTestsNotPassed = errors.New("required tests not passed")
	// ErrTaskWorkspaceExists 表示 Task 已绑定 Workspace，目标分支不可再修改。
	ErrTaskWorkspaceExists = errors.New("task workspace already exists")
	// ErrTaskRetryRequiresAnswer 表示 Task 正等待结构化 Clarification，不能绕过回答直接重试。
	ErrTaskRetryRequiresAnswer = errors.New("task retry requires clarification answer")
	// ErrTaskArchiveState 表示只有已验收或已取消的 Task 可以归档。
	ErrTaskArchiveState = errors.New("task must be accepted or cancelled before archive")
	// ErrTaskEditState 表示当前执行状态不允许修改需求。
	ErrTaskEditState = errors.New("task cannot be edited in current state")
)

const taskColumns = `
id, task_key, title, title_source, title_locked, description, acceptance_criteria, status, priority,
target_branch, base_commit_sha, current_workspace_id, latest_run_id,
COALESCE(source_plan_revision_id, ''), COALESCE(source_plan_task_draft_id, ''),
assessment_input_version, version, created_at, updated_at, COALESCE(archived_at, '')`

// TaskStore 使用 SQLite 持久化 Task 当前状态和审计事件。
type TaskStore struct {
	database *sql.DB
}

// NewTaskStore 创建 Task 持久化服务。
func NewTaskStore(database *sql.DB) *TaskStore {
	return &TaskStore{database: database}
}

// Create 原子创建 Task 及其首条审计事件。
func (store *TaskStore) Create(ctx context.Context, input task.CreateInput) (task.Task, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return task.Task{}, err
	}

	created, event, err := newTaskAndEvent(input)
	if err != nil {
		return task.Task{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return task.Task{}, fmt.Errorf("begin task creation: %w", err)
	}
	defer transaction.Rollback()

	if err := insertTask(ctx, transaction, created); err != nil {
		return task.Task{}, err
	}
	if err := insertTaskEvent(ctx, transaction, event); err != nil {
		return task.Task{}, err
	}
	if err := transaction.Commit(); err != nil {
		return task.Task{}, fmt.Errorf("commit task creation: %w", err)
	}
	return created, nil
}

// Get 根据 ID 读取 Task。
func (store *TaskStore) Get(ctx context.Context, id string) (task.Task, error) {
	return getTask(ctx, store.database, id)
}

// UpdateTitle 人工更新并锁定标题，后续 Triage 只能保留该标题。
func (store *TaskStore) UpdateTitle(
	ctx context.Context,
	id string,
	expectedVersion int64,
	input task.UpdateTitleInput,
) (task.Task, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return task.Task{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return task.Task{}, fmt.Errorf("begin task title update: %w", err)
	}
	defer transaction.Rollback()
	current, err := getTask(ctx, transaction, id)
	if err != nil {
		return task.Task{}, err
	}
	if current.Version != expectedVersion {
		return task.Task{}, ErrTaskVersionConflict
	}
	updated, event, err := prepareTitleUpdate(current, input.Title, task.TitleSourceHuman, true)
	if err != nil {
		return task.Task{}, err
	}
	if err := updateTaskTitle(ctx, transaction, current, updated); err != nil {
		return task.Task{}, err
	}
	if err := insertTaskEvent(ctx, transaction, event); err != nil {
		return task.Task{}, err
	}
	if err := transaction.Commit(); err != nil {
		return task.Task{}, fmt.Errorf("commit task title update: %w", err)
	}
	return updated, nil
}

// UpdateDetails 原子修订描述、验收标准和优先级，并使旧评估失效。
func (store *TaskStore) UpdateDetails(ctx context.Context, id string, expectedVersion int64, input task.UpdateDetailsInput) (task.Task, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return task.Task{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return task.Task{}, fmt.Errorf("begin task details update: %w", err)
	}
	defer transaction.Rollback()
	current, err := getTask(ctx, transaction, id)
	if err != nil {
		return task.Task{}, err
	}
	if current.Version != expectedVersion {
		return task.Task{}, ErrTaskVersionConflict
	}
	if current.Status == task.StatusRunning || current.Status == task.StatusCancelled {
		return task.Task{}, ErrTaskEditState
	}
	updated, event, err := prepareDetailsUpdate(current, input)
	if err != nil {
		return task.Task{}, err
	}
	if err := updateTaskDetails(ctx, transaction, current, updated); err != nil {
		return task.Task{}, err
	}
	if err := insertTaskEvent(ctx, transaction, event); err != nil {
		return task.Task{}, err
	}
	if err := transaction.Commit(); err != nil {
		return task.Task{}, fmt.Errorf("commit task details update: %w", err)
	}
	return updated, nil
}

// Archive 将已验收或已取消的 Task 从默认工作视图隐藏，保留全部历史。
func (store *TaskStore) Archive(ctx context.Context, id string, expectedVersion int64) (task.Task, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return task.Task{}, fmt.Errorf("begin task archive: %w", err)
	}
	defer transaction.Rollback()
	current, err := getTask(ctx, transaction, id)
	if err != nil {
		return task.Task{}, err
	}
	if current.Version != expectedVersion {
		return task.Task{}, ErrTaskVersionConflict
	}
	if current.ArchivedAt != nil {
		return current, nil
	}
	if current.Status != task.StatusAccepted && current.Status != task.StatusCancelled {
		return task.Task{}, ErrTaskArchiveState
	}
	updated, event, err := prepareArchive(current)
	if err != nil {
		return task.Task{}, err
	}
	result, err := transaction.ExecContext(ctx, `UPDATE tasks SET archived_at = ?, version = ?, updated_at = ? WHERE id = ? AND version = ? AND archived_at IS NULL`,
		formatTime(*updated.ArchivedAt), updated.Version, formatTime(updated.UpdatedAt), current.ID, current.Version)
	if err != nil {
		return task.Task{}, fmt.Errorf("archive task: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return task.Task{}, ErrTaskVersionConflict
	}
	if err := insertTaskEvent(ctx, transaction, event); err != nil {
		return task.Task{}, err
	}
	if err := transaction.Commit(); err != nil {
		return task.Task{}, fmt.Errorf("commit task archive: %w", err)
	}
	return updated, nil
}

// UpdateTargetBranch 在 Workspace 创建前原子更新 Task 目标分支。
func (store *TaskStore) UpdateTargetBranch(
	ctx context.Context,
	id string,
	expectedVersion int64,
	targetBranch string,
) (task.Task, error) {
	input := task.UpdateTargetBranchInput{TargetBranch: targetBranch}.Normalized()
	if err := input.Validate(); err != nil {
		return task.Task{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return task.Task{}, fmt.Errorf("begin task target branch update: %w", err)
	}
	defer transaction.Rollback()
	current, err := getTask(ctx, transaction, id)
	if err != nil {
		return task.Task{}, err
	}
	if current.Version != expectedVersion {
		return task.Task{}, ErrTaskVersionConflict
	}
	if current.CurrentWorkspaceID != "" {
		return task.Task{}, ErrTaskWorkspaceExists
	}
	if current.TargetBranch == input.TargetBranch {
		return current, nil
	}
	updated, event, err := prepareTargetBranchUpdate(current, input.TargetBranch)
	if err != nil {
		return task.Task{}, err
	}
	if err := updateTaskTargetBranch(ctx, transaction, current, updated); err != nil {
		return task.Task{}, err
	}
	if err := insertTaskEvent(ctx, transaction, event); err != nil {
		return task.Task{}, err
	}
	if err := transaction.Commit(); err != nil {
		return task.Task{}, fmt.Errorf("commit task target branch update: %w", err)
	}
	return updated, nil
}

// List 按优先级和创建时间返回当前项目的全部 Task。
func (store *TaskStore) List(ctx context.Context) ([]task.Task, error) {
	return store.list(ctx, false)
}

// ListIncludingArchived 返回包含已归档项的全部 Task。
func (store *TaskStore) ListIncludingArchived(ctx context.Context) ([]task.Task, error) {
	return store.list(ctx, true)
}

func (store *TaskStore) list(ctx context.Context, includeArchived bool) ([]task.Task, error) {
	where := " WHERE archived_at IS NULL"
	if includeArchived {
		where = ""
	}
	rows, err := store.database.QueryContext(ctx, "SELECT "+taskColumns+" FROM tasks"+where+" ORDER BY priority ASC, created_at ASC")
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	result := make([]task.Task, 0)
	for rows.Next() {
		item, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tasks: %w", err)
	}
	return result, nil
}

// ApplyCommand 使用乐观版本校验原子更新状态并追加审计事件。
func (store *TaskStore) ApplyCommand(
	ctx context.Context,
	id string,
	expectedVersion int64,
	command task.Command,
) (task.Task, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return task.Task{}, fmt.Errorf("begin task command: %w", err)
	}
	defer transaction.Rollback()

	current, err := getTask(ctx, transaction, id)
	if err != nil {
		return task.Task{}, err
	}
	if current.Version != expectedVersion {
		return task.Task{}, ErrTaskVersionConflict
	}
	next, err := task.Transition(current.Status, command)
	if err != nil {
		return task.Task{}, err
	}
	updated, event, err := prepareTransition(current, next, command)
	if err != nil {
		return task.Task{}, err
	}
	if err := updateTaskStatus(ctx, transaction, current, updated); err != nil {
		return task.Task{}, err
	}
	if err := insertTaskEvent(ctx, transaction, event); err != nil {
		return task.Task{}, err
	}
	if err := transaction.Commit(); err != nil {
		return task.Task{}, fmt.Errorf("commit task command: %w", err)
	}
	return updated, nil
}

// RetryBlocked 将失败或取消的 Task 按最近一次 Run purpose 放回正确队列。
func (store *TaskStore) RetryBlocked(ctx context.Context, id string, expectedVersion int64) (task.Task, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return task.Task{}, fmt.Errorf("begin retry blocked task: %w", err)
	}
	defer transaction.Rollback()
	current, err := getTask(ctx, transaction, id)
	if err != nil {
		return task.Task{}, err
	}
	if current.Version != expectedVersion {
		return task.Task{}, ErrTaskVersionConflict
	}
	var purpose, runStatus string
	err = transaction.QueryRowContext(ctx, `
SELECT purpose, status FROM runs WHERE task_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, id).Scan(&purpose, &runStatus)
	if err != nil {
		return task.Task{}, fmt.Errorf("read latest run for retry: %w", err)
	}
	if domainrun.Status(runStatus) == domainrun.StatusNeedsInput {
		return task.Task{}, ErrTaskRetryRequiresAnswer
	}
	command := task.CommandRetry
	if domainrun.Purpose(purpose) == domainrun.PurposeRevision {
		command = task.CommandResumeRevision
	}
	next, err := task.Transition(current.Status, command)
	if err != nil {
		return task.Task{}, err
	}
	updated, event, err := prepareTransition(current, next, command)
	if err != nil {
		return task.Task{}, err
	}
	if err := updateTaskStatus(ctx, transaction, current, updated); err != nil {
		return task.Task{}, err
	}
	if err := insertTaskEvent(ctx, transaction, event); err != nil {
		return task.Task{}, err
	}
	if err := transaction.Commit(); err != nil {
		return task.Task{}, fmt.Errorf("commit retry blocked task: %w", err)
	}
	return updated, nil
}

// ListEvents 按聚合内序号返回 Task 的完整审计记录。
func (store *TaskStore) ListEvents(ctx context.Context, taskID string) ([]task.Event, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, task_id, sequence, event_type, payload_json, occurred_at
FROM task_events
WHERE task_id = ?
ORDER BY sequence`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task events: %w", err)
	}
	defer rows.Close()

	events := make([]task.Event, 0)
	for rows.Next() {
		event, err := scanTaskEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task events: %w", err)
	}
	return events, nil
}

// ApplyReview 原子保存人工验收、更新 Task 状态并追加审计事件。
func (store *TaskStore) ApplyReview(
	ctx context.Context,
	id string,
	expectedVersion int64,
	input task.ReviewInput,
	commitSHA string,
) (task.Task, task.Review, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return task.Task{}, task.Review{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return task.Task{}, task.Review{}, fmt.Errorf("begin task review: %w", err)
	}
	defer transaction.Rollback()
	current, err := getTask(ctx, transaction, id)
	if err != nil {
		return task.Task{}, task.Review{}, err
	}
	if current.Version != expectedVersion {
		return task.Task{}, task.Review{}, ErrTaskVersionConflict
	}
	if input.Decision == task.ReviewAccepted {
		if err := ensureRequiredTestsPassed(ctx, transaction, id); err != nil {
			return task.Task{}, task.Review{}, err
		}
	}
	updated, event, err := prepareReviewTransition(current, input)
	if err != nil {
		return task.Task{}, task.Review{}, err
	}
	review, err := newReview(id, input, commitSHA, event.OccurredAt)
	if err != nil {
		return task.Task{}, task.Review{}, err
	}
	if err := updateTaskStatus(ctx, transaction, current, updated); err != nil {
		return task.Task{}, task.Review{}, err
	}
	if err := insertTaskEvent(ctx, transaction, event); err != nil {
		return task.Task{}, task.Review{}, err
	}
	if err := insertTaskReview(ctx, transaction, review); err != nil {
		return task.Task{}, task.Review{}, err
	}
	if err := transaction.Commit(); err != nil {
		return task.Task{}, task.Review{}, fmt.Errorf("commit task review: %w", err)
	}
	return updated, review, nil
}

// ListReviews 按时间倒序返回 Task 的人工验收历史。
func (store *TaskStore) ListReviews(ctx context.Context, taskID string) ([]task.Review, error) {
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return nil, err
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT id, task_id, decision, comment, commit_sha, created_at
FROM task_reviews WHERE task_id = ? ORDER BY created_at DESC, id DESC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task reviews: %w", err)
	}
	defer rows.Close()
	result := make([]task.Review, 0)
	for rows.Next() {
		review, scanErr := scanTaskReview(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, review)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task reviews: %w", err)
	}
	return result, nil
}

func prepareReviewTransition(current task.Task, input task.ReviewInput) (task.Task, task.Event, error) {
	return prepareTransition(current, mustReviewStatus(input), input.Command())
}

func mustReviewStatus(input task.ReviewInput) task.Status {
	if input.Decision == task.ReviewAccepted {
		return task.StatusAccepted
	}
	return task.StatusChangesRequested
}

func newReview(taskID string, input task.ReviewInput, commitSHA string, createdAt time.Time) (task.Review, error) {
	id, err := newID()
	if err != nil {
		return task.Review{}, err
	}
	return task.Review{
		ID: id, TaskID: taskID, Decision: input.Decision,
		Comment: input.Comment, CommitSHA: commitSHA, CreatedAt: createdAt,
	}, nil
}

func insertTaskReview(ctx context.Context, transaction *sql.Tx, review task.Review) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO task_reviews(id, task_id, decision, comment, commit_sha, created_at)
VALUES (?, ?, ?, ?, ?, ?)`,
		review.ID, review.TaskID, review.Decision, review.Comment, review.CommitSHA, formatTime(review.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert task review: %w", err)
	}
	return nil
}

func scanTaskReview(scanner rowScanner) (task.Review, error) {
	var review task.Review
	var createdAt string
	if err := scanner.Scan(
		&review.ID, &review.TaskID, &review.Decision, &review.Comment, &review.CommitSHA, &createdAt,
	); err != nil {
		return task.Review{}, fmt.Errorf("scan task review: %w", err)
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return task.Review{}, fmt.Errorf("parse task review time: %w", err)
	}
	review.CreatedAt = parsed
	return review, nil
}

func newTaskAndEvent(input task.CreateInput) (task.Task, task.Event, error) {
	id, err := newID()
	if err != nil {
		return task.Task{}, task.Event{}, err
	}
	eventID, err := newID()
	if err != nil {
		return task.Task{}, task.Event{}, err
	}
	now := time.Now().UTC()
	created := task.Task{
		ID: id, Key: taskKey(id), Title: input.Title,
		TitleSource: input.TitleSource, TitleLocked: input.TitleSource == task.TitleSourceHuman,
		Description: input.Description, AcceptanceCriteria: input.AcceptanceCriteria,
		Status: task.StatusReady, Priority: input.Priority, TargetBranch: input.TargetBranch,
		SourcePlanRevisionID: input.SourcePlanRevisionID, SourcePlanTaskDraftID: input.SourcePlanTaskDraftID,
		AssessmentInputVersion: 1, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1, "title": created.Title, "title_source": created.TitleSource,
	})
	if err != nil {
		return task.Task{}, task.Event{}, fmt.Errorf("encode task created event: %w", err)
	}
	event := task.Event{ID: eventID, TaskID: id, Sequence: 1, Type: task.EventCreated, Payload: payload, OccurredAt: now}
	return created, event, nil
}

func prepareTitleUpdate(
	current task.Task,
	title string,
	source task.TitleSource,
	locked bool,
) (task.Task, task.Event, error) {
	eventID, err := newID()
	if err != nil {
		return task.Task{}, task.Event{}, err
	}
	now := time.Now().UTC()
	updated := current
	updated.Title = title
	updated.TitleSource = source
	updated.TitleLocked = locked
	updated.Version++
	updated.UpdatedAt = now
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1, "from": current.Title, "to": title,
		"source": source, "locked": locked,
	})
	if err != nil {
		return task.Task{}, task.Event{}, fmt.Errorf("encode task title event: %w", err)
	}
	return updated, task.Event{
		ID: eventID, TaskID: current.ID, Sequence: updated.Version,
		Type: task.EventTitleChanged, Payload: payload, OccurredAt: now,
	}, nil
}

func prepareTargetBranchUpdate(current task.Task, targetBranch string) (task.Task, task.Event, error) {
	eventID, err := newID()
	if err != nil {
		return task.Task{}, task.Event{}, err
	}
	now := time.Now().UTC()
	updated := current
	updated.TargetBranch = targetBranch
	updated.AssessmentInputVersion++
	updated.Version++
	updated.UpdatedAt = now
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1, "from": current.TargetBranch, "to": targetBranch,
	})
	if err != nil {
		return task.Task{}, task.Event{}, fmt.Errorf("encode task target branch event: %w", err)
	}
	return updated, task.Event{
		ID: eventID, TaskID: current.ID, Sequence: updated.Version,
		Type: task.EventTargetBranchChanged, Payload: payload, OccurredAt: now,
	}, nil
}

func prepareDetailsUpdate(current task.Task, input task.UpdateDetailsInput) (task.Task, task.Event, error) {
	eventID, err := newID()
	if err != nil {
		return task.Task{}, task.Event{}, err
	}
	now := time.Now().UTC()
	updated := current
	updated.Description = input.Description
	updated.AcceptanceCriteria = input.AcceptanceCriteria
	updated.Priority = input.Priority
	if current.Status == task.StatusReview || current.Status == task.StatusAccepted || current.Status == task.StatusBlocked {
		updated.Status = task.StatusChangesRequested
	}
	updated.AssessmentInputVersion++
	updated.Version++
	updated.UpdatedAt = now
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"from":           map[string]any{"description": current.Description, "acceptance_criteria": current.AcceptanceCriteria, "priority": current.Priority, "status": current.Status},
		"to":             map[string]any{"description": updated.Description, "acceptance_criteria": updated.AcceptanceCriteria, "priority": updated.Priority, "status": updated.Status},
	})
	if err != nil {
		return task.Task{}, task.Event{}, err
	}
	return updated, task.Event{ID: eventID, TaskID: current.ID, Sequence: updated.Version, Type: task.EventDetailsChanged, Payload: payload, OccurredAt: now}, nil
}

func prepareArchive(current task.Task) (task.Task, task.Event, error) {
	eventID, err := newID()
	if err != nil {
		return task.Task{}, task.Event{}, err
	}
	now := time.Now().UTC()
	updated := current
	updated.ArchivedAt = &now
	updated.Version++
	updated.UpdatedAt = now
	payload, err := json.Marshal(map[string]any{"schema_version": 1, "archived_at": formatTime(now)})
	if err != nil {
		return task.Task{}, task.Event{}, err
	}
	return updated, task.Event{ID: eventID, TaskID: current.ID, Sequence: updated.Version, Type: task.EventArchived, Payload: payload, OccurredAt: now}, nil
}

func prepareTransition(current task.Task, next task.Status, command task.Command) (task.Task, task.Event, error) {
	eventID, err := newID()
	if err != nil {
		return task.Task{}, task.Event{}, err
	}
	now := time.Now().UTC()
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"command":        command,
		"from":           current.Status,
		"to":             next,
	})
	if err != nil {
		return task.Task{}, task.Event{}, fmt.Errorf("encode task status event: %w", err)
	}
	updated := current
	updated.Status = next
	updated.Version++
	updated.UpdatedAt = now
	event := task.Event{
		ID: eventID, TaskID: current.ID, Sequence: updated.Version,
		Type: task.EventStatusChanged, Payload: payload, OccurredAt: now,
	}
	return updated, event, nil
}

func insertTask(ctx context.Context, transaction *sql.Tx, item task.Task) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO tasks (
    id, task_key, title, title_source, title_locked, description, acceptance_criteria, status, priority,
    target_branch, base_commit_sha, current_workspace_id, latest_run_id,
    source_plan_revision_id, source_plan_task_draft_id, assessment_input_version,
    version, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.Key, item.Title, item.TitleSource, item.TitleLocked,
		item.Description, item.AcceptanceCriteria,
		item.Status, item.Priority, item.TargetBranch, item.BaseCommitSHA,
		item.CurrentWorkspaceID, item.LatestRunID, nullableString(item.SourcePlanRevisionID),
		nullableString(item.SourcePlanTaskDraftID), item.AssessmentInputVersion, item.Version,
		formatTime(item.CreatedAt), formatTime(item.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
}

func updateTaskTitle(ctx context.Context, transaction *sql.Tx, current, updated task.Task) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE tasks SET title = ?, title_source = ?, title_locked = ?, version = ?, updated_at = ?
WHERE id = ? AND version = ?`,
		updated.Title, updated.TitleSource, updated.TitleLocked, updated.Version,
		formatTime(updated.UpdatedAt), current.ID, current.Version,
	)
	if err != nil {
		return fmt.Errorf("update task title: %w", err)
	}
	return requireSingleChange(result)
}

func updateTaskTargetBranch(ctx context.Context, transaction *sql.Tx, current, updated task.Task) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE tasks SET target_branch = ?, assessment_input_version = ?, version = ?, updated_at = ?
WHERE id = ? AND version = ? AND current_workspace_id = ''`,
		updated.TargetBranch, updated.AssessmentInputVersion, updated.Version,
		formatTime(updated.UpdatedAt), current.ID, current.Version,
	)
	if err != nil {
		return fmt.Errorf("update task target branch: %w", err)
	}
	return requireSingleChange(result)
}

func updateTaskDetails(ctx context.Context, transaction *sql.Tx, current, updated task.Task) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE tasks SET description = ?, acceptance_criteria = ?, priority = ?, status = ?,
                 assessment_input_version = ?, version = ?, updated_at = ?
WHERE id = ? AND version = ? AND status != 'RUNNING' AND status != 'CANCELLED' AND archived_at IS NULL`,
		updated.Description, updated.AcceptanceCriteria, updated.Priority, updated.Status,
		updated.AssessmentInputVersion, updated.Version, formatTime(updated.UpdatedAt), current.ID, current.Version)
	if err != nil {
		return fmt.Errorf("update task details: %w", err)
	}
	return requireSingleChange(result)
}

func updateTaskStatus(ctx context.Context, transaction *sql.Tx, current, updated task.Task) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE tasks
SET status = ?, version = ?, updated_at = ?
WHERE id = ? AND status = ? AND version = ?`,
		updated.Status, updated.Version, formatTime(updated.UpdatedAt),
		current.ID, current.Status, current.Version,
	)
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read task update result: %w", err)
	}
	if count != 1 {
		return ErrTaskVersionConflict
	}
	return nil
}

func insertTaskEvent(ctx context.Context, transaction *sql.Tx, event task.Event) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO task_events (id, task_id, sequence, event_type, payload_json, occurred_at)
VALUES (?, ?, ?, ?, ?, ?)`,
		event.ID, event.TaskID, event.Sequence, event.Type, string(event.Payload), formatTime(event.OccurredAt),
	)
	if err != nil {
		return fmt.Errorf("insert task event: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func getTask(ctx context.Context, queryer rowQueryer, id string) (task.Task, error) {
	item, err := scanTask(queryer.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return task.Task{}, ErrTaskNotFound
	}
	return item, err
}

func scanTask(scanner rowScanner) (task.Task, error) {
	var item task.Task
	var createdAt, updatedAt, archivedAt string
	err := scanner.Scan(
		&item.ID, &item.Key, &item.Title, &item.TitleSource, &item.TitleLocked,
		&item.Description, &item.AcceptanceCriteria,
		&item.Status, &item.Priority, &item.TargetBranch, &item.BaseCommitSHA,
		&item.CurrentWorkspaceID, &item.LatestRunID, &item.SourcePlanRevisionID,
		&item.SourcePlanTaskDraftID, &item.AssessmentInputVersion,
		&item.Version, &createdAt, &updatedAt, &archivedAt,
	)
	if err != nil {
		return task.Task{}, err
	}
	item.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return task.Task{}, fmt.Errorf("parse task created time: %w", err)
	}
	item.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return task.Task{}, fmt.Errorf("parse task updated time: %w", err)
	}
	if archivedAt != "" {
		parsed, parseErr := parseTime(archivedAt)
		if parseErr != nil {
			return task.Task{}, fmt.Errorf("parse task archived time: %w", parseErr)
		}
		item.ArchivedAt = &parsed
	}
	return item, nil
}

func scanTaskEvent(scanner rowScanner) (task.Event, error) {
	var event task.Event
	var payload, occurredAt string
	if err := scanner.Scan(&event.ID, &event.TaskID, &event.Sequence, &event.Type, &payload, &occurredAt); err != nil {
		return task.Event{}, fmt.Errorf("scan task event: %w", err)
	}
	event.Payload = json.RawMessage(payload)
	parsed, err := parseTime(occurredAt)
	if err != nil {
		return task.Event{}, fmt.Errorf("parse task event time: %w", err)
	}
	event.OccurredAt = parsed
	return event, nil
}

func newID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate identifier: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func taskKey(id string) string {
	compact := strings.ReplaceAll(id, "-", "")
	return "ATS-" + strings.ToUpper(compact[:8])
}

func formatTime(value time.Time) string {
	return value.Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}
