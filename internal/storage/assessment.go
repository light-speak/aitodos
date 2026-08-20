package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/light-speak/aitodos/internal/domain/assessment"
	domainrun "github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
)

var (
	// ErrAssessmentNotFound 表示 Task 尚无评估。
	ErrAssessmentNotFound = errors.New("task assessment not found")
	// ErrAssessmentInputConflict 表示 Run 评估的是已经过期的 Task 输入版本。
	ErrAssessmentInputConflict = errors.New("task assessment input conflict")
)

// AssessmentStore 持久化不可变 Task 评估并安全应用 AI 标题。
type AssessmentStore struct {
	database *sql.DB
}

// NewAssessmentStore 创建 Task 评估持久化服务。
func NewAssessmentStore(database *sql.DB) *AssessmentStore {
	return &AssessmentStore{database: database}
}

// ApplyTriageResult 校验 Triage Run 后创建评估，并且只更新未锁定标题。
func (store *AssessmentStore) ApplyTriageResult(
	ctx context.Context,
	runID string,
	input assessment.Input,
) (assessment.Assessment, task.Task, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return assessment.Assessment{}, task.Task{}, err
	}
	calculation, err := assessment.Calculate(input.Scores)
	if err != nil {
		return assessment.Assessment{}, task.Task{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return assessment.Assessment{}, task.Task{}, fmt.Errorf("begin task assessment: %w", err)
	}
	defer transaction.Rollback()
	currentRun, currentTask, err := loadTriageSubject(ctx, transaction, runID)
	if err != nil {
		return assessment.Assessment{}, task.Task{}, err
	}
	if currentRun.SubjectVersion != currentTask.AssessmentInputVersion {
		return assessment.Assessment{}, task.Task{}, ErrAssessmentInputConflict
	}
	updatedTask, err := applySuggestedTitle(ctx, transaction, currentTask, input.SuggestedTitle)
	if err != nil {
		return assessment.Assessment{}, task.Task{}, err
	}
	created, err := buildAssessment(ctx, transaction, currentRun, updatedTask, input, calculation)
	if err != nil {
		return assessment.Assessment{}, task.Task{}, err
	}
	if err := insertAssessment(ctx, transaction, created); err != nil {
		return assessment.Assessment{}, task.Task{}, err
	}
	if err := transaction.Commit(); err != nil {
		return assessment.Assessment{}, task.Task{}, fmt.Errorf("commit task assessment: %w", err)
	}
	return created, updatedTask, nil
}

