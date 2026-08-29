package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/domain/discussion"
	domainrun "github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/taskfeedback"
)

var (
	// ErrTaskFeedbackNotFound 表示反馈不存在。
	ErrTaskFeedbackNotFound = errors.New("task feedback not found")
	// ErrTaskFeedbackConflict 表示当前 Task 或反馈状态不允许该操作。
	ErrTaskFeedbackConflict = errors.New("task feedback conflict")
)

const taskFeedbackColumns = `
id, task_id, source_message_id, COALESCE(retry_of_feedback_id, ''), intent, status,
COALESCE(run_id, ''), COALESCE(response_message_id, ''), failure_message,
created_at, updated_at`

// TaskFeedbackStore 原子保存 Task 反馈并衔接只读问答或修订流程。
type TaskFeedbackStore struct {
	database *sql.DB
}

// NewTaskFeedbackStore 创建 Task 反馈存储。
func NewTaskFeedbackStore(database *sql.DB) *TaskFeedbackStore {
	return &TaskFeedbackStore{database: database}
}

// Discuss 保存消息并排队只读 Reviewer Run。
func (store *TaskFeedbackStore) Discuss(
	ctx context.Context,
	taskID string,
	input discussion.CreateMessageInput,
) (discussion.Message, taskfeedback.Feedback, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return discussion.Message{}, taskfeedback.Feedback{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return discussion.Message{}, taskfeedback.Feedback{}, fmt.Errorf("begin task discussion feedback: %w", err)
	}
	defer transaction.Rollback()
	if _, err := getTask(ctx, transaction, taskID); err != nil {
		return discussion.Message{}, taskfeedback.Feedback{}, err
	}
	message, err := appendHumanTaskMessage(ctx, transaction, taskID, input)
	if err != nil {
		return discussion.Message{}, taskfeedback.Feedback{}, err
	}
	feedback, err := createTaskFeedback(ctx, transaction, taskID, message.ID, "", taskfeedback.IntentDiscuss, taskfeedback.StatusQueued)
	if err != nil {
		return discussion.Message{}, taskfeedback.Feedback{}, err
	}
	if err := transaction.Commit(); err != nil {
		return discussion.Message{}, taskfeedback.Feedback{}, fmt.Errorf("commit task discussion feedback: %w", err)
	}
	return message, feedback, nil
}

// RequestChanges 保存显式修改请求并进入修订流程；已验收 Task 创建关联后续 Task。
func (store *TaskFeedbackStore) RequestChanges(
	ctx context.Context,
	taskID string,
	expectedVersion int64,
	input discussion.CreateMessageInput,
) (discussion.Message, taskfeedback.Feedback, task.Task, *task.Task, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return discussion.Message{}, taskfeedback.Feedback{}, task.Task{}, nil, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return discussion.Message{}, taskfeedback.Feedback{}, task.Task{}, nil, fmt.Errorf("begin task change feedback: %w", err)
	}
	defer transaction.Rollback()
	current, err := getTask(ctx, transaction, taskID)
	if err != nil {
		return discussion.Message{}, taskfeedback.Feedback{}, task.Task{}, nil, err
	}
	if current.Version != expectedVersion {
		return discussion.Message{}, taskfeedback.Feedback{}, task.Task{}, nil, ErrTaskVersionConflict
	}
	message, feedback, updated, followUp, err := applyChangeFeedback(ctx, transaction, current, input)
	if err != nil {
		return discussion.Message{}, taskfeedback.Feedback{}, task.Task{}, nil, err
	}
	if err := transaction.Commit(); err != nil {
		return discussion.Message{}, taskfeedback.Feedback{}, task.Task{}, nil, fmt.Errorf("commit task change feedback: %w", err)
	}
	return message, feedback, updated, followUp, nil
}

