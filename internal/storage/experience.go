package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/light-speak/aitodos/internal/domain/experience"
	domainrun "github.com/light-speak/aitodos/internal/domain/run"
)

var (
	// ErrExperienceNotFound 表示经验不存在。
	ErrExperienceNotFound = errors.New("experience not found")
	// ErrExperienceSubjectMismatch 表示被替代经验不属于同一个来源主体。
	ErrExperienceSubjectMismatch = errors.New("superseded experience belongs to another subject")
	// ErrExperienceRunSubjectMismatch 表示候选经验与来源 Run 的主体或职责不一致。
	ErrExperienceRunSubjectMismatch = errors.New("experience candidate does not match source run")
	// ErrExperienceRecallNotFound 表示召回记录不存在。
	ErrExperienceRecallNotFound = errors.New("experience recall not found")
)

const experienceColumns = `records.id, records.experience_key,
COALESCE(records.topic_id, ''), COALESCE(records.task_id, ''), records.title, records.summary,
records.guidance, records.applicability, records.project_wide, records.status, records.pinned,
records.verification_count, records.successful_applications, records.failed_applications,
(SELECT COUNT(*) FROM experience_recalls WHERE experience_id = records.id),
COALESCE(records.source_run_id, ''), COALESCE(records.supersedes_experience_id, ''),
records.created_at, records.updated_at`

// ExperienceStore 持久化经验资产、动态召回依据和应用结果。
type ExperienceStore struct {
	database *sql.DB
}

// NewExperienceStore 创建经验持久化服务。
func NewExperienceStore(database *sql.DB) *ExperienceStore {
	return &ExperienceStore{database: database}
}

