package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/domain/objective"
)

var (
	// ErrObjectiveNotFound 表示长期目标不存在。
	ErrObjectiveNotFound = errors.New("objective not found")
	// ErrActiveObjectiveExists 表示项目已有活跃或暂停的长期目标。
	ErrActiveObjectiveExists = errors.New("active objective already exists")
	// ErrObjectiveVersionConflict 表示命令基于过期版本。
	ErrObjectiveVersionConflict = errors.New("objective version conflict")
	// ErrObjectiveNotReady 表示完成条件或关联工作尚未满足。
	ErrObjectiveNotReady = errors.New("objective is not ready to achieve")
	// ErrObjectiveStateConflict 表示命令不适用于当前状态。
	ErrObjectiveStateConflict = errors.New("objective state conflict")
	// ErrInvalidObjectiveInput 表示长期目标或检查点输入无效。
	ErrInvalidObjectiveInput = errors.New("invalid objective input")
)

const objectiveColumns = `
id, objective_key, root_topic_id, status, COALESCE(current_revision_id, ''),
max_continuations, continuation_count, version, created_at, updated_at, COALESCE(completed_at, '')`

// ObjectiveStore 持久化长期目标、不可变 Revision 和 Checkpoint。
type ObjectiveStore struct {
	database *sql.DB
}

// NewObjectiveStore 创建长期目标持久化服务。
func NewObjectiveStore(database *sql.DB) *ObjectiveStore {
	return &ObjectiveStore{database: database}
}

// Create 原子创建长期目标、首个 Revision 和审计事件。
func (store *ObjectiveStore) Create(ctx context.Context, input objective.CreateInput) (objective.View, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return objective.View{}, fmt.Errorf("%w: %v", ErrInvalidObjectiveInput, err)
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return objective.View{}, fmt.Errorf("begin objective creation: %w", err)
	}
	defer transaction.Rollback()
	if err := requireTopic(ctx, transaction, input.RootTopicID); err != nil {
		return objective.View{}, err
	}
	if err := requireNoCurrentObjective(ctx, transaction); err != nil {
		return objective.View{}, err
	}
	created, revision, err := prepareObjective(input)
	if err != nil {
		return objective.View{}, err
	}
	if err := insertObjective(ctx, transaction, created); err != nil {
		return objective.View{}, err
	}
	if err := insertObjectiveRevision(ctx, transaction, revision); err != nil {
		return objective.View{}, err
	}
	if err := setCurrentObjectiveRevision(ctx, transaction, created.ID, revision.ID); err != nil {
		return objective.View{}, err
	}
	created.CurrentRevisionID = revision.ID
	if err := insertObjectiveEvent(ctx, transaction, created, "OBJECTIVE_CREATED", map[string]any{"schema_version": 1}); err != nil {
		return objective.View{}, err
	}
	if err := transaction.Commit(); err != nil {
		return objective.View{}, fmt.Errorf("commit objective creation: %w", err)
	}
	return store.Get(ctx, created.ID)
}

// GetCurrent 返回项目当前活跃或暂停的长期目标。
func (store *ObjectiveStore) GetCurrent(ctx context.Context) (objective.View, error) {
	return store.getCurrentBy(ctx, "", "")
}

// GetForTopic 返回以指定 Topic 为根的当前长期目标。
func (store *ObjectiveStore) GetForTopic(ctx context.Context, topicID string) (objective.View, error) {
	return store.getCurrentBy(ctx, topicID, "")
}

// GetForTask 返回根 Topic 已关联指定 Task 的当前长期目标。
func (store *ObjectiveStore) GetForTask(ctx context.Context, taskID string) (objective.View, error) {
	return store.getCurrentBy(ctx, "", taskID)
}

// Get 返回一个长期目标及其当前 Revision、最近 Checkpoint 和派生进度。
func (store *ObjectiveStore) Get(ctx context.Context, id string) (objective.View, error) {
	return getObjectiveView(ctx, store.database, id)
}

func (store *ObjectiveStore) getCurrentBy(ctx context.Context, topicID, taskID string) (objective.View, error) {
	var id string
	err := store.database.QueryRowContext(ctx, `
SELECT objectives.id
FROM objectives
WHERE objectives.status IN ('ACTIVE', 'PAUSED')
  AND (? = '' OR objectives.root_topic_id = ?)
  AND (? = '' OR EXISTS (
      SELECT 1 FROM topic_task_links
      WHERE topic_task_links.topic_id = objectives.root_topic_id
        AND topic_task_links.task_id = ?
  ))
ORDER BY objectives.created_at DESC
LIMIT 1`, topicID, topicID, taskID, taskID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return objective.View{}, ErrObjectiveNotFound
	}
	if err != nil {
		return objective.View{}, fmt.Errorf("query current objective: %w", err)
	}
	return store.Get(ctx, id)
}

