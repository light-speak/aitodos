package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/domain/quality"
	"github.com/light-speak/aitodos/internal/domain/task"
)

// QualityStore 持久化 Task 估算和测试证据，并生成项目进度投影。
type QualityStore struct {
	database *sql.DB
}

// NewQualityStore 创建质量与进度持久化服务。
func NewQualityStore(database *sql.DB) *QualityStore {
	return &QualityStore{database: database}
}

// CreateEstimate 追加一个不可变估算修订。
func (store *QualityStore) CreateEstimate(ctx context.Context, taskID string, input quality.EstimateInput) (quality.Estimate, error) {
	input.Rationale = strings.TrimSpace(input.Rationale)
	input.SourceRunID = strings.TrimSpace(input.SourceRunID)
	if err := input.Validate(); err != nil {
		return quality.Estimate{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return quality.Estimate{}, fmt.Errorf("begin estimate revision: %w", err)
	}
	defer transaction.Rollback()
	if err := requireTask(ctx, transaction, taskID); err != nil {
		return quality.Estimate{}, err
	}
	var revision int64
	if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(MAX(revision), 0) + 1 FROM task_estimates WHERE task_id = ?`, taskID).Scan(&revision); err != nil {
		return quality.Estimate{}, fmt.Errorf("read next estimate revision: %w", err)
	}
	id, err := newID()
	if err != nil {
		return quality.Estimate{}, err
	}
	created := quality.Estimate{
		ID: id, TaskID: taskID, Revision: revision, Points: input.Points,
		RemainingPoints: input.RemainingPoints, Confidence: input.Confidence,
		Rationale: input.Rationale, Source: input.Source, SourceRunID: input.SourceRunID,
		CreatedAt: time.Now().UTC(),
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO task_estimates(
    id, task_id, revision, points, remaining_points, confidence,
    rationale, source, source_run_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?)`,
		created.ID, created.TaskID, created.Revision, created.Points, created.RemainingPoints,
		created.Confidence, created.Rationale, created.Source, created.SourceRunID,
		formatTime(created.CreatedAt),
	); err != nil {
		return quality.Estimate{}, fmt.Errorf("insert task estimate: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return quality.Estimate{}, fmt.Errorf("commit task estimate: %w", err)
	}
	return created, nil
}

// EnsureRequiredTestsPassed 校验 Task 的全部必测项都有当前可验证通过证据。
func (store *QualityStore) EnsureRequiredTestsPassed(ctx context.Context, taskID string) error {
	return ensureRequiredTestsPassed(ctx, store.database, taskID)
}

// CreateTestCase 创建一个长期测试项。
func (store *QualityStore) CreateTestCase(ctx context.Context, taskID string, input quality.TestCaseInput) (quality.TestCase, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.SourceRunID = strings.TrimSpace(input.SourceRunID)
	if err := input.Validate(); err != nil {
		return quality.TestCase{}, err
	}
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return quality.TestCase{}, err
	}
	id, err := newID()
	if err != nil {
		return quality.TestCase{}, err
	}
	now := time.Now().UTC()
	created := quality.TestCase{
		ID: id, TaskID: taskID, Title: input.Title, Description: input.Description,
		Required: input.Required, SortOrder: input.SortOrder, CreatedBy: input.CreatedBy,
		SourceRunID: input.SourceRunID, CreatedAt: now, UpdatedAt: now,
	}
	_, err = store.database.ExecContext(ctx, `
INSERT INTO task_test_cases(
    id, task_id, title, description, required, sort_order, created_by,
    source_run_id, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?)`,
		created.ID, created.TaskID, created.Title, created.Description, created.Required,
		created.SortOrder, created.CreatedBy, created.SourceRunID,
		formatTime(created.CreatedAt), formatTime(created.UpdatedAt),
	)
	if err != nil {
		return quality.TestCase{}, fmt.Errorf("insert task test case: %w", err)
	}
	return created, nil
}