// CreateVerified 创建一条由人类确认的 ACTIVE 经验，并原子替代旧记录。
func (store *ExperienceStore) CreateVerified(ctx context.Context, input experience.Input) (experience.Record, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return experience.Record{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return experience.Record{}, fmt.Errorf("begin experience creation: %w", err)
	}
	defer transaction.Rollback()
	if err := requireExperienceSubject(ctx, transaction, input); err != nil {
		return experience.Record{}, err
	}
	if input.SupersedesExperienceID != "" {
		if err := supersedeExperience(ctx, transaction, input); err != nil {
			return experience.Record{}, err
		}
	}
	id, err := newID()
	if err != nil {
		return experience.Record{}, err
	}
	now := time.Now().UTC()
	created := experience.Record{
		ID: id, Key: "EXP-" + strings.ToUpper(strings.ReplaceAll(id, "-", "")[:8]),
		TopicID: input.TopicID, TaskID: input.TaskID, Title: input.Title, Summary: input.Summary,
		Guidance: input.Guidance, Applicability: input.Applicability, ProjectWide: input.ProjectWide,
		Status: experience.StatusActive, Pinned: input.Pinned, VerificationCount: 1,
		SourceRunID: input.SourceRunID, SupersedesExperienceID: input.SupersedesExperienceID,
		CreatedAt: now, UpdatedAt: now,
	}
	_, err = transaction.ExecContext(ctx, `INSERT INTO experience_records(
id, experience_key, topic_id, task_id, title, summary, guidance, applicability, project_wide,
status, pinned, verification_count, successful_applications, failed_applications,
source_run_id, supersedes_experience_id, created_at, updated_at
) VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, 1, 0, 0, NULLIF(?, ''), NULLIF(?, ''), ?, ?)`,
		created.ID, created.Key, created.TopicID, created.TaskID, created.Title, created.Summary,
		created.Guidance, created.Applicability, boolInt(created.ProjectWide), created.Status,
		boolInt(created.Pinned), created.SourceRunID, created.SupersedesExperienceID,
		formatTime(created.CreatedAt), formatTime(created.UpdatedAt))
	if err != nil {
		return experience.Record{}, fmt.Errorf("insert experience: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return experience.Record{}, fmt.Errorf("commit experience creation: %w", err)
	}
	return created, nil
}

// CreateCandidate 创建一条由 Task Run 提出的候选经验；同一 Run 的相同内容幂等返回。
func (store *ExperienceStore) CreateCandidate(ctx context.Context, input experience.Input) (experience.Record, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return experience.Record{}, err
	}
	if input.TopicID != "" || input.SourceRunID == "" || input.Pinned || input.SupersedesExperienceID != "" {
		return experience.Record{}, ErrExperienceRunSubjectMismatch
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return experience.Record{}, fmt.Errorf("begin experience candidate creation: %w", err)
	}
	defer transaction.Rollback()
	if err := requireCandidateRun(ctx, transaction, input.TaskID, input.SourceRunID); err != nil {
		return experience.Record{}, err
	}
	fingerprint := experienceCandidateFingerprint(input)
	id, err := newID()
	if err != nil {
		return experience.Record{}, err
	}
	now := time.Now().UTC()
	created := experience.Record{
		ID: id, Key: "EXP-" + strings.ToUpper(strings.ReplaceAll(id, "-", "")[:8]),
		TaskID: input.TaskID, Title: input.Title, Summary: input.Summary, Guidance: input.Guidance,
		Applicability: input.Applicability, ProjectWide: input.ProjectWide, Status: experience.StatusCandidate,
		SourceRunID: input.SourceRunID, CreatedAt: now, UpdatedAt: now,
	}
	inserted, err := transaction.ExecContext(ctx, `INSERT INTO experience_records(
id, experience_key, task_id, title, summary, guidance, applicability, project_wide,
status, pinned, verification_count, successful_applications, failed_applications,
source_run_id, candidate_fingerprint, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'CANDIDATE', 0, 0, 0, 0, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`,
		created.ID, created.Key, created.TaskID, created.Title, created.Summary, created.Guidance,
		created.Applicability, boolInt(created.ProjectWide), created.SourceRunID, fingerprint,
		formatTime(created.CreatedAt), formatTime(created.UpdatedAt))
	if err != nil {
		return experience.Record{}, fmt.Errorf("insert experience candidate: %w", err)
	}
	if changed, err := inserted.RowsAffected(); err != nil {
		return experience.Record{}, fmt.Errorf("read experience candidate insertion: %w", err)
	} else if changed == 0 {
		existing, loadErr := scanExperience(transaction.QueryRowContext(ctx, `SELECT `+experienceColumns+`
FROM experience_records records WHERE records.source_run_id = ? AND records.candidate_fingerprint = ?`,
			input.SourceRunID, fingerprint))
		if loadErr != nil {
			return experience.Record{}, fmt.Errorf("resolve experience candidate conflict: %w", loadErr)
		}
		return existing, nil
	}
	if err := transaction.Commit(); err != nil {
		return experience.Record{}, fmt.Errorf("commit experience candidate creation: %w", err)
	}
	return created, nil
}

func requireCandidateRun(ctx context.Context, transaction *sql.Tx, taskID, runID string) error {
	var purpose domainrun.Purpose
	var runTaskID, runTopicID string
	err := transaction.QueryRowContext(ctx, `SELECT purpose, COALESCE(task_id, ''), COALESCE(topic_id, '')
FROM runs WHERE id = ?`, runID).Scan(&purpose, &runTaskID, &runTopicID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrExperienceRunSubjectMismatch
	}
	if err != nil {
		return fmt.Errorf("load experience source run: %w", err)
	}
	if runTopicID != "" || runTaskID != taskID ||
		(purpose != domainrun.PurposeImplementation && purpose != domainrun.PurposeRevision) {
		return ErrExperienceRunSubjectMismatch
	}
	return nil
}

