package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/light-speak/aitodos/internal/domain/knowledge"
)

var (
	// ErrDecisionNotFound 表示决策不存在。
	ErrDecisionNotFound = errors.New("decision not found")
	// ErrDecisionSubjectMismatch 表示被替代决策不属于同一个 Topic 或 Task。
	ErrDecisionSubjectMismatch = errors.New("superseded decision belongs to another subject")
	// ErrLabelNotFound 表示标签不存在。
	ErrLabelNotFound = errors.New("label not found")
	// ErrRunSummaryNotFound 表示 Run 尚无摘要投影。
	ErrRunSummaryNotFound = errors.New("run summary not found")
)

// KnowledgeStore 持久化可搜索、可重建的项目知识与外部检查快照。
type KnowledgeStore struct {
	database *sql.DB
}

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// NewKnowledgeStore 创建知识持久化服务。
func NewKnowledgeStore(database *sql.DB) *KnowledgeStore {
	return &KnowledgeStore{database: database}
}

// CreateDecision 创建不可变决策，并在同一事务中替代旧决策。
func (store *KnowledgeStore) CreateDecision(ctx context.Context, input knowledge.DecisionInput) (knowledge.Decision, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return knowledge.Decision{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return knowledge.Decision{}, fmt.Errorf("begin decision creation: %w", err)
	}
	defer transaction.Rollback()
	if err := requireDecisionSubject(ctx, transaction, input); err != nil {
		return knowledge.Decision{}, err
	}
	if input.SupersedesDecisionID != "" {
		if err := supersedeDecision(ctx, transaction, input); err != nil {
			return knowledge.Decision{}, err
		}
	}
	id, err := newID()
	if err != nil {
		return knowledge.Decision{}, err
	}
	now := time.Now().UTC()
	created := knowledge.Decision{
		ID: id, Key: "DEC-" + strings.ToUpper(strings.ReplaceAll(id, "-", "")[:8]),
		TopicID: input.TopicID, TaskID: input.TaskID, Title: input.Title, Content: input.Content,
		Status: knowledge.DecisionStatusActive, SupersedesDecisionID: input.SupersedesDecisionID, CreatedAt: now,
	}
	_, err = transaction.ExecContext(ctx, `INSERT INTO decisions(
id, decision_key, topic_id, task_id, title, content, status, supersedes_decision_id, created_at
) VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, NULLIF(?, ''), ?)`,
		created.ID, created.Key, created.TopicID, created.TaskID, created.Title, created.Content,
		created.Status, created.SupersedesDecisionID, formatTime(created.CreatedAt))
	if err != nil {
		return knowledge.Decision{}, fmt.Errorf("insert decision: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return knowledge.Decision{}, fmt.Errorf("commit decision creation: %w", err)
	}
	return created, nil
}

// ListTopicDecisions 返回 Topic 的决策，新决策在前。
func (store *KnowledgeStore) ListTopicDecisions(ctx context.Context, topicID string, includeSuperseded bool) ([]knowledge.Decision, error) {
	if err := requireTopic(ctx, store.database, topicID); err != nil {
		return nil, err
	}
	return store.listDecisions(ctx, "topic_id", topicID, includeSuperseded)
}

// ListTaskDecisions 返回 Task 的决策，新决策在前。
func (store *KnowledgeStore) ListTaskDecisions(ctx context.Context, taskID string, includeSuperseded bool) ([]knowledge.Decision, error) {
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return nil, err
	}
	return store.listDecisions(ctx, "task_id", taskID, includeSuperseded)
}

func (store *KnowledgeStore) listDecisions(ctx context.Context, subjectColumn, subjectID string, includeSuperseded bool) ([]knowledge.Decision, error) {
	query := `SELECT id, decision_key, COALESCE(topic_id, ''), COALESCE(task_id, ''), title, content,
status, COALESCE(supersedes_decision_id, ''), created_at FROM decisions WHERE ` + subjectColumn + ` = ?`
	if !includeSuperseded {
		query += " AND status = 'ACTIVE'"
	}
	query += " ORDER BY created_at DESC, id DESC"
	rows, err := store.database.QueryContext(ctx, query, subjectID)
	if err != nil {
		return nil, fmt.Errorf("list decisions: %w", err)
	}
	defer rows.Close()
	items := make([]knowledge.Decision, 0)
	for rows.Next() {
		item, scanErr := scanDecision(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate decisions: %w", err)
	}
	return items, nil
}

// CreateLabel 创建项目级标签。
func (store *KnowledgeStore) CreateLabel(ctx context.Context, input knowledge.LabelInput) (knowledge.Label, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return knowledge.Label{}, err
	}
	id, err := newID()
	if err != nil {
		return knowledge.Label{}, err
	}
	created := knowledge.Label{ID: id, Name: input.Name, Color: input.Color, CreatedAt: time.Now().UTC()}
	if _, err := store.database.ExecContext(ctx, `INSERT INTO labels(id, name, color, created_at) VALUES (?, ?, ?, ?)`,
		created.ID, created.Name, created.Color, formatTime(created.CreatedAt)); err != nil {
		return knowledge.Label{}, fmt.Errorf("insert label: %w", err)
	}
	return created, nil
}

