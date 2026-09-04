package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/domain/plan"
	"github.com/light-speak/aitodos/internal/domain/quality"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/topic"
)

var (
	// ErrPlanNotFound 表示 Topic 尚无 Plan。
	ErrPlanNotFound = errors.New("plan not found")
	// ErrPlanConflict 表示 Plan 或 Topic 已被其他操作更新。
	ErrPlanConflict = errors.New("plan conflict")
)

// PlanStore 持久化不可变 Plan Revision 和审核命令。
type PlanStore struct {
	database *sql.DB
}

// NewPlanStore 创建 Plan 持久化服务。
func NewPlanStore(database *sql.DB) *PlanStore {
	return &PlanStore{database: database}
}

// GetByTopic 返回 Topic 当前 Plan 读取模型。
func (store *PlanStore) GetByTopic(ctx context.Context, topicID string) (plan.View, error) {
	current, err := scanPlan(store.database.QueryRowContext(ctx, `
SELECT id, plan_key, topic_id, status, current_revision_id,
       COALESCE(approved_revision_id, ''), version, created_at, updated_at
FROM plans WHERE topic_id = ?`, topicID))
	if errors.Is(err, sql.ErrNoRows) {
		return plan.View{}, ErrPlanNotFound
	}
	if err != nil {
		return plan.View{}, err
	}
	revision, err := loadPlanRevision(ctx, store.database, current.CurrentRevisionID)
	if err != nil {
		return plan.View{}, err
	}
	reviews, err := listPlanReviews(ctx, store.database, current.ID)
	if err != nil {
		return plan.View{}, err
	}
	return plan.View{Plan: current, Revision: revision, Reviews: reviews}, nil
}

// CreateRevision 创建新 Plan 或在驳回后追加不可变 Revision。
func (store *PlanStore) CreateRevision(
	ctx context.Context,
	topicID string,
	expectedTopicVersion int64,
	input plan.RevisionInput,
) (plan.View, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return plan.View{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return plan.View{}, fmt.Errorf("begin plan revision: %w", err)
	}
	defer transaction.Rollback()
	currentTopic, err := getTopic(ctx, transaction, topicID)
	if err != nil {
		return plan.View{}, err
	}
	if currentTopic.Version != expectedTopicVersion || currentTopic.Status != topic.StatusOpen {
		return plan.View{}, ErrPlanConflict
	}
	currentPlan, creating, err := findOrCreatePlan(ctx, transaction, topicID)
	if err != nil {
		return plan.View{}, err
	}
	if !creating && currentPlan.Status != plan.StatusChangesRequested {
		return plan.View{}, ErrPlanConflict
	}
	revisionNumber := int64(1)
	if !creating {
		previous, loadErr := loadPlanRevision(ctx, transaction, currentPlan.CurrentRevisionID)
		if loadErr != nil {
			return plan.View{}, loadErr
		}
		revisionNumber = previous.Revision + 1
	}
	revision, err := buildPlanRevision(currentPlan, revisionNumber, input)
	if err != nil {
		return plan.View{}, err
	}
	if creating {
		currentPlan.CurrentRevisionID = revision.ID
		if err := insertPlan(ctx, transaction, currentPlan); err != nil {
			return plan.View{}, err
		}
	} else if err := activatePlanRevision(ctx, transaction, &currentPlan, revision.ID); err != nil {
		return plan.View{}, err
	}
	if err := insertPlanRevision(ctx, transaction, revision); err != nil {
		return plan.View{}, err
	}
	updatedTopic, event, err := prepareTopicTransition(currentTopic, topic.StatusPlanReview, topic.CommandSubmitPlan)
	if err != nil {
		return plan.View{}, err
	}
	updatedTopic.CurrentPlanID = currentPlan.ID
	if err := updateTopicForPlan(ctx, transaction, currentTopic, updatedTopic); err != nil {
		return plan.View{}, err
	}
	if err := insertTopicEvent(ctx, transaction, event); err != nil {
		return plan.View{}, err
	}
	if err := transaction.Commit(); err != nil {
		return plan.View{}, fmt.Errorf("commit plan revision: %w", err)
	}
	return store.GetByTopic(ctx, topicID)
}

// RequestChanges 驳回当前 Revision 并让 Topic 回到讨论中。
func (store *PlanStore) RequestChanges(ctx context.Context, planID string, input plan.ReviewInput) (plan.View, error) {
	if err := input.ValidateChangesRequest(); err != nil {
		return plan.View{}, err
	}
	return store.review(ctx, planID, input, false)
}