// ListCheckpoints 返回目标的不可变检查点历史。
func (store *ObjectiveStore) ListCheckpoints(ctx context.Context, id string) ([]objective.Checkpoint, error) {
	if _, err := getObjective(ctx, store.database, id); err != nil {
		return nil, err
	}
	rows, err := store.database.QueryContext(ctx, checkpointSelect+" WHERE objective_id = ? ORDER BY sequence DESC", id)
	if err != nil {
		return nil, fmt.Errorf("list objective checkpoints: %w", err)
	}
	defer rows.Close()
	result := make([]objective.Checkpoint, 0)
	for rows.Next() {
		checkpoint, err := scanCheckpoint(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, checkpoint)
	}
	return result, rows.Err()
}

// AppendCheckpoint 原子追加一个检查点并推进聚合版本。
func (store *ObjectiveStore) AppendCheckpoint(ctx context.Context, id string, expectedVersion int64, input objective.CheckpointInput) (objective.View, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return objective.View{}, fmt.Errorf("begin objective checkpoint: %w", err)
	}
	defer transaction.Rollback()
	current, err := getObjectiveView(ctx, transaction, id)
	if err != nil {
		return objective.View{}, err
	}
	if current.Objective.Version != expectedVersion {
		return objective.View{}, ErrObjectiveVersionConflict
	}
	if current.Objective.Status != objective.StatusActive {
		return objective.View{}, ErrObjectiveStateConflict
	}
	input = input.Normalized()
	if err := input.Validate(criterionIDs(current.Revision)); err != nil {
		return objective.View{}, fmt.Errorf("%w: %v", ErrInvalidObjectiveInput, err)
	}
	checkpoint, err := prepareCheckpoint(ctx, transaction, current.Objective.ID, input)
	if err != nil {
		return objective.View{}, err
	}
	if err := insertCheckpoint(ctx, transaction, checkpoint); err != nil {
		return objective.View{}, err
	}
	updated := current.Objective
	updated.Version++
	updated.UpdatedAt = checkpoint.CreatedAt
	if err := updateObjective(ctx, transaction, current.Objective, updated); err != nil {
		return objective.View{}, err
	}
	if err := insertObjectiveEvent(ctx, transaction, updated, "OBJECTIVE_CHECKPOINTED", map[string]any{
		"schema_version": 1, "checkpoint_id": checkpoint.ID, "stop_reason": checkpoint.StopReason,
	}); err != nil {
		return objective.View{}, err
	}
	if err := transaction.Commit(); err != nil {
		return objective.View{}, fmt.Errorf("commit objective checkpoint: %w", err)
	}
	return store.Get(ctx, id)
}

// ApplyCommand 原子执行显式生命周期命令。
func (store *ObjectiveStore) ApplyCommand(ctx context.Context, id string, expectedVersion int64, command objective.Command) (objective.View, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return objective.View{}, fmt.Errorf("begin objective command: %w", err)
	}
	defer transaction.Rollback()
	current, err := getObjectiveView(ctx, transaction, id)
	if err != nil {
		return objective.View{}, err
	}
	if current.Objective.Version != expectedVersion {
		return objective.View{}, ErrObjectiveVersionConflict
	}
	next, err := objective.Transition(current.Objective.Status, command)
	if err != nil {
		return objective.View{}, fmt.Errorf("%w: %v", ErrObjectiveStateConflict, err)
	}
	if command == objective.CommandAchieve {
		if ready, err := objectiveReady(ctx, transaction, current); err != nil {
			return objective.View{}, err
		} else if !ready {
			return objective.View{}, ErrObjectiveNotReady
		}
	}
	updated := current.Objective
	updated.Status = next
	updated.Version++
	updated.UpdatedAt = time.Now().UTC()
	if next == objective.StatusAchieved || next == objective.StatusCancelled {
		completedAt := updated.UpdatedAt
		updated.CompletedAt = &completedAt
	}
	if err := updateObjective(ctx, transaction, current.Objective, updated); err != nil {
		return objective.View{}, err
	}
	if err := insertObjectiveEvent(ctx, transaction, updated, "OBJECTIVE_STATUS_CHANGED", map[string]any{
		"schema_version": 1, "command": command, "from": current.Objective.Status, "to": next,
	}); err != nil {
		return objective.View{}, err
	}
	if err := transaction.Commit(); err != nil {
		return objective.View{}, fmt.Errorf("commit objective command: %w", err)
	}
	return store.Get(ctx, id)
}