// ListLabels 返回项目全部标签。
func (store *KnowledgeStore) ListLabels(ctx context.Context) ([]knowledge.Label, error) {
	rows, err := store.database.QueryContext(ctx, `SELECT id, name, color, created_at FROM labels ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}
	return scanLabels(rows)
}

// AttachTopicLabel 幂等绑定 Topic 标签。
func (store *KnowledgeStore) AttachTopicLabel(ctx context.Context, topicID, labelID string) error {
	return store.attachLabel(ctx, "topic_labels", "topic_id", topicID, labelID, true)
}

// AttachTaskLabel 幂等绑定 Task 标签。
func (store *KnowledgeStore) AttachTaskLabel(ctx context.Context, taskID, labelID string) error {
	return store.attachLabel(ctx, "task_labels", "task_id", taskID, labelID, false)
}

func (store *KnowledgeStore) attachLabel(ctx context.Context, table, subjectColumn, subjectID, labelID string, topicSubject bool) error {
	if err := store.requireLabelSubjects(ctx, subjectID, labelID, topicSubject); err != nil {
		return err
	}
	_, err := store.database.ExecContext(ctx, `INSERT INTO `+table+`(`+subjectColumn+`, label_id, created_at)
VALUES (?, ?, ?) ON CONFLICT(`+subjectColumn+`, label_id) DO NOTHING`, subjectID, labelID, formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("attach label: %w", err)
	}
	return nil
}

// DetachTopicLabel 移除 Topic 标签。
func (store *KnowledgeStore) DetachTopicLabel(ctx context.Context, topicID, labelID string) error {
	return store.detachLabel(ctx, "topic_labels", "topic_id", topicID, labelID, true)
}

// DetachTaskLabel 移除 Task 标签。
func (store *KnowledgeStore) DetachTaskLabel(ctx context.Context, taskID, labelID string) error {
	return store.detachLabel(ctx, "task_labels", "task_id", taskID, labelID, false)
}

func (store *KnowledgeStore) detachLabel(ctx context.Context, table, subjectColumn, subjectID, labelID string, topicSubject bool) error {
	if err := store.requireLabelSubjects(ctx, subjectID, labelID, topicSubject); err != nil {
		return err
	}
	if _, err := store.database.ExecContext(ctx, `DELETE FROM `+table+` WHERE `+subjectColumn+` = ? AND label_id = ?`, subjectID, labelID); err != nil {
		return fmt.Errorf("detach label: %w", err)
	}
	return nil
}

// ListTopicLabels 返回 Topic 标签。
func (store *KnowledgeStore) ListTopicLabels(ctx context.Context, topicID string) ([]knowledge.Label, error) {
	if err := requireTopic(ctx, store.database, topicID); err != nil {
		return nil, err
	}
	return store.listSubjectLabels(ctx, "topic_labels", "topic_id", topicID)
}

// ListTaskLabels 返回 Task 标签。
func (store *KnowledgeStore) ListTaskLabels(ctx context.Context, taskID string) ([]knowledge.Label, error) {
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return nil, err
	}
	return store.listSubjectLabels(ctx, "task_labels", "task_id", taskID)
}

func (store *KnowledgeStore) listSubjectLabels(ctx context.Context, table, subjectColumn, subjectID string) ([]knowledge.Label, error) {
	rows, err := store.database.QueryContext(ctx, `SELECT labels.id, labels.name, labels.color, labels.created_at
FROM `+table+` JOIN labels ON labels.id = `+table+`.label_id
WHERE `+table+`.`+subjectColumn+` = ? ORDER BY labels.name COLLATE NOCASE, labels.id`, subjectID)
	if err != nil {
		return nil, fmt.Errorf("list subject labels: %w", err)
	}
	return scanLabels(rows)
}

// UpsertRunSummary 重建 Run 摘要投影；原始 Run 仍为事实来源。
func (store *KnowledgeStore) UpsertRunSummary(ctx context.Context, summary knowledge.RunSummary) error {
	return upsertRunSummary(ctx, store.database, summary)
}

func upsertRunSummary(ctx context.Context, executor sqlExecutor, summary knowledge.RunSummary) error {
	summary.Status = strings.TrimSpace(summary.Status)
	summary.Summary = strings.TrimSpace(summary.Summary)
	if summary.RunID == "" || summary.Status == "" || summary.Summary == "" || utf8.RuneCountInString(summary.Summary) > 4000 || summary.PassedTests < 0 || summary.FailedTests < 0 {
		return errors.New("run summary is invalid")
	}
	if summary.CreatedAt.IsZero() {
		summary.CreatedAt = time.Now().UTC()
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO run_summaries(
run_id, status, summary, passed_tests, failed_tests, created_at
) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET status = excluded.status, summary = excluded.summary,
passed_tests = excluded.passed_tests, failed_tests = excluded.failed_tests, created_at = excluded.created_at`,
		summary.RunID, summary.Status, summary.Summary, summary.PassedTests, summary.FailedTests, formatTime(summary.CreatedAt))
	if err != nil {
		return fmt.Errorf("upsert run summary: %w", err)
	}
	return nil
}

