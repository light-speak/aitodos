package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/light-speak/aitodos/internal/domain/clarification"
	domainrun "github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
)

var (
	// ErrClarificationNotFound 表示指定问题不存在。
	ErrClarificationNotFound = errors.New("clarification not found")
	// ErrClarificationConflict 表示问题已回答或版本已变化。
	ErrClarificationConflict = errors.New("clarification conflict")
)

const clarificationColumns = `
id, task_id, source_run_id, COALESCE(continuation_run_id, ''), continuation_purpose,
category, question, options_json, recommended_option_id, allow_custom_answer,
status, selected_option_id, custom_answer, version, created_at, updated_at,
COALESCE(answered_at, '')`

// ClarificationStore 持久化 Agent 阻塞问题和人工答案。
type ClarificationStore struct {
	database *sql.DB
}

// NewClarificationStore 创建 Clarification 存储。
func NewClarificationStore(database *sql.DB) *ClarificationStore {
	return &ClarificationStore{database: database}
}

// ListOpen 返回全项目待人工回答的问题。
func (store *ClarificationStore) ListOpen(ctx context.Context) ([]clarification.Clarification, error) {
	return listClarifications(ctx, store.database, `WHERE status = 'OPEN' ORDER BY created_at ASC`, nil)
}

// ListTask 返回 Task 的完整问题历史。
func (store *ClarificationStore) ListTask(ctx context.Context, taskID string) ([]clarification.Clarification, error) {
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return nil, err
	}
	return listClarifications(ctx, store.database, `WHERE task_id = ? ORDER BY created_at DESC`, []any{taskID})
}