func applyChangeFeedback(
	ctx context.Context,
	transaction *sql.Tx,
	current task.Task,
	input discussion.CreateMessageInput,
) (discussion.Message, taskfeedback.Feedback, task.Task, *task.Task, error) {
	if current.Status == task.StatusRunning || current.Status == task.StatusCancelled || current.Status == task.StatusBacklog {
		return discussion.Message{}, taskfeedback.Feedback{}, task.Task{}, nil, ErrTaskFeedbackConflict
	}
	message, err := appendHumanTaskMessage(ctx, transaction, current.ID, input)
	if err != nil {
		return discussion.Message{}, taskfeedback.Feedback{}, task.Task{}, nil, err
	}
	feedback, err := createTaskFeedback(ctx, transaction, current.ID, message.ID, "", taskfeedback.IntentRequestChanges, taskfeedback.StatusApplied)
	if err != nil {
		return discussion.Message{}, taskfeedback.Feedback{}, task.Task{}, nil, err
	}
	if current.Status == task.StatusAccepted {
		followUp, createErr := createFollowUpTask(ctx, transaction, current, message)
		return message, feedback, current, followUp, createErr
	}
	updated, err := applyRequestedChanges(ctx, transaction, current, message)
	return message, feedback, updated, nil, err
}

func applyRequestedChanges(
	ctx context.Context,
	transaction *sql.Tx,
	current task.Task,
	message discussion.Message,
) (task.Task, error) {
	next, err := task.Transition(current.Status, task.CommandRequestChanges)
	if err != nil {
		return task.Task{}, err
	}
	updated, event, err := prepareTransition(current, next, task.CommandRequestChanges)
	if err != nil {
		return task.Task{}, err
	}
	if err := updateTaskStatus(ctx, transaction, current, updated); err != nil {
		return task.Task{}, err
	}
	if err := insertTaskEvent(ctx, transaction, event); err != nil {
		return task.Task{}, err
	}
	if current.Status == task.StatusReview {
		review, buildErr := newReview(current.ID, task.ReviewInput{Decision: task.ReviewRejected, Comment: message.Content}, "", event.OccurredAt)
		if buildErr != nil {
			return task.Task{}, buildErr
		}
		if err := insertTaskReview(ctx, transaction, review); err != nil {
			return task.Task{}, err
		}
	}
	return updated, nil
}

func createFollowUpTask(
	ctx context.Context,
	transaction *sql.Tx,
	current task.Task,
	message discussion.Message,
) (*task.Task, error) {
	input := task.CreateInput{
		Description:        fmt.Sprintf("修复 %s 的已验收实现：\n\n%s", current.Key, message.Content),
		AcceptanceCriteria: "完成反馈要求，并证明原有已验收行为没有回归。",
		Priority:           current.Priority, TargetBranch: current.TargetBranch,
	}.Normalized()
	created, event, err := newTaskAndEvent(input)
	if err != nil {
		return nil, err
	}
	if err := insertTask(ctx, transaction, created); err != nil {
		return nil, err
	}
	if err := insertTaskEvent(ctx, transaction, event); err != nil {
		return nil, err
	}
	if err := linkTasks(ctx, transaction, current.ID, created.ID, message.ID); err != nil {
		return nil, err
	}
	return &created, nil
}

func appendHumanTaskMessage(
	ctx context.Context,
	transaction *sql.Tx,
	taskID string,
	input discussion.CreateMessageInput,
) (discussion.Message, error) {
	threadID, err := findOrCreateThread(ctx, transaction, "task_id", taskID)
	if err != nil {
		return discussion.Message{}, err
	}
	message, err := appendMessage(ctx, transaction, threadID, discussion.AuthorHuman, input)
	if err != nil {
		return discussion.Message{}, err
	}
	if err := linkTaskMessageTasks(ctx, transaction, taskID, message); err != nil {
		return discussion.Message{}, err
	}
	if _, err := transaction.ExecContext(ctx, "UPDATE tasks SET updated_at = ? WHERE id = ?", formatTime(message.CreatedAt), taskID); err != nil {
		return discussion.Message{}, fmt.Errorf("update task feedback activity: %w", err)
	}
	return message, nil
}