func createRunSummary(ctx context.Context, transaction *sql.Tx, currentStatus, runID, purpose, closureSummary string, createdAt time.Time) error {
	var passed, failed int
	if err := transaction.QueryRowContext(ctx, `SELECT
COALESCE(SUM(CASE WHEN outcome = 'PASSED' THEN 1 ELSE 0 END), 0),
COALESCE(SUM(CASE WHEN outcome IN ('FAILED', 'BLOCKED') THEN 1 ELSE 0 END), 0)
FROM task_test_results WHERE source_run_id = ?`, runID).Scan(&passed, &failed); err != nil {
		return fmt.Errorf("summarize run tests: %w", err)
	}
	summary := strings.TrimSpace(closureSummary)
	if summary == "" {
		summary = purpose + " Run 已完成，状态为 " + currentStatus
	}
	if passed > 0 || failed > 0 {
		summary += fmt.Sprintf("；本次记录测试通过 %d 项、失败或阻塞 %d 项", passed, failed)
	}
	return upsertRunSummary(ctx, transaction, knowledge.RunSummary{
		RunID: runID, Status: currentStatus, Summary: summary,
		PassedTests: passed, FailedTests: failed, CreatedAt: createdAt,
	})
}

// GetRunSummary 返回 Run 的当前可重建摘要。
func (store *KnowledgeStore) GetRunSummary(ctx context.Context, runID string) (knowledge.RunSummary, error) {
	var summary knowledge.RunSummary
	var createdAt string
	err := store.database.QueryRowContext(ctx, `SELECT run_id, status, summary, passed_tests, failed_tests, created_at
FROM run_summaries WHERE run_id = ?`, runID).Scan(&summary.RunID, &summary.Status, &summary.Summary,
		&summary.PassedTests, &summary.FailedTests, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return knowledge.RunSummary{}, ErrRunSummaryNotFound
	}
	if err != nil {
		return knowledge.RunSummary{}, fmt.Errorf("get run summary: %w", err)
	}
	summary.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return knowledge.RunSummary{}, fmt.Errorf("parse run summary time: %w", err)
	}
	return summary, nil
}

// CreateCISnapshot 显式导入不可变 CI 检查快照。
func (store *KnowledgeStore) CreateCISnapshot(ctx context.Context, taskID string, input knowledge.CISnapshotInput) (knowledge.CICheckSnapshot, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return knowledge.CICheckSnapshot{}, err
	}
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return knowledge.CICheckSnapshot{}, err
	}
	id, err := newID()
	if err != nil {
		return knowledge.CICheckSnapshot{}, err
	}
	now := time.Now().UTC()
	if input.ObservedAt.IsZero() {
		input.ObservedAt = now
	} else {
		input.ObservedAt = input.ObservedAt.UTC()
	}
	checks, err := json.Marshal(input.Checks)
	if err != nil {
		return knowledge.CICheckSnapshot{}, fmt.Errorf("encode CI checks: %w", err)
	}
	created := knowledge.CICheckSnapshot{
		ID: id, TaskID: taskID, Provider: input.Provider, CommitSHA: input.CommitSHA, State: input.State,
		Checks: input.Checks, SourceURL: input.SourceURL, ObservedAt: input.ObservedAt, CreatedAt: now,
	}
	_, err = store.database.ExecContext(ctx, `INSERT INTO ci_check_snapshots(
id, task_id, provider, commit_sha, state, checks_json, source_url, observed_at, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, created.ID, created.TaskID, created.Provider, created.CommitSHA,
		created.State, string(checks), created.SourceURL, formatTime(created.ObservedAt), formatTime(created.CreatedAt))
	if err != nil {
		return knowledge.CICheckSnapshot{}, fmt.Errorf("insert CI snapshot: %w", err)
	}
	return created, nil
}

// ListCISnapshots 返回 Task 最近的 CI 检查快照。
func (store *KnowledgeStore) ListCISnapshots(ctx context.Context, taskID string, limit int) ([]knowledge.CICheckSnapshot, error) {
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := store.database.QueryContext(ctx, `SELECT id, task_id, provider, commit_sha, state, checks_json,
source_url, observed_at, created_at FROM ci_check_snapshots
WHERE task_id = ? ORDER BY observed_at DESC, id DESC LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("list CI snapshots: %w", err)
	}
	defer rows.Close()
	items := make([]knowledge.CICheckSnapshot, 0)
	for rows.Next() {
		item, scanErr := scanCISnapshot(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate CI snapshots: %w", err)
	}
	return items, nil
}