func prepareObjective(input objective.CreateInput) (objective.Objective, objective.Revision, error) {
	objectiveID, err := newID()
	if err != nil {
		return objective.Objective{}, objective.Revision{}, err
	}
	revisionID, err := newID()
	if err != nil {
		return objective.Objective{}, objective.Revision{}, err
	}
	criteria := make([]objective.Criterion, len(input.CompletionCriteria))
	for index, description := range input.CompletionCriteria {
		id, err := newID()
		if err != nil {
			return objective.Objective{}, objective.Revision{}, err
		}
		criteria[index] = objective.Criterion{ID: id, Description: description}
	}
	now := time.Now().UTC()
	created := objective.Objective{
		ID: objectiveID, Key: objectiveKey(objectiveID), RootTopicID: input.RootTopicID,
		Status: objective.StatusActive, MaxContinuations: 20, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	revision := objective.Revision{
		ID: revisionID, ObjectiveID: objectiveID, Revision: 1, Statement: input.Statement,
		Scope: input.Scope, Constraints: input.Constraints, CompletionCriteria: criteria, CreatedAt: now,
	}
	return created, revision, nil
}

func getObjectiveView(ctx context.Context, queryer rowQueryer, id string) (objective.View, error) {
	item, err := getObjective(ctx, queryer, id)
	if err != nil {
		return objective.View{}, err
	}
	revision, err := getObjectiveRevision(ctx, queryer, item.CurrentRevisionID)
	if err != nil {
		return objective.View{}, err
	}
	checkpoint, err := getLatestCheckpoint(ctx, queryer, id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return objective.View{}, err
	}
	progress, err := objectiveProgress(ctx, queryer, item.RootTopicID, revision, checkpoint)
	if err != nil {
		return objective.View{}, err
	}
	view := objective.View{Objective: item, Revision: revision, Progress: progress}
	if err == nil && checkpoint.ID != "" {
		view.LatestCheckpoint = &checkpoint
	}
	return view, nil
}

func getObjective(ctx context.Context, queryer rowQueryer, id string) (objective.Objective, error) {
	item, err := scanObjective(queryer.QueryRowContext(ctx, "SELECT "+objectiveColumns+" FROM objectives WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return objective.Objective{}, ErrObjectiveNotFound
	}
	return item, err
}

func scanObjective(scanner rowScanner) (objective.Objective, error) {
	var item objective.Objective
	var createdAt, updatedAt, completedAt string
	err := scanner.Scan(&item.ID, &item.Key, &item.RootTopicID, &item.Status, &item.CurrentRevisionID,
		&item.MaxContinuations, &item.ContinuationCount, &item.Version, &createdAt, &updatedAt, &completedAt)
	if err != nil {
		return objective.Objective{}, err
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return objective.Objective{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return objective.Objective{}, err
	}
	if completedAt != "" {
		parsed, parseErr := parseTime(completedAt)
		item.CompletedAt, err = &parsed, parseErr
	}
	return item, err
}

func getObjectiveRevision(ctx context.Context, queryer rowQueryer, id string) (objective.Revision, error) {
	var revision objective.Revision
	var constraintsJSON, criteriaJSON, createdAt string
	err := queryer.QueryRowContext(ctx, `
SELECT id, objective_id, revision_number, statement, scope, constraints_json,
       completion_criteria_json, COALESCE(previous_revision_id, ''), created_at
FROM objective_revisions WHERE id = ?`, id).Scan(
		&revision.ID, &revision.ObjectiveID, &revision.Revision, &revision.Statement, &revision.Scope,
		&constraintsJSON, &criteriaJSON, &revision.PreviousRevisionID, &createdAt,
	)
	if err != nil {
		return objective.Revision{}, fmt.Errorf("query objective revision: %w", err)
	}
	if err := json.Unmarshal([]byte(constraintsJSON), &revision.Constraints); err != nil {
		return objective.Revision{}, fmt.Errorf("decode objective constraints: %w", err)
	}
	if err := json.Unmarshal([]byte(criteriaJSON), &revision.CompletionCriteria); err != nil {
		return objective.Revision{}, fmt.Errorf("decode objective criteria: %w", err)
	}
	revision.CreatedAt, err = parseTime(createdAt)
	return revision, err
}

const checkpointSelect = `SELECT id, objective_id, sequence, COALESCE(source_run_id, ''), summary,
criteria_json, completed_json, remaining_json, risks_json, stop_reason, next_action, created_at
FROM objective_checkpoints`

func getLatestCheckpoint(ctx context.Context, queryer rowQueryer, id string) (objective.Checkpoint, error) {
	return scanCheckpoint(queryer.QueryRowContext(ctx, checkpointSelect+" WHERE objective_id = ? ORDER BY sequence DESC LIMIT 1", id))
}

func scanCheckpoint(scanner rowScanner) (objective.Checkpoint, error) {
	var item objective.Checkpoint
	var criteriaJSON, completedJSON, remainingJSON, risksJSON, createdAt string
	if err := scanner.Scan(&item.ID, &item.ObjectiveID, &item.Sequence, &item.SourceRunID, &item.Summary,
		&criteriaJSON, &completedJSON, &remainingJSON, &risksJSON, &item.StopReason, &item.NextAction, &createdAt); err != nil {
		return objective.Checkpoint{}, err
	}
	for _, target := range []struct {
		content string
		value   any
	}{{criteriaJSON, &item.Criteria}, {completedJSON, &item.Completed}, {remainingJSON, &item.Remaining}, {risksJSON, &item.Risks}} {
		if err := json.Unmarshal([]byte(target.content), target.value); err != nil {
			return objective.Checkpoint{}, fmt.Errorf("decode objective checkpoint: %w", err)
		}
	}
	parsed, err := parseTime(createdAt)
	item.CreatedAt = parsed
	return item, err
}

func objectiveProgress(ctx context.Context, queryer rowQueryer, topicID string, revision objective.Revision, checkpoint objective.Checkpoint) (objective.Progress, error) {
	progress := objective.Progress{CriteriaTotal: len(revision.CompletionCriteria)}
	for _, result := range checkpoint.Criteria {
		if result.Status == objective.CriterionSatisfied {
			progress.CriteriaSatisfied++
		}
	}
	err := queryer.QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(SUM(CASE WHEN tasks.status = 'ACCEPTED' THEN 1 ELSE 0 END), 0)
FROM topic_task_links JOIN tasks ON tasks.id = topic_task_links.task_id
WHERE topic_task_links.topic_id = ?`, topicID).Scan(&progress.TasksTotal, &progress.TasksAccepted)
	if err != nil {
		return objective.Progress{}, fmt.Errorf("query objective task progress: %w", err)
	}
	return progress, nil
}

func objectiveReady(ctx context.Context, queryer rowQueryer, view objective.View) (bool, error) {
	if view.LatestCheckpoint == nil || !objective.AllCriteriaSatisfied(view.Revision.CompletionCriteria, view.LatestCheckpoint.Criteria) {
		return false, nil
	}
	if view.Progress.TasksTotal != view.Progress.TasksAccepted {
		return false, nil
	}
	var blockers int
	err := queryer.QueryRowContext(ctx, `
SELECT
    (SELECT COUNT(*) FROM clarifications WHERE topic_id = ? AND status = 'OPEN') +
    (SELECT COUNT(*) FROM plans WHERE topic_id = ? AND status <> 'APPROVED')`,
		view.Objective.RootTopicID, view.Objective.RootTopicID).Scan(&blockers)
	if err != nil {
		return false, fmt.Errorf("query objective blockers: %w", err)
	}
	return blockers == 0, nil
}

func criterionIDs(revision objective.Revision) map[string]struct{} {
	result := make(map[string]struct{}, len(revision.CompletionCriteria))
	for _, criterion := range revision.CompletionCriteria {
		result[criterion.ID] = struct{}{}
	}
	return result
}

func objectiveKey(id string) string {
	return "OBJ-" + strings.ToUpper(strings.ReplaceAll(id, "-", "")[:8])
}

func requireNoCurrentObjective(ctx context.Context, queryer rowQueryer) error {
	var count int
	if err := queryer.QueryRowContext(ctx, "SELECT COUNT(*) FROM objectives WHERE status IN ('ACTIVE', 'PAUSED')").Scan(&count); err != nil {
		return fmt.Errorf("query current objective: %w", err)
	}
	if count > 0 {
		return ErrActiveObjectiveExists
	}
	return nil
}

func insertObjective(ctx context.Context, transaction *sql.Tx, item objective.Objective) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO objectives(id, objective_key, root_topic_id, status, current_revision_id,
    max_continuations, continuation_count, version, created_at, updated_at, completed_at)
VALUES (?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, NULL)`, item.ID, item.Key, item.RootTopicID, item.Status,
		item.MaxContinuations, item.ContinuationCount, item.Version, formatTime(item.CreatedAt), formatTime(item.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert objective: %w", err)
	}
	return nil
}

func insertObjectiveRevision(ctx context.Context, transaction *sql.Tx, item objective.Revision) error {
	constraintsJSON, err := json.Marshal(item.Constraints)
	if err != nil {
		return err
	}
	criteriaJSON, err := json.Marshal(item.CompletionCriteria)
	if err != nil {
		return err
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO objective_revisions(id, objective_id, revision_number, statement, scope,
    constraints_json, completion_criteria_json, previous_revision_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?)`, item.ID, item.ObjectiveID, item.Revision,
		item.Statement, item.Scope, string(constraintsJSON), string(criteriaJSON), item.PreviousRevisionID, formatTime(item.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert objective revision: %w", err)
	}
	return nil
}

func setCurrentObjectiveRevision(ctx context.Context, transaction *sql.Tx, objectiveID, revisionID string) error {
	result, err := transaction.ExecContext(ctx, "UPDATE objectives SET current_revision_id = ? WHERE id = ? AND current_revision_id IS NULL", revisionID, objectiveID)
	if err != nil {
		return fmt.Errorf("set current objective revision: %w", err)
	}
	return requireObjectiveChange(result)
}

func prepareCheckpoint(ctx context.Context, queryer rowQueryer, objectiveID string, input objective.CheckpointInput) (objective.Checkpoint, error) {
	id, err := newID()
	if err != nil {
		return objective.Checkpoint{}, err
	}
	var sequence int
	if err := queryer.QueryRowContext(ctx, "SELECT COALESCE(MAX(sequence), 0) + 1 FROM objective_checkpoints WHERE objective_id = ?", objectiveID).Scan(&sequence); err != nil {
		return objective.Checkpoint{}, fmt.Errorf("prepare objective checkpoint sequence: %w", err)
	}
	return objective.Checkpoint{
		ID: id, ObjectiveID: objectiveID, Sequence: sequence, SourceRunID: input.SourceRunID,
		Summary: input.Summary, Criteria: input.Criteria, Completed: input.Completed,
		Remaining: input.Remaining, Risks: input.Risks, StopReason: input.StopReason,
		NextAction: input.NextAction, CreatedAt: time.Now().UTC(),
	}, nil
}

func insertCheckpoint(ctx context.Context, transaction *sql.Tx, item objective.Checkpoint) error {
	criteriaJSON, _ := json.Marshal(item.Criteria)
	completedJSON, _ := json.Marshal(item.Completed)
	remainingJSON, _ := json.Marshal(item.Remaining)
	risksJSON, _ := json.Marshal(item.Risks)
	_, err := transaction.ExecContext(ctx, `
INSERT INTO objective_checkpoints(id, objective_id, sequence, source_run_id, summary,
    criteria_json, completed_json, remaining_json, risks_json, stop_reason, next_action, created_at)
VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, item.ObjectiveID, item.Sequence,
		item.SourceRunID, item.Summary, string(criteriaJSON), string(completedJSON), string(remainingJSON),
		string(risksJSON), item.StopReason, item.NextAction, formatTime(item.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert objective checkpoint: %w", err)
	}
	return nil
}

func updateObjective(ctx context.Context, transaction *sql.Tx, current, updated objective.Objective) error {
	completedAt := any(nil)
	if updated.CompletedAt != nil {
		completedAt = formatTime(*updated.CompletedAt)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE objectives SET status = ?, version = ?, updated_at = ?, completed_at = ?
WHERE id = ? AND status = ? AND version = ?`, updated.Status, updated.Version,
		formatTime(updated.UpdatedAt), completedAt, current.ID, current.Status, current.Version)
	if err != nil {
		return fmt.Errorf("update objective: %w", err)
	}
	return requireObjectiveChange(result)
}

func insertObjectiveEvent(ctx context.Context, transaction *sql.Tx, item objective.Objective, eventType string, payloadValue map[string]any) error {
	id, err := newID()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(payloadValue)
	if err != nil {
		return fmt.Errorf("encode objective event: %w", err)
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO objective_events(id, objective_id, sequence, event_type, payload_json, occurred_at)
VALUES (?, ?, ?, ?, ?, ?)`, id, item.ID, item.Version, eventType, string(payload), formatTime(item.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert objective event: %w", err)
	}
	return nil
}

func requireObjectiveChange(result sql.Result) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read objective update result: %w", err)
	}
	if changed != 1 {
		return ErrObjectiveVersionConflict
	}
	return nil
}