func createTaskFeedback(
	ctx context.Context,
	transaction *sql.Tx,
	taskID string,
	messageID string,
	retryOfFeedbackID string,
	intent taskfeedback.Intent,
	status taskfeedback.Status,
) (taskfeedback.Feedback, error) {
	id, err := newID()
	if err != nil {
		return taskfeedback.Feedback{}, err
	}
	now := time.Now().UTC()
	created := taskfeedback.Feedback{
		ID: id, TaskID: taskID, SourceMessageID: messageID,
		RetryOfFeedbackID: retryOfFeedbackID, Intent: intent, Status: status,
		CreatedAt: now, UpdatedAt: now,
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO task_feedback_turns(
    id, task_id, source_message_id, retry_of_feedback_id, intent, status,
    failure_message, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, '', ?, ?)`,
		created.ID, created.TaskID, created.SourceMessageID, nullableString(created.RetryOfFeedbackID),
		created.Intent, created.Status,
		formatTime(created.CreatedAt), formatTime(created.UpdatedAt))
	if err != nil {
		return taskfeedback.Feedback{}, fmt.Errorf("insert task feedback: %w", err)
	}
	if _, err := appendTaskFeedbackEvent(ctx, transaction, created); err != nil {
		return taskfeedback.Feedback{}, err
	}
	return created, nil
}

// Get 返回一条反馈的当前持久状态。
func (store *TaskFeedbackStore) Get(ctx context.Context, id string) (taskfeedback.Feedback, error) {
	item, err := scanTaskFeedback(store.database.QueryRowContext(ctx,
		`SELECT `+taskFeedbackColumns+` FROM task_feedback_turns WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return taskfeedback.Feedback{}, ErrTaskFeedbackNotFound
	}
	return item, err
}