// Approve 批准当前 Revision，并原子创建正式 Task、测试项与 Topic 关系。
func (store *PlanStore) Approve(ctx context.Context, planID string, input plan.ReviewInput) (plan.View, []task.Task, error) {
	transaction, currentPlan, currentTopic, revision, err := store.beginReview(ctx, planID, input)
	if err != nil {
		return plan.View{}, nil, err
	}
	defer transaction.Rollback()
	createdTasks := make([]task.Task, 0, len(revision.Drafts))
	for _, draft := range revision.Drafts {
		created, event, buildErr := newTaskAndEvent(task.CreateInput{
			Title: draft.Title, Description: draft.Description,
			AcceptanceCriteria: draft.AcceptanceCriteria, Priority: draft.Priority,
			TitleSource: task.TitleSourceHuman, SourcePlanRevisionID: revision.ID,
			SourcePlanTaskDraftID: draft.ID,
		})
		if buildErr != nil {
			return plan.View{}, nil, buildErr
		}
		if err := insertTask(ctx, transaction, created); err != nil {
			return plan.View{}, nil, err
		}
		if err := insertTaskEvent(ctx, transaction, event); err != nil {
			return plan.View{}, nil, err
		}
		if err := linkTopicTask(ctx, transaction, currentTopic.ID, created.ID, ""); err != nil {
			return plan.View{}, nil, err
		}
		if err := createApprovedTestCases(ctx, transaction, created.ID, draft.TestCases); err != nil {
			return plan.View{}, nil, err
		}
		createdTasks = append(createdTasks, created)
	}
	if err := finishPlanReview(ctx, transaction, &currentPlan, &currentTopic, revision.ID, input.Comment, true); err != nil {
		return plan.View{}, nil, err
	}
	if err := transaction.Commit(); err != nil {
		return plan.View{}, nil, fmt.Errorf("commit plan approval: %w", err)
	}
	view, err := store.GetByTopic(ctx, currentTopic.ID)
	return view, createdTasks, err
}

func (store *PlanStore) review(ctx context.Context, planID string, input plan.ReviewInput, approved bool) (plan.View, error) {
	transaction, currentPlan, currentTopic, revision, err := store.beginReview(ctx, planID, input)
	if err != nil {
		return plan.View{}, err
	}
	defer transaction.Rollback()
	if err := finishPlanReview(ctx, transaction, &currentPlan, &currentTopic, revision.ID, input.Comment, approved); err != nil {
		return plan.View{}, err
	}
	if err := transaction.Commit(); err != nil {
		return plan.View{}, fmt.Errorf("commit plan review: %w", err)
	}
	return store.GetByTopic(ctx, currentTopic.ID)
}

func (store *PlanStore) beginReview(
	ctx context.Context,
	planID string,
	input plan.ReviewInput,
) (*sql.Tx, plan.Plan, topic.Topic, plan.Revision, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, plan.Plan{}, topic.Topic{}, plan.Revision{}, err
	}
	currentPlan, err := getPlan(ctx, transaction, planID)
	if err != nil {
		transaction.Rollback()
		return nil, plan.Plan{}, topic.Topic{}, plan.Revision{}, err
	}
	currentTopic, err := getTopic(ctx, transaction, currentPlan.TopicID)
	if err != nil {
		transaction.Rollback()
		return nil, plan.Plan{}, topic.Topic{}, plan.Revision{}, err
	}
	if currentPlan.Status != plan.StatusInReview || currentTopic.Status != topic.StatusPlanReview ||
		currentTopic.Version != input.ExpectedTopicVersion || currentPlan.CurrentRevisionID != input.RevisionID {
		transaction.Rollback()
		return nil, plan.Plan{}, topic.Topic{}, plan.Revision{}, ErrPlanConflict
	}
	revision, err := loadPlanRevision(ctx, transaction, input.RevisionID)
	if err != nil {
		transaction.Rollback()
		return nil, plan.Plan{}, topic.Topic{}, plan.Revision{}, err
	}
	return transaction, currentPlan, currentTopic, revision, nil
}

