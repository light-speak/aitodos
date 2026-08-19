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

	"github.com/light-speak/aitodos/internal/domain/task"
)

var (
	// ErrTaskNotFound 表示指定 Task 不存在。
	ErrTaskNotFound = errors.New("task not found")
	// ErrTaskVersionConflict 表示写入基于过期的 Task 版本。
	ErrTaskVersionConflict = errors.New("task version conflict")
)

const taskColumns = `
id, task_key, title, description, acceptance_criteria, status, priority,
target_branch, base_commit_sha, current_workspace_id, latest_run_id,
version, created_at, updated_at`

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

// List 按优先级和创建时间返回当前项目的全部 Task。
func (store *TaskStore) List(ctx context.Context) ([]task.Task, error) {
	rows, err := store.database.QueryContext(ctx, "SELECT "+taskColumns+" FROM tasks ORDER BY priority DESC, created_at ASC")
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
		Description: input.Description, AcceptanceCriteria: input.AcceptanceCriteria,
		Status: task.StatusBacklog, Priority: input.Priority, TargetBranch: input.TargetBranch,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	payload, err := json.Marshal(map[string]any{"schema_version": 1, "title": created.Title})
	if err != nil {
		return task.Task{}, task.Event{}, fmt.Errorf("encode task created event: %w", err)
	}
	event := task.Event{ID: eventID, TaskID: id, Sequence: 1, Type: task.EventCreated, Payload: payload, OccurredAt: now}
	return created, event, nil
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
    id, task_key, title, description, acceptance_criteria, status, priority,
    target_branch, base_commit_sha, current_workspace_id, latest_run_id,
    version, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.Key, item.Title, item.Description, item.AcceptanceCriteria,
		item.Status, item.Priority, item.TargetBranch, item.BaseCommitSHA,
		item.CurrentWorkspaceID, item.LatestRunID, item.Version,
		formatTime(item.CreatedAt), formatTime(item.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
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
	var createdAt, updatedAt string
	err := scanner.Scan(
		&item.ID, &item.Key, &item.Title, &item.Description, &item.AcceptanceCriteria,
		&item.Status, &item.Priority, &item.TargetBranch, &item.BaseCommitSHA,
		&item.CurrentWorkspaceID, &item.LatestRunID, &item.Version, &createdAt, &updatedAt,
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