// ListTask 返回 Task 的全部反馈尝试，包含失败历史与重试链。
func (store *TaskFeedbackStore) ListTask(ctx context.Context, taskID string) ([]taskfeedback.Feedback, error) {
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return nil, err
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT `+taskFeedbackColumns+` FROM task_feedback_turns
WHERE task_id = ? ORDER BY created_at, id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task feedback: %w", err)
	}
	defer rows.Close()
	items := make([]taskfeedback.Feedback, 0)
	for rows.Next() {
		item, scanErr := scanTaskFeedback(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListEvents 返回指定 sequence 之后的反馈状态事件。
func (store *TaskFeedbackStore) ListEvents(ctx context.Context, taskID string, after int64, limit int) ([]taskfeedback.Event, error) {
	if after < 0 || limit < 1 || limit > 500 {
		return nil, errors.New("invalid task feedback event cursor")
	}
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return nil, err
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT id, task_id, feedback_id, sequence, status, COALESCE(run_id, ''),
       COALESCE(response_message_id, ''), failure_message, occurred_at
FROM task_feedback_events
WHERE task_id = ? AND sequence > ? ORDER BY sequence LIMIT ?`, taskID, after, limit)
	if err != nil {
		return nil, fmt.Errorf("list task feedback events: %w", err)
	}
	defer rows.Close()
	items := make([]taskfeedback.Event, 0)
	for rows.Next() {
		item, scanErr := scanTaskFeedbackEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// HasPendingTask 报告 Task 是否仍有等待或运行中的反馈尝试。
func (store *TaskFeedbackStore) HasPendingTask(ctx context.Context, taskID string) (bool, error) {
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return false, err
	}
	var exists bool
	if err := store.database.QueryRowContext(ctx, `
SELECT EXISTS(
    SELECT 1 FROM task_feedback_turns
    WHERE task_id = ? AND status IN ('QUEUED', 'RUNNING')
)`, taskID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check pending task feedback: %w", err)
	}
	return exists, nil
}

// Retry 为失败的讨论创建新尝试，不覆盖原反馈或原 Run。
func (store *TaskFeedbackStore) Retry(ctx context.Context, feedbackID string) (taskfeedback.Feedback, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return taskfeedback.Feedback{}, fmt.Errorf("begin retry task feedback: %w", err)
	}
	defer transaction.Rollback()
	original, err := getTaskFeedback(ctx, transaction, feedbackID)
	if err != nil {
		return taskfeedback.Feedback{}, err
	}
	if original.Intent != taskfeedback.IntentDiscuss || original.Status != taskfeedback.StatusFailed {
		return taskfeedback.Feedback{}, ErrTaskFeedbackConflict
	}
	var childID string
	err = transaction.QueryRowContext(ctx,
		"SELECT id FROM task_feedback_turns WHERE retry_of_feedback_id = ?", original.ID).Scan(&childID)
	if err == nil {
		return taskfeedback.Feedback{}, ErrTaskFeedbackConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return taskfeedback.Feedback{}, fmt.Errorf("check task feedback retry: %w", err)
	}
	created, err := createTaskFeedback(ctx, transaction, original.TaskID, original.SourceMessageID,
		original.ID, original.Intent, taskfeedback.StatusQueued)
	if err != nil {
		return taskfeedback.Feedback{}, err
	}
	if err := transaction.Commit(); err != nil {
		return taskfeedback.Feedback{}, fmt.Errorf("commit task feedback retry: %w", err)
	}
	return created, nil
}

// QuestionForRun 返回 REVIEW Run 当前必须回答的来源消息。
func (store *TaskFeedbackStore) QuestionForRun(ctx context.Context, runID string) (discussion.Message, error) {
	message, err := scanMessage(store.database.QueryRowContext(ctx, `
SELECT m.id, m.thread_id, m.sequence, m.author_kind, m.content, m.created_at
FROM task_feedback_turns f
JOIN messages m ON m.id = f.source_message_id
WHERE f.run_id = ? AND f.intent = 'DISCUSS'`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return discussion.Message{}, ErrTaskFeedbackNotFound
	}
	if err != nil {
		return discussion.Message{}, fmt.Errorf("load task feedback question: %w", err)
	}
	return message, nil
}

func scanTaskFeedback(scanner rowScanner) (taskfeedback.Feedback, error) {
	var item taskfeedback.Feedback
	var createdAt, updatedAt string
	if err := scanner.Scan(
		&item.ID, &item.TaskID, &item.SourceMessageID, &item.RetryOfFeedbackID, &item.Intent, &item.Status,
		&item.RunID, &item.ResponseMessageID, &item.FailureMessage, &createdAt, &updatedAt,
	); err != nil {
		return taskfeedback.Feedback{}, err
	}
	var err error
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return taskfeedback.Feedback{}, err
	}
	item.UpdatedAt, err = parseTime(updatedAt)
	return item, err
}

func scanTaskFeedbackEvent(scanner rowScanner) (taskfeedback.Event, error) {
	var item taskfeedback.Event
	var occurredAt string
	if err := scanner.Scan(
		&item.ID, &item.TaskID, &item.FeedbackID, &item.Sequence, &item.Status,
		&item.RunID, &item.ResponseMessageID, &item.FailureMessage, &occurredAt,
	); err != nil {
		return taskfeedback.Event{}, err
	}
	parsed, err := parseTime(occurredAt)
	item.OccurredAt = parsed
	return item, err
}

func getTaskFeedback(ctx context.Context, queryer rowQueryer, id string) (taskfeedback.Feedback, error) {
	item, err := scanTaskFeedback(queryer.QueryRowContext(ctx,
		`SELECT `+taskFeedbackColumns+` FROM task_feedback_turns WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return taskfeedback.Feedback{}, ErrTaskFeedbackNotFound
	}
	if err != nil {
		return taskfeedback.Feedback{}, fmt.Errorf("load task feedback: %w", err)
	}
	return item, nil
}

func appendTaskFeedbackEvent(
	ctx context.Context,
	transaction *sql.Tx,
	feedback taskfeedback.Feedback,
) (taskfeedback.Event, error) {
	id, err := newID()
	if err != nil {
		return taskfeedback.Event{}, err
	}
	var sequence int64
	if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(MAX(sequence), 0) + 1 FROM task_feedback_events WHERE task_id = ?`,
		feedback.TaskID).Scan(&sequence); err != nil {
		return taskfeedback.Event{}, fmt.Errorf("next task feedback event sequence: %w", err)
	}
	event := taskfeedback.Event{
		ID: id, TaskID: feedback.TaskID, FeedbackID: feedback.ID, Sequence: sequence,
		Status: feedback.Status, RunID: feedback.RunID, ResponseMessageID: feedback.ResponseMessageID,
		FailureMessage: feedback.FailureMessage, OccurredAt: feedback.UpdatedAt,
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO task_feedback_events(
    id, task_id, feedback_id, sequence, status, run_id,
    response_message_id, failure_message, occurred_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.TaskID, event.FeedbackID, event.Sequence, event.Status,
		nullableString(event.RunID), nullableString(event.ResponseMessageID),
		event.FailureMessage, formatTime(event.OccurredAt))
	if err != nil {
		return taskfeedback.Event{}, fmt.Errorf("append task feedback event: %w", err)
	}
	return event, nil
}

func claimTaskFeedback(ctx context.Context, transaction *sql.Tx, feedbackID string, runID string) error {
	now := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE task_feedback_turns SET status = 'RUNNING', run_id = ?, updated_at = ?
WHERE id = ? AND status = 'QUEUED' AND run_id IS NULL`, runID, formatTime(now), feedbackID)
	if err != nil {
		return fmt.Errorf("claim task feedback: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return ErrTaskFeedbackConflict
	}
	feedback, err := getTaskFeedback(ctx, transaction, feedbackID)
	if err != nil {
		return err
	}
	_, err = appendTaskFeedbackEvent(ctx, transaction, feedback)
	return err
}

func finalizeTaskFeedback(
	ctx context.Context,
	transaction *sql.Tx,
	current domainrun.Run,
	finish RunFinish,
	reply string,
) error {
	if finish.Status != domainrun.StatusSucceeded {
		return failTaskFeedback(ctx, transaction, current.ID, finish.FailureMessage)
	}
	message, err := appendAgentTaskMessage(ctx, transaction, current.TaskID, reply)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE task_feedback_turns
SET status = 'ANSWERED', response_message_id = ?, failure_message = '', updated_at = ?
WHERE run_id = ? AND status = 'RUNNING'`, message.ID, formatTime(now), current.ID)
	if err != nil {
		return fmt.Errorf("answer task feedback: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return ErrTaskFeedbackConflict
	}
	feedback, err := getTaskFeedbackByRun(ctx, transaction, current.ID)
	if err != nil {
		return err
	}
	_, err = appendTaskFeedbackEvent(ctx, transaction, feedback)
	return err
}

func failTaskFeedback(ctx context.Context, transaction *sql.Tx, runID string, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "Agent 未能生成回答"
	}
	now := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE task_feedback_turns
SET status = 'FAILED', failure_message = ?, updated_at = ?
WHERE run_id = ? AND status = 'RUNNING'`, message, formatTime(now), runID)
	if err != nil {
		return fmt.Errorf("fail task feedback: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return nil
	}
	if changed != 1 {
		return ErrTaskFeedbackConflict
	}
	feedback, err := getTaskFeedbackByRun(ctx, transaction, runID)
	if err != nil {
		return err
	}
	_, err = appendTaskFeedbackEvent(ctx, transaction, feedback)
	return err
}

func getTaskFeedbackByRun(ctx context.Context, queryer rowQueryer, runID string) (taskfeedback.Feedback, error) {
	item, err := scanTaskFeedback(queryer.QueryRowContext(ctx,
		`SELECT `+taskFeedbackColumns+` FROM task_feedback_turns WHERE run_id = ?`, runID))
	if err != nil {
		return taskfeedback.Feedback{}, fmt.Errorf("load task feedback for run: %w", err)
	}
	return item, nil
}