func finishPlanReview(
	ctx context.Context,
	transaction *sql.Tx,
	currentPlan *plan.Plan,
	currentTopic *topic.Topic,
	revisionID string,
	comment string,
	approved bool,
) error {
	decision := plan.StatusChangesRequested
	command := topic.CommandRequestPlanChanges
	nextTopicStatus := topic.StatusOpen
	if approved {
		decision = plan.StatusApproved
		command = topic.CommandApprovePlan
		nextTopicStatus = topic.StatusPlanned
	}
	updatedTopic, event, err := prepareTopicTransition(*currentTopic, nextTopicStatus, command)
	if err != nil {
		return err
	}
	now := updatedTopic.UpdatedAt
	approvedRevision := any(nil)
	if approved {
		approvedRevision = revisionID
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE plans SET status = ?, approved_revision_id = ?, version = version + 1, updated_at = ?
WHERE id = ? AND status = 'IN_REVIEW' AND current_revision_id = ? AND version = ?`,
		decision, approvedRevision, formatTime(now), currentPlan.ID, revisionID, currentPlan.Version)
	if err != nil {
		return err
	}
	if err := requireSingleChange(result); err != nil {
		return ErrPlanConflict
	}
	if err := insertPlanReview(ctx, transaction, *currentPlan, revisionID, decision, comment, now); err != nil {
		return err
	}
	if err := updateTopicForPlan(ctx, transaction, *currentTopic, updatedTopic); err != nil {
		return err
	}
	if err := insertTopicEvent(ctx, transaction, event); err != nil {
		return err
	}
	currentPlan.Status = decision
	currentPlan.Version++
	currentPlan.UpdatedAt = now
	if approved {
		currentPlan.ApprovedRevisionID = revisionID
	}
	*currentTopic = updatedTopic
	return nil
}

func findOrCreatePlan(ctx context.Context, transaction *sql.Tx, topicID string) (plan.Plan, bool, error) {
	current, err := scanPlan(transaction.QueryRowContext(ctx, `
SELECT id, plan_key, topic_id, status, current_revision_id,
       COALESCE(approved_revision_id, ''), version, created_at, updated_at
FROM plans WHERE topic_id = ?`, topicID))
	if err == nil {
		return current, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return plan.Plan{}, false, err
	}
	id, err := newID()
	if err != nil {
		return plan.Plan{}, false, err
	}
	now := time.Now().UTC()
	return plan.Plan{ID: id, Key: planKey(id), TopicID: topicID, Status: plan.StatusInReview, Version: 1, CreatedAt: now, UpdatedAt: now}, true, nil
}

func buildPlanRevision(current plan.Plan, revisionNumber int64, input plan.RevisionInput) (plan.Revision, error) {
	id, err := newID()
	if err != nil {
		return plan.Revision{}, err
	}
	revision := plan.Revision{
		ID: id, PlanID: current.ID, Revision: revisionNumber,
		Summary: input.Summary, Rationale: input.Rationale, Risks: input.Risks,
		SourceRunID: input.SourceRunID, PreviousRevisionID: current.CurrentRevisionID,
		CreatedAt: time.Now().UTC(), Drafts: make([]plan.TaskDraft, 0, len(input.Drafts)),
	}
	for index, inputDraft := range input.Drafts {
		draftID, idErr := newID()
		if idErr != nil {
			return plan.Revision{}, idErr
		}
		draft := plan.TaskDraft{
			ID: draftID, PlanRevisionID: id, Key: fmt.Sprintf("T%d", index+1),
			Title: inputDraft.Title, Description: inputDraft.Description,
			AcceptanceCriteria: inputDraft.AcceptanceCriteria,
			Priority:           inputDraft.Priority, ProposedOrder: index,
		}
		for testIndex, inputTest := range inputDraft.TestCases {
			testID, testIDErr := newID()
			if testIDErr != nil {
				return plan.Revision{}, testIDErr
			}
			draft.TestCases = append(draft.TestCases, plan.TestCaseDraft{
				ID: testID, TaskDraftID: draftID, Title: inputTest.Title,
				Description: inputTest.Description, Required: inputTest.Required, SortOrder: testIndex,
			})
		}
		revision.Drafts = append(revision.Drafts, draft)
	}
	return revision, nil
}

func insertPlan(ctx context.Context, transaction *sql.Tx, current plan.Plan) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO plans(id, plan_key, topic_id, status, current_revision_id, approved_revision_id,
                  version, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, NULL, ?, ?, ?)`, current.ID, current.Key, current.TopicID, current.Status,
		current.CurrentRevisionID, current.Version, formatTime(current.CreatedAt), formatTime(current.UpdatedAt))
	return err
}