func experienceCandidateFingerprint(input experience.Input) string {
	content := strings.Join([]string{
		input.Title, input.Summary, input.Guidance, input.Applicability, fmt.Sprintf("%t", input.ProjectWide),
	}, "\x00")
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func requireExperienceSubject(ctx context.Context, transaction *sql.Tx, input experience.Input) error {
	if input.TopicID != "" {
		return requireTopic(ctx, transaction, input.TopicID)
	}
	return requireTask(ctx, transaction, input.TaskID)
}

func supersedeExperience(ctx context.Context, transaction *sql.Tx, input experience.Input) error {
	var topicID, taskID string
	err := transaction.QueryRowContext(ctx, `SELECT COALESCE(topic_id, ''), COALESCE(task_id, '')
FROM experience_records WHERE id = ?`, input.SupersedesExperienceID).Scan(&topicID, &taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrExperienceNotFound
	}
	if err != nil {
		return fmt.Errorf("load superseded experience: %w", err)
	}
	if topicID != input.TopicID || taskID != input.TaskID {
		return ErrExperienceSubjectMismatch
	}
	_, err = transaction.ExecContext(ctx, `UPDATE experience_records SET status = 'SUPERSEDED', updated_at = ? WHERE id = ?`,
		formatTime(time.Now().UTC()), input.SupersedesExperienceID)
	return err
}

// Get 返回单条经验及其召回统计。
func (store *ExperienceStore) Get(ctx context.Context, id string) (experience.Record, error) {
	return scanExperience(store.database.QueryRowContext(ctx, `SELECT `+experienceColumns+` FROM experience_records records WHERE records.id = ?`, id))
}

// ListTopic 返回 Topic 产生的经验。
func (store *ExperienceStore) ListTopic(ctx context.Context, topicID string, includeInactive bool) ([]experience.Record, error) {
	if err := requireTopic(ctx, store.database, topicID); err != nil {
		return nil, err
	}
	return store.list(ctx, "topic_id", topicID, includeInactive)
}

// ListTask 返回 Task 产生的经验。
func (store *ExperienceStore) ListTask(ctx context.Context, taskID string, includeInactive bool) ([]experience.Record, error) {
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return nil, err
	}
	return store.list(ctx, "task_id", taskID, includeInactive)
}