// GetForContinuationRun 返回触发指定续跑的已回答问题。
func (store *ClarificationStore) GetForContinuationRun(ctx context.Context, runID string) (clarification.Clarification, error) {
	item, err := scanClarification(store.database.QueryRowContext(ctx,
		`SELECT `+clarificationColumns+` FROM clarifications WHERE continuation_run_id = ?`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return clarification.Clarification{}, ErrClarificationNotFound
	}
	return item, err
}

// Answer 原子保存人工回答并恢复 Task 到对应执行队列。
func (store *ClarificationStore) Answer(
	ctx context.Context,
	id string,
	input clarification.AnswerInput,
) (clarification.Clarification, task.Task, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return clarification.Clarification{}, task.Task{}, fmt.Errorf("begin clarification answer: %w", err)
	}
	defer transaction.Rollback()
	current, err := getClarification(ctx, transaction, id)
	if err != nil {
		return clarification.Clarification{}, task.Task{}, err
	}
	if current.Status != clarification.StatusOpen || current.Version != input.ExpectedVersion {
		return clarification.Clarification{}, task.Task{}, ErrClarificationConflict
	}
	input = input.Normalized()
	if err := input.ValidateFor(current); err != nil {
		return clarification.Clarification{}, task.Task{}, err
	}
	currentTask, err := getTask(ctx, transaction, current.TaskID)
	if err != nil {
		return clarification.Clarification{}, task.Task{}, err
	}
	command, err := resumeCommand(current.ContinuationPurpose)
	if err != nil {
		return clarification.Clarification{}, task.Task{}, err
	}
	next, err := task.Transition(currentTask.Status, command)
	if err != nil {
		return clarification.Clarification{}, task.Task{}, err
	}
	updatedTask, event, err := prepareTransition(currentTask, next, command)
	if err != nil {
		return clarification.Clarification{}, task.Task{}, err
	}
	if err := updateTaskStatus(ctx, transaction, currentTask, updatedTask); err != nil {
		return clarification.Clarification{}, task.Task{}, err
	}
	if err := insertTaskEvent(ctx, transaction, event); err != nil {
		return clarification.Clarification{}, task.Task{}, err
	}
	now := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE clarifications
SET status = 'ANSWERED', selected_option_id = ?, custom_answer = ?,
    version = version + 1, answered_at = ?, updated_at = ?
WHERE id = ? AND status = 'OPEN' AND version = ?`, input.SelectedOptionID,
		input.CustomAnswer, formatTime(now), formatTime(now), id, input.ExpectedVersion)
	if err != nil {
		return clarification.Clarification{}, task.Task{}, fmt.Errorf("answer clarification: %w", err)
	}
	if err := requireClarificationChange(result); err != nil {
		return clarification.Clarification{}, task.Task{}, err
	}
	current.Status = clarification.StatusAnswered
	current.SelectedOptionID = input.SelectedOptionID
	current.CustomAnswer = input.CustomAnswer
	current.Version++
	current.AnsweredAt = now
	current.UpdatedAt = now
	if err := transaction.Commit(); err != nil {
		return clarification.Clarification{}, task.Task{}, fmt.Errorf("commit clarification answer: %w", err)
	}
	return current, updatedTask, nil
}

func listClarifications(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, clause string, args []any) ([]clarification.Clarification, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT `+clarificationColumns+` FROM clarifications `+clause, args...)
	if err != nil {
		return nil, fmt.Errorf("list clarifications: %w", err)
	}
	defer rows.Close()
	result := make([]clarification.Clarification, 0)
	for rows.Next() {
		item, scanErr := scanClarification(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func getClarification(ctx context.Context, queryer rowQueryer, id string) (clarification.Clarification, error) {
	item, err := scanClarification(queryer.QueryRowContext(ctx,
		`SELECT `+clarificationColumns+` FROM clarifications WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return clarification.Clarification{}, ErrClarificationNotFound
	}
	return item, err
}

func scanClarification(scanner rowScanner) (clarification.Clarification, error) {
	var item clarification.Clarification
	var optionsJSON, createdAt, updatedAt, answeredAt string
	if err := scanner.Scan(&item.ID, &item.TaskID, &item.SourceRunID, &item.ContinuationRunID,
		&item.ContinuationPurpose, &item.Category, &item.Question, &optionsJSON,
		&item.RecommendedOptionID, &item.AllowCustomAnswer, &item.Status,
		&item.SelectedOptionID, &item.CustomAnswer, &item.Version, &createdAt,
		&updatedAt, &answeredAt); err != nil {
		return clarification.Clarification{}, err
	}
	if err := json.Unmarshal([]byte(optionsJSON), &item.Options); err != nil {
		return clarification.Clarification{}, fmt.Errorf("decode clarification options: %w", err)
	}
	var err error
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return clarification.Clarification{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return clarification.Clarification{}, err
	}
	if answeredAt != "" {
		if item.AnsweredAt, err = parseTime(answeredAt); err != nil {
			return clarification.Clarification{}, err
		}
	}
	return item, nil
}

func resumeCommand(purpose domainrun.Purpose) (task.Command, error) {
	switch purpose {
	case domainrun.PurposeImplementation:
		return task.CommandResumeImplementation, nil
	case domainrun.PurposeRevision:
		return task.CommandResumeRevision, nil
	default:
		return "", errors.New("clarification continuation purpose 无效")
	}
}

func buildClarification(currentRun domainrun.Run, request clarification.Request) (clarification.Clarification, error) {
	id, err := newID()
	if err != nil {
		return clarification.Clarification{}, err
	}
	now := time.Now().UTC()
	return clarification.Clarification{
		ID: id, TaskID: currentRun.TaskID, SourceRunID: currentRun.ID,
		ContinuationPurpose: currentRun.Purpose, Category: request.Category,
		Question: request.Question, Options: request.Options,
		RecommendedOptionID: request.RecommendedOptionID,
		AllowCustomAnswer:   request.AllowCustomAnswer, Status: clarification.StatusOpen,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func insertClarification(ctx context.Context, transaction *sql.Tx, item clarification.Clarification) error {
	optionsJSON, err := json.Marshal(item.Options)
	if err != nil {
		return fmt.Errorf("encode clarification options: %w", err)
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO clarifications(
    id, task_id, source_run_id, continuation_run_id, continuation_purpose,
    category, question, options_json, recommended_option_id, allow_custom_answer,
    status, selected_option_id, custom_answer, version, created_at, updated_at, answered_at
) VALUES (?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, 'OPEN', '', '', 1, ?, ?, NULL)`,
		item.ID, item.TaskID, item.SourceRunID, item.ContinuationPurpose,
		item.Category, item.Question, string(optionsJSON), item.RecommendedOptionID,
		item.AllowCustomAnswer, formatTime(item.CreatedAt), formatTime(item.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert clarification: %w", err)
	}
	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func requireClarificationChange(result sql.Result) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read clarification update result: %w", err)
	}
	if changed != 1 {
		return ErrClarificationConflict
	}
	return nil
}