// AddTestResult 追加一次测试结论，不覆盖历史。
func (store *QualityStore) AddTestResult(
	ctx context.Context,
	taskID string,
	testCaseID string,
	input quality.TestResultInput,
) (quality.TestResult, error) {
	input.Summary = strings.TrimSpace(input.Summary)
	input.Command = strings.TrimSpace(input.Command)
	input.ArtifactRef = strings.TrimSpace(input.ArtifactRef)
	input.SourceRunID = strings.TrimSpace(input.SourceRunID)
	if err := input.Validate(); err != nil {
		return quality.TestResult{}, err
	}
	var exists int
	if err := store.database.QueryRowContext(ctx, `
SELECT 1 FROM task_test_cases WHERE id = ? AND task_id = ?`, testCaseID, taskID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return quality.TestResult{}, ErrTaskNotFound
	} else if err != nil {
		return quality.TestResult{}, fmt.Errorf("read task test case: %w", err)
	}
	id, err := newID()
	if err != nil {
		return quality.TestResult{}, err
	}
	created := quality.TestResult{
		ID: id, TestCaseID: testCaseID, TaskID: taskID, Outcome: input.Outcome,
		EvidenceKind: input.EvidenceKind, Summary: input.Summary, Command: input.Command,
		ExitCode: input.ExitCode, ArtifactRef: input.ArtifactRef, SourceRunID: input.SourceRunID,
		CreatedAt: time.Now().UTC(),
	}
	_, err = store.database.ExecContext(ctx, `
INSERT INTO task_test_results(
    id, test_case_id, task_id, outcome, evidence_kind, summary,
    command, exit_code, artifact_ref, source_run_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?)`,
		created.ID, created.TestCaseID, created.TaskID, created.Outcome, created.EvidenceKind,
		created.Summary, created.Command, created.ExitCode, created.ArtifactRef, created.SourceRunID,
		formatTime(created.CreatedAt),
	)
	if err != nil {
		return quality.TestResult{}, fmt.Errorf("insert task test result: %w", err)
	}
	return created, nil
}

// GetTaskQuality 返回 Task 当前估算、估算历史和测试项最新证据。
func (store *QualityStore) GetTaskQuality(ctx context.Context, taskID string) (quality.TaskQuality, error) {
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return quality.TaskQuality{}, err
	}
	estimates, err := store.listEstimates(ctx, taskID)
	if err != nil {
		return quality.TaskQuality{}, err
	}
	testCases, err := store.listTestCases(ctx, taskID)
	if err != nil {
		return quality.TaskQuality{}, err
	}
	result := quality.TaskQuality{EstimateHistory: estimates, TestCases: testCases}
	if len(estimates) > 0 {
		current := estimates[0]
		result.Estimate = &current
	}
	return result, nil
}

// ProjectProgress 生成严格完成度、估算覆盖率、预测进度与测试证据统计。
func (store *QualityStore) ProjectProgress(ctx context.Context) (quality.ProjectProgress, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT t.status, e.points, e.remaining_points
FROM tasks t
LEFT JOIN task_estimates e ON e.task_id = t.id AND e.revision = (
    SELECT MAX(current.revision) FROM task_estimates current WHERE current.task_id = t.id
)
WHERE t.status != 'CANCELLED'`)
	if err != nil {
		return quality.ProjectProgress{}, fmt.Errorf("read project progress tasks: %w", err)
	}
	var progress quality.ProjectProgress
	for rows.Next() {
		var status task.Status
		var points, remaining sql.NullInt64
		if err := rows.Scan(&status, &points, &remaining); err != nil {
			rows.Close()
			return quality.ProjectProgress{}, err
		}
		progress.TotalTasks++
		if status == task.StatusAccepted {
			progress.AcceptedTasks++
		}
		if points.Valid {
			progress.EstimatedTasks++
			progress.TotalPoints += int(points.Int64)
			if status != task.StatusAccepted {
				progress.RemainingPoints += int(remaining.Int64)
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return quality.ProjectProgress{}, err
	}
	if err := rows.Close(); err != nil {
		return quality.ProjectProgress{}, err
	}
	if progress.TotalTasks > 0 {
		progress.StrictPercent = percent(progress.AcceptedTasks, progress.TotalTasks)
		progress.EstimateCoverage = percent(progress.EstimatedTasks, progress.TotalTasks)
	}
	if progress.TotalPoints > 0 {
		forecast := 100 * float64(progress.TotalPoints-progress.RemainingPoints) / float64(progress.TotalPoints)
		progress.ForecastPercent = &forecast
	}
	if err := store.readTestProgress(ctx, &progress); err != nil {
		return quality.ProjectProgress{}, err
	}
	return progress, nil
}

func (store *QualityStore) listEstimates(ctx context.Context, taskID string) ([]quality.Estimate, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, task_id, revision, points, remaining_points, confidence, rationale,
       source, COALESCE(source_run_id, ''), created_at
FROM task_estimates WHERE task_id = ? ORDER BY revision DESC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task estimates: %w", err)
	}
	defer rows.Close()
	result := make([]quality.Estimate, 0)
	for rows.Next() {
		var item quality.Estimate
		var createdAt string
		if err := rows.Scan(
			&item.ID, &item.TaskID, &item.Revision, &item.Points, &item.RemainingPoints,
			&item.Confidence, &item.Rationale, &item.Source, &item.SourceRunID, &createdAt,
		); err != nil {
			return nil, err
		}
		item.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse task estimate time: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *QualityStore) listTestCases(ctx context.Context, taskID string) ([]quality.TestCase, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT c.id, c.task_id, c.title, c.description, c.required, c.sort_order,
       c.created_by, COALESCE(c.source_run_id, ''), c.created_at, c.updated_at,
       COALESCE(r.id, ''), COALESCE(r.outcome, ''), COALESCE(r.evidence_kind, ''),
       COALESCE(r.summary, ''), COALESCE(r.command, ''), r.exit_code, COALESCE(r.artifact_ref, ''),
       COALESCE(r.source_run_id, ''), COALESCE(r.created_at, ''),
       CASE WHEN r.id IS NOT NULL AND r.created_at <= COALESCE((
           SELECT MAX(i.updated_at) FROM task_integration_attempts i
           WHERE i.task_id = c.task_id AND i.operation = 'SYNC'
             AND i.status IN ('SYNCED', 'CONFLICT')
       ), '') THEN 1 ELSE 0 END
FROM task_test_cases c
LEFT JOIN task_test_results r ON r.id = (
    SELECT latest.id FROM task_test_results latest
    WHERE latest.test_case_id = c.id ORDER BY latest.created_at DESC, latest.id DESC LIMIT 1
)
WHERE c.task_id = ? ORDER BY c.sort_order, c.created_at`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task test cases: %w", err)
	}
	defer rows.Close()
	result := make([]quality.TestCase, 0)
	for rows.Next() {
		item, scanErr := scanTestCase(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func scanTestCase(scanner rowScanner) (quality.TestCase, error) {
	var item quality.TestCase
	var createdAt, updatedAt string
	var result quality.TestResult
	var exitCode sql.NullInt64
	var resultCreatedAt string
	err := scanner.Scan(
		&item.ID, &item.TaskID, &item.Title, &item.Description, &item.Required,
		&item.SortOrder, &item.CreatedBy, &item.SourceRunID, &createdAt, &updatedAt,
		&result.ID, &result.Outcome, &result.EvidenceKind, &result.Summary, &result.Command, &exitCode,
		&result.ArtifactRef, &result.SourceRunID, &resultCreatedAt,
		&result.Stale,
	)
	if err != nil {
		return quality.TestCase{}, err
	}
	item.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return quality.TestCase{}, err
	}
	item.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return quality.TestCase{}, err
	}
	if result.ID != "" {
		if exitCode.Valid {
			value := int(exitCode.Int64)
			result.ExitCode = &value
		}
		result.TestCaseID = item.ID
		result.TaskID = item.TaskID
		result.CreatedAt, err = parseTime(resultCreatedAt)
		if err != nil {
			return quality.TestCase{}, err
		}
		item.LatestResult = &result
	}
	return item, nil
}