func (store *ExperienceStore) list(ctx context.Context, column, id string, includeInactive bool) ([]experience.Record, error) {
	query := `SELECT ` + experienceColumns + ` FROM experience_records records WHERE records.` + column + ` = ?`
	if !includeInactive {
		query += " AND records.status = 'ACTIVE'"
	}
	query += " ORDER BY records.updated_at DESC, records.id DESC LIMIT 100"
	rows, err := store.database.QueryContext(ctx, query, id)
	if err != nil {
		return nil, fmt.Errorf("list experiences: %w", err)
	}
	defer rows.Close()
	items := make([]experience.Record, 0)
	for rows.Next() {
		item, scanErr := scanExperience(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// SetPinned 设置人工固定状态；固定只影响动态评分，不绕过 ACTIVE 状态。
func (store *ExperienceStore) SetPinned(ctx context.Context, id string, pinned bool) (experience.Record, error) {
	result, err := store.database.ExecContext(ctx, `UPDATE experience_records SET pinned = ?, updated_at = ? WHERE id = ?`,
		boolInt(pinned), formatTime(time.Now().UTC()), strings.TrimSpace(id))
	if err != nil {
		return experience.Record{}, fmt.Errorf("pin experience: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return experience.Record{}, ErrExperienceNotFound
	}
	return store.Get(ctx, id)
}

// ConfirmCandidate 将人工确认的候选经验激活，使其可以进入后续召回。
func (store *ExperienceStore) ConfirmCandidate(ctx context.Context, id string) (experience.Record, error) {
	result, err := store.database.ExecContext(ctx, `UPDATE experience_records
SET status = 'ACTIVE', verification_count = verification_count + 1, updated_at = ?
WHERE id = ? AND status = 'CANDIDATE'`, formatTime(time.Now().UTC()), strings.TrimSpace(id))
	if err != nil {
		return experience.Record{}, fmt.Errorf("confirm experience candidate: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		if _, getErr := store.Get(ctx, id); getErr != nil {
			return experience.Record{}, getErr
		}
		return experience.Record{}, errors.New("experience is not a candidate")
	}
	return store.Get(ctx, id)
}

// Challenge 标记经验存在反例，使其不再进入后续 Context。
func (store *ExperienceStore) Challenge(ctx context.Context, id string) (experience.Record, error) {
	result, err := store.database.ExecContext(ctx, `UPDATE experience_records SET status = 'CHALLENGED', updated_at = ?
WHERE id = ? AND status IN ('ACTIVE', 'CANDIDATE')`, formatTime(time.Now().UTC()), strings.TrimSpace(id))
	if err != nil {
		return experience.Record{}, fmt.Errorf("challenge experience: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		if _, getErr := store.Get(ctx, id); getErr != nil {
			return experience.Record{}, getErr
		}
		return experience.Record{}, errors.New("experience cannot be challenged from current status")
	}
	return store.Get(ctx, id)
}

// RecallQuery 描述一次 Run 的有界经验召回。
type RecallQuery struct {
	RunID   string
	Purpose domainrun.Purpose
	TopicID string
	TaskID  string
	Text    string
	Limit   int
	Now     time.Time
}

// RecalledExperience 保存被选中的经验和可解释评分。
type RecalledExperience struct {
	RecallID   string                    `json:"recall_id"`
	Rank       int                       `json:"rank"`
	Experience experience.Record         `json:"experience"`
	Score      experience.ScoreBreakdown `json:"score"`
	Outcome    experience.Outcome        `json:"outcome"`
	RecalledAt time.Time                 `json:"recalled_at"`
}

// Recall 选择当前 Run 相关的 ACTIVE 经验并持久化审计事件。
func (store *ExperienceStore) Recall(ctx context.Context, query RecallQuery) ([]RecalledExperience, error) {
	if err := normalizeRecallQuery(&query); err != nil {
		return nil, err
	}
	candidates, err := store.recallCandidates(ctx, query)
	if err != nil {
		return nil, err
	}
	scored := make([]RecalledExperience, 0, len(candidates))
	recallText := query.Text + "\n" + string(query.Purpose)
	for _, candidate := range candidates {
		scope := recallScope(candidate, query)
		relevance := experience.LexicalRelevance(recallText, candidate.Title, candidate.Summary, candidate.Applicability)
		if relevance < 0.08 && !candidate.Pinned {
			continue
		}
		freshness := math.Exp2(-query.Now.Sub(candidate.UpdatedAt).Hours() / (24 * 180))
		breakdown := experience.Score(experience.InputSignals{
			Relevance: relevance, ScopeMatch: scope, Freshness: freshness,
			VerificationCount: candidate.VerificationCount, SuccessCount: candidate.SuccessfulApplications,
			FailureCount: candidate.FailedApplications, Pinned: candidate.Pinned,
		})
		scored = append(scored, RecalledExperience{Experience: candidate, Score: breakdown, Outcome: experience.OutcomePending})
	}
	sort.SliceStable(scored, func(left, right int) bool {
		if scored[left].Score.Final == scored[right].Score.Final {
			return scored[left].Experience.UpdatedAt.After(scored[right].Experience.UpdatedAt)
		}
		return scored[left].Score.Final > scored[right].Score.Final
	})
	if len(scored) > query.Limit {
		scored = scored[:query.Limit]
	}
	return store.persistRecalls(ctx, query, scored)
}

func normalizeRecallQuery(query *RecallQuery) error {
	query.RunID = strings.TrimSpace(query.RunID)
	query.TopicID = strings.TrimSpace(query.TopicID)
	query.TaskID = strings.TrimSpace(query.TaskID)
	query.Text = strings.TrimSpace(query.Text)
	if query.Limit == 0 {
		query.Limit = 5
	}
	if query.Now.IsZero() {
		query.Now = time.Now().UTC()
	} else {
		query.Now = query.Now.UTC()
	}
	if query.RunID == "" || query.Text == "" || query.Limit < 1 || query.Limit > 10 || (query.TopicID == "") == (query.TaskID == "") {
		return errors.New("experience recall query is invalid")
	}
	return nil
}

func (store *ExperienceStore) recallCandidates(ctx context.Context, query RecallQuery) ([]experience.Record, error) {
	rows, err := store.database.QueryContext(ctx, `SELECT `+experienceColumns+` FROM experience_records records
WHERE records.status = 'ACTIVE' AND (
    records.project_wide = 1 OR records.topic_id = NULLIF(?, '') OR records.task_id = NULLIF(?, '') OR
    records.topic_id IN (SELECT topic_id FROM topic_task_links WHERE task_id = NULLIF(?, ''))
)
ORDER BY records.pinned DESC, records.updated_at DESC LIMIT 1000`, query.TopicID, query.TaskID, query.TaskID)
	if err != nil {
		return nil, fmt.Errorf("load experience recall candidates: %w", err)
	}
	defer rows.Close()
	items := make([]experience.Record, 0)
	for rows.Next() {
		item, scanErr := scanExperience(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func recallScope(record experience.Record, query RecallQuery) float64 {
	if record.TaskID != "" && record.TaskID == query.TaskID || record.TopicID != "" && record.TopicID == query.TopicID {
		return 1
	}
	if !record.ProjectWide && record.TopicID != "" && query.TaskID != "" {
		return 0.85
	}
	return 0.6
}

func (store *ExperienceStore) persistRecalls(ctx context.Context, query RecallQuery, items []RecalledExperience) ([]RecalledExperience, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin experience recall: %w", err)
	}
	defer transaction.Rollback()
	for index := range items {
		id, idErr := newID()
		if idErr != nil {
			return nil, idErr
		}
		items[index].RecallID = id
		items[index].Rank = index + 1
		items[index].RecalledAt = query.Now
		_, err = transaction.ExecContext(ctx, `INSERT INTO experience_recalls(
id, run_id, experience_id, rank, relevance_score, utility_score, scope_score, freshness_score,
final_score, estimated_tokens, outcome, recalled_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'PENDING', ?)
ON CONFLICT(run_id, experience_id) DO NOTHING`, id, query.RunID, items[index].Experience.ID,
			items[index].Rank, items[index].Score.Relevance, items[index].Score.Utility, items[index].Score.Scope,
			items[index].Score.Freshness, items[index].Score.Final, estimateExperienceTokens(items[index].Experience.Summary),
			formatTime(query.Now))
		if err != nil {
			return nil, fmt.Errorf("insert experience recall: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit experience recall: %w", err)
	}
	return items, nil
}

// RecordOutcome 幂等修改召回评价，并同步经验应用统计。
func (store *ExperienceStore) RecordOutcome(ctx context.Context, recallID string, outcome experience.Outcome) error {
	if outcome != experience.OutcomeHelpful && outcome != experience.OutcomeHarmful && outcome != experience.OutcomeIgnored {
		return errors.New("experience recall outcome is invalid")
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin experience outcome: %w", err)
	}
	defer transaction.Rollback()
	var experienceID string
	var previous experience.Outcome
	err = transaction.QueryRowContext(ctx, `SELECT experience_id, outcome FROM experience_recalls WHERE id = ?`, recallID).Scan(&experienceID, &previous)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrExperienceRecallNotFound
	}
	if err != nil {
		return fmt.Errorf("load experience recall: %w", err)
	}
	if previous == outcome {
		return transaction.Commit()
	}
	if err := adjustOutcomeCounters(ctx, transaction, experienceID, previous, -1); err != nil {
		return err
	}
	if err := adjustOutcomeCounters(ctx, transaction, experienceID, outcome, 1); err != nil {
		return err
	}
	_, err = transaction.ExecContext(ctx, `UPDATE experience_recalls SET outcome = ?, evaluated_at = ? WHERE id = ?`,
		outcome, formatTime(time.Now().UTC()), recallID)
	if err != nil {
		return fmt.Errorf("update experience recall outcome: %w", err)
	}
	return transaction.Commit()
}

// ListRunRecalls 返回一个 Run 已选择的经验和当时评分，不重新计算历史。
func (store *ExperienceStore) ListRunRecalls(ctx context.Context, runID string) ([]RecalledExperience, error) {
	rows, err := store.database.QueryContext(ctx, `SELECT recalls.id, recalls.rank, `+experienceColumns+`,
recalls.relevance_score, recalls.utility_score, recalls.scope_score, recalls.freshness_score,
recalls.final_score, recalls.outcome, recalls.recalled_at
FROM experience_recalls recalls JOIN experience_records records ON records.id = recalls.experience_id
WHERE recalls.run_id = ? ORDER BY recalls.rank LIMIT 10`, strings.TrimSpace(runID))
	if err != nil {
		return nil, fmt.Errorf("list run experience recalls: %w", err)
	}
	defer rows.Close()
	items := make([]RecalledExperience, 0)
	for rows.Next() {
		var item RecalledExperience
		var record experience.Record
		var projectWide, pinned int
		var createdAt, updatedAt, recalledAt string
		if err := rows.Scan(&item.RecallID, &item.Rank, &record.ID, &record.Key, &record.TopicID, &record.TaskID,
			&record.Title, &record.Summary, &record.Guidance, &record.Applicability, &projectWide, &record.Status,
			&pinned, &record.VerificationCount, &record.SuccessfulApplications, &record.FailedApplications,
			&record.RecallCount, &record.SourceRunID, &record.SupersedesExperienceID, &createdAt, &updatedAt,
			&item.Score.Relevance, &item.Score.Utility, &item.Score.Scope, &item.Score.Freshness,
			&item.Score.Final, &item.Outcome, &recalledAt); err != nil {
			return nil, fmt.Errorf("scan run experience recall: %w", err)
		}
		record.ProjectWide = projectWide != 0
		record.Pinned = pinned != 0
		record.CreatedAt, err = parseTime(createdAt)
		if err == nil {
			record.UpdatedAt, err = parseTime(updatedAt)
		}
		if err == nil {
			item.RecalledAt, err = parseTime(recalledAt)
		}
		if err != nil {
			return nil, err
		}
		item.Experience = record
		items = append(items, item)
	}
	return items, rows.Err()
}

func adjustOutcomeCounters(ctx context.Context, transaction *sql.Tx, experienceID string, outcome experience.Outcome, delta int) error {
	column := ""
	if outcome == experience.OutcomeHelpful {
		column = "successful_applications"
	} else if outcome == experience.OutcomeHarmful {
		column = "failed_applications"
	}
	if column == "" {
		return nil
	}
	_, err := transaction.ExecContext(ctx, `UPDATE experience_records SET `+column+` = `+column+` + ?, updated_at = ?
WHERE id = ? AND `+column+` + ? >= 0`, delta, formatTime(time.Now().UTC()), experienceID, delta)
	if err != nil {
		return fmt.Errorf("update experience outcome counters: %w", err)
	}
	return nil
}

func scanExperience(scanner rowScanner) (experience.Record, error) {
	var item experience.Record
	var projectWide, pinned int
	var createdAt, updatedAt string
	err := scanner.Scan(&item.ID, &item.Key, &item.TopicID, &item.TaskID, &item.Title, &item.Summary,
		&item.Guidance, &item.Applicability, &projectWide, &item.Status, &pinned,
		&item.VerificationCount, &item.SuccessfulApplications, &item.FailedApplications, &item.RecallCount,
		&item.SourceRunID, &item.SupersedesExperienceID, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return experience.Record{}, ErrExperienceNotFound
	}
	if err != nil {
		return experience.Record{}, fmt.Errorf("scan experience: %w", err)
	}
	item.ProjectWide = projectWide != 0
	item.Pinned = pinned != 0
	item.CreatedAt, err = parseTime(createdAt)
	if err == nil {
		item.UpdatedAt, err = parseTime(updatedAt)
	}
	return item, err
}

func estimateExperienceTokens(value string) int {
	ascii := 0
	for _, current := range value {
		if current <= 0x7f {
			ascii++
		}
	}
	return (ascii+3)/4 + utf8.RuneCountInString(value) - ascii
}