func activatePlanRevision(ctx context.Context, transaction *sql.Tx, current *plan.Plan, revisionID string) error {
	now := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE plans SET status = 'IN_REVIEW', current_revision_id = ?, version = version + 1, updated_at = ?
WHERE id = ? AND status = 'CHANGES_REQUESTED' AND version = ?`, revisionID, formatTime(now), current.ID, current.Version)
	if err != nil {
		return err
	}
	if err := requireSingleChange(result); err != nil {
		return ErrPlanConflict
	}
	current.Status = plan.StatusInReview
	current.CurrentRevisionID = revisionID
	current.Version++
	current.UpdatedAt = now
	return nil
}

func insertPlanRevision(ctx context.Context, transaction *sql.Tx, revision plan.Revision) error {
	readinessJSON, err := marshalPlanReadiness(revision.Readiness)
	if err != nil {
		return err
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO plan_revisions(id, plan_id, revision_number, summary, rationale, risks,
                           readiness_json, source_run_id, previous_revision_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?)`, revision.ID, revision.PlanID,
		revision.Revision, revision.Summary, revision.Rationale, revision.Risks,
		readinessJSON, revision.SourceRunID, revision.PreviousRevisionID, formatTime(revision.CreatedAt))
	if err != nil {
		return err
	}
	for _, draft := range revision.Drafts {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO plan_task_drafts(id, plan_revision_id, draft_key, title, description,
                             acceptance_criteria, priority, proposed_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, draft.ID, draft.PlanRevisionID, draft.Key, draft.Title,
			draft.Description, draft.AcceptanceCriteria, draft.Priority, draft.ProposedOrder); err != nil {
			return err
		}
		for _, testCase := range draft.TestCases {
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO plan_task_draft_test_cases(id, task_draft_id, title, description, required, sort_order)
VALUES (?, ?, ?, ?, ?, ?)`, testCase.ID, testCase.TaskDraftID, testCase.Title,
				testCase.Description, testCase.Required, testCase.SortOrder); err != nil {
				return err
			}
		}
	}
	return nil
}

func marshalPlanReadiness(readiness *plan.ReadinessAssessment) (any, error) {
	if readiness == nil {
		return nil, nil
	}
	normalized := readiness.Normalized()
	if err := normalized.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode plan readiness: %w", err)
	}
	return string(encoded), nil
}

func updateTopicForPlan(ctx context.Context, transaction *sql.Tx, current, updated topic.Topic) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE topics SET status = ?, current_plan_id = ?, version = ?, updated_at = ?
WHERE id = ? AND status = ? AND version = ?`, updated.Status, updated.CurrentPlanID,
		updated.Version, formatTime(updated.UpdatedAt), current.ID, current.Status, current.Version)
	if err != nil {
		return err
	}
	if err := requireSingleChange(result); err != nil {
		return ErrPlanConflict
	}
	return nil
}

func createApprovedTestCases(ctx context.Context, transaction *sql.Tx, taskID string, drafts []plan.TestCaseDraft) error {
	now := time.Now().UTC()
	for _, draft := range drafts {
		input := quality.TestCaseInput{
			Title: draft.Title, Description: draft.Description, Required: draft.Required,
			SortOrder: draft.SortOrder, CreatedBy: quality.TestCreatorHuman,
		}
		if err := input.Validate(); err != nil {
			return err
		}
		id, err := newID()
		if err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO task_test_cases(id, task_id, title, description, required, sort_order,
                            created_by, source_run_id, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, 'HUMAN', NULL, ?, ?)`, id, taskID, input.Title,
			input.Description, input.Required, input.SortOrder, formatTime(now), formatTime(now)); err != nil {
			return err
		}
	}
	return nil
}

func insertPlanReview(ctx context.Context, transaction *sql.Tx, current plan.Plan, revisionID string, decision plan.Status, comment string, now time.Time) error {
	id, err := newID()
	if err != nil {
		return err
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO plan_reviews(id, plan_id, plan_revision_id, decision, comment, created_at)
VALUES (?, ?, ?, ?, ?, ?)`, id, current.ID, revisionID, decision, strings.TrimSpace(comment), formatTime(now))
	return err
}

func getPlan(ctx context.Context, queryer rowQueryer, planID string) (plan.Plan, error) {
	current, err := scanPlan(queryer.QueryRowContext(ctx, `
SELECT id, plan_key, topic_id, status, current_revision_id,
       COALESCE(approved_revision_id, ''), version, created_at, updated_at
FROM plans WHERE id = ?`, planID))
	if errors.Is(err, sql.ErrNoRows) {
		return plan.Plan{}, ErrPlanNotFound
	}
	return current, err
}