func (store *QualityStore) readTestProgress(ctx context.Context, progress *quality.ProjectProgress) error {
	return store.database.QueryRowContext(ctx, `
SELECT
    COUNT(*),
    COALESCE(SUM(CASE WHEN r.outcome = 'PASSED'
        AND (r.evidence_kind = 'HUMAN' OR (r.evidence_kind = 'COMMAND' AND r.exit_code = 0))
        AND r.created_at > COALESCE((SELECT MAX(i.updated_at) FROM task_integration_attempts i
            WHERE i.task_id = c.task_id AND i.operation = 'SYNC'
              AND i.status IN ('SYNCED', 'CONFLICT')), '') THEN 1 ELSE 0 END), 0),
    COALESCE(SUM(CASE WHEN r.outcome = 'PASSED' AND r.evidence_kind = 'AGENT_REPORT' THEN 1 ELSE 0 END), 0)
FROM task_test_cases c
LEFT JOIN task_test_results r ON r.id = (
    SELECT latest.id FROM task_test_results latest
    WHERE latest.test_case_id = c.id ORDER BY latest.created_at DESC, latest.id DESC LIMIT 1
)
WHERE c.required = 1`).Scan(
		&progress.RequiredTests, &progress.VerifiedPassedTests, &progress.AgentReportedPassedTests,
	)
}

func ensureRequiredTestsPassed(ctx context.Context, queryer rowQueryer, taskID string) error {
	var missing int
	err := queryer.QueryRowContext(ctx, `
SELECT COUNT(*) FROM task_test_cases c
LEFT JOIN task_test_results r ON r.id = (
    SELECT latest.id FROM task_test_results latest
    WHERE latest.test_case_id = c.id ORDER BY latest.created_at DESC, latest.id DESC LIMIT 1
)
WHERE c.task_id = ? AND c.required = 1
  AND (r.outcome IS NULL OR r.outcome != 'PASSED'
       OR (r.evidence_kind != 'HUMAN' AND
           (r.evidence_kind != 'COMMAND' OR r.exit_code IS NULL OR r.exit_code != 0))
       OR r.created_at <= COALESCE((SELECT MAX(i.updated_at) FROM task_integration_attempts i
           WHERE i.task_id = c.task_id AND i.operation = 'SYNC'
             AND i.status IN ('SYNCED', 'CONFLICT')), ''))`, taskID).Scan(&missing)
	if err != nil {
		return fmt.Errorf("check required tests: %w", err)
	}
	if missing > 0 {
		return ErrRequiredTestsNotPassed
	}
	return nil
}

func percent(numerator int, denominator int) float64 {
	return 100 * float64(numerator) / float64(denominator)
}