func requireDecisionSubject(ctx context.Context, transaction *sql.Tx, input knowledge.DecisionInput) error {
	if input.TopicID != "" {
		return requireTopic(ctx, transaction, input.TopicID)
	}
	return requireTask(ctx, transaction, input.TaskID)
}

func supersedeDecision(ctx context.Context, transaction *sql.Tx, input knowledge.DecisionInput) error {
	var topicID, taskID sql.NullString
	var status knowledge.DecisionStatus
	err := transaction.QueryRowContext(ctx, `SELECT topic_id, task_id, status FROM decisions WHERE id = ?`,
		input.SupersedesDecisionID).Scan(&topicID, &taskID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDecisionNotFound
	}
	if err != nil {
		return fmt.Errorf("find superseded decision: %w", err)
	}
	if topicID.String != input.TopicID || taskID.String != input.TaskID {
		return ErrDecisionSubjectMismatch
	}
	if status != knowledge.DecisionStatusActive {
		return errors.New("decision is already superseded")
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE decisions SET status = 'SUPERSEDED' WHERE id = ? AND status = 'ACTIVE'`, input.SupersedesDecisionID); err != nil {
		return fmt.Errorf("supersede decision: %w", err)
	}
	return nil
}

func (store *KnowledgeStore) requireLabelSubjects(ctx context.Context, subjectID, labelID string, topicSubject bool) error {
	if topicSubject {
		if err := requireTopic(ctx, store.database, subjectID); err != nil {
			return err
		}
	} else if err := requireTask(ctx, store.database, subjectID); err != nil {
		return err
	}
	var exists int
	if err := store.database.QueryRowContext(ctx, "SELECT 1 FROM labels WHERE id = ?", labelID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrLabelNotFound
		}
		return fmt.Errorf("find label: %w", err)
	}
	return nil
}

func scanDecision(scanner rowScanner) (knowledge.Decision, error) {
	var item knowledge.Decision
	var createdAt string
	if err := scanner.Scan(&item.ID, &item.Key, &item.TopicID, &item.TaskID, &item.Title, &item.Content,
		&item.Status, &item.SupersedesDecisionID, &createdAt); err != nil {
		return knowledge.Decision{}, fmt.Errorf("scan decision: %w", err)
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return knowledge.Decision{}, fmt.Errorf("parse decision time: %w", err)
	}
	item.CreatedAt = parsed
	return item, nil
}

func scanLabels(rows *sql.Rows) ([]knowledge.Label, error) {
	defer rows.Close()
	items := make([]knowledge.Label, 0)
	for rows.Next() {
		var item knowledge.Label
		var createdAt string
		if err := rows.Scan(&item.ID, &item.Name, &item.Color, &createdAt); err != nil {
			return nil, fmt.Errorf("scan label: %w", err)
		}
		parsed, err := parseTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse label time: %w", err)
		}
		item.CreatedAt = parsed
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate labels: %w", err)
	}
	return items, nil
}

func scanCISnapshot(scanner rowScanner) (knowledge.CICheckSnapshot, error) {
	var item knowledge.CICheckSnapshot
	var checksJSON, observedAt, createdAt string
	if err := scanner.Scan(&item.ID, &item.TaskID, &item.Provider, &item.CommitSHA, &item.State, &checksJSON,
		&item.SourceURL, &observedAt, &createdAt); err != nil {
		return knowledge.CICheckSnapshot{}, fmt.Errorf("scan CI snapshot: %w", err)
	}
	if err := json.Unmarshal([]byte(checksJSON), &item.Checks); err != nil {
		return knowledge.CICheckSnapshot{}, fmt.Errorf("decode CI checks: %w", err)
	}
	var err error
	item.ObservedAt, err = parseTime(observedAt)
	if err != nil {
		return knowledge.CICheckSnapshot{}, fmt.Errorf("parse CI observed time: %w", err)
	}
	item.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return knowledge.CICheckSnapshot{}, fmt.Errorf("parse CI creation time: %w", err)
	}
	return item, nil
}