// GetCurrent 返回 Task 最新评估。
func (store *AssessmentStore) GetCurrent(ctx context.Context, taskID string) (assessment.Assessment, error) {
	item, err := scanAssessment(store.database.QueryRowContext(ctx, `
SELECT id, task_id, task_assessment_version, revision, suggested_title, applied_title,
       technical_complexity, requirement_uncertainty, change_scope, validation_burden,
       human_dependency, risk_and_reversibility, weighted_score, complexity, autonomy,
       confidence, rationale, assumptions_json, split_recommended, split_rationale,
       source_run_id, created_at
FROM task_assessments WHERE task_id = ? ORDER BY revision DESC LIMIT 1`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return assessment.Assessment{}, ErrAssessmentNotFound
	}
	return item, err
}

// List 按新到旧返回 Task 评估历史。
func (store *AssessmentStore) List(ctx context.Context, taskID string) ([]assessment.Assessment, error) {
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return nil, err
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT id, task_id, task_assessment_version, revision, suggested_title, applied_title,
       technical_complexity, requirement_uncertainty, change_scope, validation_burden,
       human_dependency, risk_and_reversibility, weighted_score, complexity, autonomy,
       confidence, rationale, assumptions_json, split_recommended, split_rationale,
       source_run_id, created_at
FROM task_assessments WHERE task_id = ? ORDER BY revision DESC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task assessments: %w", err)
	}
	defer rows.Close()
	result := make([]assessment.Assessment, 0)
	for rows.Next() {
		item, scanErr := scanAssessment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// ListCurrent 返回所有 Task 的最新评估，供看板一次性构建只读投影。
func (store *AssessmentStore) ListCurrent(ctx context.Context) (map[string]assessment.Assessment, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, task_id, task_assessment_version, revision, suggested_title, applied_title,
       technical_complexity, requirement_uncertainty, change_scope, validation_burden,
       human_dependency, risk_and_reversibility, weighted_score, complexity, autonomy,
       confidence, rationale, assumptions_json, split_recommended, split_rationale,
       source_run_id, created_at
FROM task_assessments a
WHERE revision = (SELECT MAX(current.revision) FROM task_assessments current WHERE current.task_id = a.task_id)`)
	if err != nil {
		return nil, fmt.Errorf("list current task assessments: %w", err)
	}
	defer rows.Close()
	result := make(map[string]assessment.Assessment)
	for rows.Next() {
		item, scanErr := scanAssessment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result[item.TaskID] = item
	}
	return result, rows.Err()
}

func loadTriageSubject(
	ctx context.Context,
	transaction *sql.Tx,
	runID string,
) (domainrun.Run, task.Task, error) {
	currentRun, err := getRun(ctx, transaction, runID)
	if err != nil {
		return domainrun.Run{}, task.Task{}, err
	}
	if currentRun.Purpose != domainrun.PurposeTriage || currentRun.Status != domainrun.StatusRunning {
		return domainrun.Run{}, task.Task{}, ErrRunStateConflict
	}
	currentTask, err := getTask(ctx, transaction, currentRun.TaskID)
	return currentRun, currentTask, err
}

func applySuggestedTitle(
	ctx context.Context,
	transaction *sql.Tx,
	current task.Task,
	suggested string,
) (task.Task, error) {
	if current.TitleLocked {
		return current, nil
	}
	updated, event, err := prepareTitleUpdate(current, suggested, task.TitleSourceAI, false)
	if err != nil {
		return task.Task{}, err
	}
	if err := updateTaskTitle(ctx, transaction, current, updated); err != nil {
		return task.Task{}, err
	}
	if err := insertTaskEvent(ctx, transaction, event); err != nil {
		return task.Task{}, err
	}
	return updated, nil
}

func buildAssessment(
	ctx context.Context,
	transaction *sql.Tx,
	run domainrun.Run,
	currentTask task.Task,
	input assessment.Input,
	calculation assessment.Calculation,
) (assessment.Assessment, error) {
	id, err := newID()
	if err != nil {
		return assessment.Assessment{}, err
	}
	var revision int64
	if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(MAX(revision), 0) + 1 FROM task_assessments WHERE task_id = ?`, run.TaskID).Scan(&revision); err != nil {
		return assessment.Assessment{}, fmt.Errorf("read next assessment revision: %w", err)
	}
	return assessment.Assessment{
		ID: id, TaskID: run.TaskID, TaskAssessmentVersion: run.SubjectVersion,
		Revision: revision, SuggestedTitle: input.SuggestedTitle, AppliedTitle: currentTask.Title,
		Scores: input.Scores, WeightedScore: calculation.WeightedScore,
		Complexity: calculation.Complexity, Autonomy: calculation.Autonomy,
		Confidence: input.Confidence, Rationale: input.Rationale, Assumptions: input.Assumptions,
		SplitRecommended: input.SplitRecommended, SplitRationale: input.SplitRationale,
		SourceRunID: run.ID, CreatedAt: time.Now().UTC(),
	}, nil
}

func insertAssessment(ctx context.Context, transaction *sql.Tx, item assessment.Assessment) error {
	assumptions, err := json.Marshal(item.Assumptions)
	if err != nil {
		return fmt.Errorf("encode assessment assumptions: %w", err)
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO task_assessments(
    id, task_id, task_assessment_version, revision, suggested_title, applied_title,
    technical_complexity, requirement_uncertainty, change_scope, validation_burden,
    human_dependency, risk_and_reversibility, weighted_score, complexity, autonomy,
    confidence, rationale, assumptions_json, split_recommended, split_rationale,
    source_run_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.TaskID, item.TaskAssessmentVersion, item.Revision,
		item.SuggestedTitle, item.AppliedTitle,
		item.Scores.TechnicalComplexity, item.Scores.RequirementUncertainty,
		item.Scores.ChangeScope, item.Scores.ValidationBurden, item.Scores.HumanDependency,
		item.Scores.RiskAndReversibility, item.WeightedScore, item.Complexity, item.Autonomy,
		item.Confidence, item.Rationale, string(assumptions), item.SplitRecommended,
		item.SplitRationale, item.SourceRunID, formatTime(item.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert task assessment: %w", err)
	}
	return nil
}

func scanAssessment(scanner rowScanner) (assessment.Assessment, error) {
	var item assessment.Assessment
	var assumptionsJSON, createdAt string
	err := scanner.Scan(
		&item.ID, &item.TaskID, &item.TaskAssessmentVersion, &item.Revision,
		&item.SuggestedTitle, &item.AppliedTitle,
		&item.Scores.TechnicalComplexity, &item.Scores.RequirementUncertainty,
		&item.Scores.ChangeScope, &item.Scores.ValidationBurden, &item.Scores.HumanDependency,
		&item.Scores.RiskAndReversibility, &item.WeightedScore, &item.Complexity,
		&item.Autonomy, &item.Confidence, &item.Rationale, &assumptionsJSON,
		&item.SplitRecommended, &item.SplitRationale, &item.SourceRunID, &createdAt,
	)
	if err != nil {
		return assessment.Assessment{}, err
	}
	if err := json.Unmarshal([]byte(assumptionsJSON), &item.Assumptions); err != nil {
		return assessment.Assessment{}, fmt.Errorf("decode assessment assumptions: %w", err)
	}
	item.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return assessment.Assessment{}, fmt.Errorf("parse assessment created_at: %w", err)
	}
	return item, nil
}