func scanPlan(scanner rowScanner) (plan.Plan, error) {
	var current plan.Plan
	var createdAt, updatedAt string
	if err := scanner.Scan(&current.ID, &current.Key, &current.TopicID, &current.Status,
		&current.CurrentRevisionID, &current.ApprovedRevisionID, &current.Version,
		&createdAt, &updatedAt); err != nil {
		return plan.Plan{}, err
	}
	var err error
	current.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return plan.Plan{}, err
	}
	current.UpdatedAt, err = parseTime(updatedAt)
	return current, err
}

func loadPlanRevision(ctx context.Context, queryer rowQueryer, revisionID string) (plan.Revision, error) {
	var revision plan.Revision
	var readinessJSON sql.NullString
	var createdAt string
	err := queryer.QueryRowContext(ctx, `
SELECT id, plan_id, revision_number, summary, rationale, risks,
       readiness_json, COALESCE(source_run_id, ''), COALESCE(previous_revision_id, ''), created_at
FROM plan_revisions WHERE id = ?`, revisionID).Scan(&revision.ID, &revision.PlanID,
		&revision.Revision, &revision.Summary, &revision.Rationale, &revision.Risks,
		&readinessJSON, &revision.SourceRunID, &revision.PreviousRevisionID, &createdAt)
	if err != nil {
		return plan.Revision{}, err
	}
	if readinessJSON.Valid {
		revision.Readiness = &plan.ReadinessAssessment{}
		if err := json.Unmarshal([]byte(readinessJSON.String), revision.Readiness); err != nil {
			return plan.Revision{}, fmt.Errorf("decode plan readiness: %w", err)
		}
	}
	revision.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return plan.Revision{}, err
	}
	revision.Drafts, err = listPlanDrafts(ctx, queryer, revisionID)
	return revision, err
}

func listPlanDrafts(ctx context.Context, queryer rowQueryer, revisionID string) ([]plan.TaskDraft, error) {
	rows, err := queryRows(ctx, queryer, `
SELECT id, plan_revision_id, draft_key, title, description, acceptance_criteria, priority, proposed_order
FROM plan_task_drafts WHERE plan_revision_id = ? ORDER BY proposed_order`, revisionID)
	if err != nil {
		return nil, err
	}
	drafts := make([]plan.TaskDraft, 0)
	for rows.Next() {
		var draft plan.TaskDraft
		if err := rows.Scan(&draft.ID, &draft.PlanRevisionID, &draft.Key, &draft.Title,
			&draft.Description, &draft.AcceptanceCriteria, &draft.Priority, &draft.ProposedOrder); err != nil {
			rows.Close()
			return nil, err
		}
		drafts = append(drafts, draft)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range drafts {
		drafts[index].TestCases, err = listPlanDraftTests(ctx, queryer, drafts[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return drafts, nil
}

type rowsQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func queryRows(ctx context.Context, queryer rowQueryer, query string, args ...any) (*sql.Rows, error) {
	rowsSource, ok := queryer.(rowsQueryer)
	if !ok {
		return nil, errors.New("queryer does not support rows")
	}
	return rowsSource.QueryContext(ctx, query, args...)
}

func listPlanDraftTests(ctx context.Context, queryer rowQueryer, draftID string) ([]plan.TestCaseDraft, error) {
	rows, err := queryRows(ctx, queryer, `
SELECT id, task_draft_id, title, description, required, sort_order
FROM plan_task_draft_test_cases WHERE task_draft_id = ? ORDER BY sort_order`, draftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tests := make([]plan.TestCaseDraft, 0)
	for rows.Next() {
		var testCase plan.TestCaseDraft
		if err := rows.Scan(&testCase.ID, &testCase.TaskDraftID, &testCase.Title,
			&testCase.Description, &testCase.Required, &testCase.SortOrder); err != nil {
			return nil, err
		}
		tests = append(tests, testCase)
	}
	return tests, rows.Err()
}

func listPlanReviews(ctx context.Context, queryer rowQueryer, planID string) ([]plan.Review, error) {
	rows, err := queryRows(ctx, queryer, `
SELECT id, plan_id, plan_revision_id, decision, comment, created_at
FROM plan_reviews WHERE plan_id = ? ORDER BY created_at DESC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reviews := make([]plan.Review, 0)
	for rows.Next() {
		var review plan.Review
		var createdAt string
		if err := rows.Scan(&review.ID, &review.PlanID, &review.PlanRevisionID,
			&review.Decision, &review.Comment, &createdAt); err != nil {
			return nil, err
		}
		review.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, review)
	}
	return reviews, rows.Err()
}

func planKey(id string) string {
	return "PLN-" + strings.ToUpper(strings.ReplaceAll(id, "-", "")[:8])
}
