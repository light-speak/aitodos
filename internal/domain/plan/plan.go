// Package plan 定义不可变计划修订及其 Task 草案。
package plan

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Status 表示 Plan 的人工审核状态。
type Status string

const (
	StatusInReview         Status = "IN_REVIEW"
	StatusChangesRequested Status = "CHANGES_REQUESTED"
	StatusApproved         Status = "APPROVED"
)

// Plan 聚合 Topic 下的不可变修订。
type Plan struct {
	ID                 string    `json:"id"`
	Key                string    `json:"key"`
	TopicID            string    `json:"topic_id"`
	Status             Status    `json:"status"`
	CurrentRevisionID  string    `json:"current_revision_id"`
	ApprovedRevisionID string    `json:"approved_revision_id,omitempty"`
	Version            int64     `json:"version"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// Revision 是创建后不可修改的计划内容快照。
type Revision struct {
	ID                 string               `json:"id"`
	PlanID             string               `json:"plan_id"`
	Revision           int64                `json:"revision"`
	Summary            string               `json:"summary"`
	Rationale          string               `json:"rationale"`
	Risks              string               `json:"risks"`
	Readiness          *ReadinessAssessment `json:"readiness,omitempty"`
	SourceRunID        string               `json:"source_run_id,omitempty"`
	PreviousRevisionID string               `json:"previous_revision_id,omitempty"`
	Drafts             []TaskDraft          `json:"drafts"`
	CreatedAt          time.Time            `json:"created_at"`
}

// ReadinessStatus 表示 Planner 是否已有足够信息提交方案审核。
type ReadinessStatus string

const (
	ReadinessNeedsDiscussion ReadinessStatus = "NEEDS_DISCUSSION"
	ReadinessReadyForReview  ReadinessStatus = "READY_FOR_REVIEW"
)

// Alternative 记录形成方案前实际考虑过的可行方向及其取舍。
type Alternative struct {
	Title    string `json:"title"`
	Tradeoff string `json:"tradeoff"`
}

// ReadinessAssessment 保存 Planner 对收敛充分度的结构化判断。
type ReadinessAssessment struct {
	Status        ReadinessStatus `json:"status"`
	Confidence    float64         `json:"confidence"`
	Assumptions   []string        `json:"assumptions"`
	OpenQuestions []string        `json:"open_questions"`
	Alternatives  []Alternative   `json:"alternatives"`
}

// TaskDraft 是 Plan Revision 中尚不可执行的 Task 草案。
type TaskDraft struct {
	ID                 string          `json:"id"`
	PlanRevisionID     string          `json:"plan_revision_id"`
	Key                string          `json:"key"`
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	AcceptanceCriteria string          `json:"acceptance_criteria"`
	Priority           int             `json:"priority"`
	ProposedOrder      int             `json:"proposed_order"`
	TestCases          []TestCaseDraft `json:"test_cases"`
}

// TestCaseDraft 是随 Task 草案审核的测试要求。
type TestCaseDraft struct {
	ID          string `json:"id"`
	TaskDraftID string `json:"task_draft_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	SortOrder   int    `json:"sort_order"`
}

// RevisionInput 是创建新 Revision 的完整输入。
type RevisionInput struct {
	Summary     string           `json:"summary"`
	Rationale   string           `json:"rationale"`
	Risks       string           `json:"risks"`
	SourceRunID string           `json:"source_run_id,omitempty"`
	Drafts      []TaskDraftInput `json:"drafts"`
}

// PlanningResult 是规划 Agent 一轮讨论后提交的结构化结果。
// Reply 始终写回 Topic；Plan 仅在信息充分时提供，并继续等待人工批准。
type PlanningResult struct {
	Reply     string               `json:"reply"`
	Readiness *ReadinessAssessment `json:"readiness"`
	Plan      *RevisionInput       `json:"plan,omitempty"`
}

// Validate 校验 Agent 回复和可选方案草案。
func (result PlanningResult) Validate() error {
	result = result.Normalized()
	if result.Reply == "" || utf8.RuneCountInString(result.Reply) > 20000 {
		return errors.New("planning reply 不能为空且最长 20000 个字符")
	}
	if result.Readiness == nil {
		return errors.New("planning readiness 不能为空")
	}
	if err := result.Readiness.Validate(); err != nil {
		return err
	}
	if result.Plan == nil && result.Readiness.Status != ReadinessNeedsDiscussion {
		return errors.New("没有 Plan 时 readiness 必须为 NEEDS_DISCUSSION")
	}
	if result.Plan != nil && result.Readiness.Status != ReadinessReadyForReview {
		return errors.New("提交 Plan 时 readiness 必须为 READY_FOR_REVIEW")
	}
	if result.Plan != nil {
		return result.Plan.Validate()
	}
	return nil
}

// Normalized 清理规划结果文本。
func (result PlanningResult) Normalized() PlanningResult {
	result.Reply = strings.TrimSpace(result.Reply)
	if result.Readiness != nil {
		normalized := result.Readiness.Normalized()
		result.Readiness = &normalized
	}
	if result.Plan != nil {
		normalized := result.Plan.Normalized()
		result.Plan = &normalized
	}
	return result
}

// Validate 校验充分度判断必须与未决问题一致。
func (assessment ReadinessAssessment) Validate() error {
	assessment = assessment.Normalized()
	if assessment.Status != ReadinessNeedsDiscussion && assessment.Status != ReadinessReadyForReview {
		return errors.New("planning readiness status 无效")
	}
	if assessment.Confidence <= 0 || assessment.Confidence > 1 {
		return errors.New("planning readiness confidence 必须大于 0 且不超过 1")
	}
	if err := validateReadinessStrings(assessment.Assumptions, "assumptions"); err != nil {
		return err
	}
	if err := validateReadinessStrings(assessment.OpenQuestions, "open_questions"); err != nil {
		return err
	}
	if assessment.Status == ReadinessNeedsDiscussion && len(assessment.OpenQuestions) == 0 {
		return errors.New("NEEDS_DISCUSSION 必须列出未决问题")
	}
	if assessment.Status == ReadinessReadyForReview && len(assessment.OpenQuestions) > 0 {
		return errors.New("READY_FOR_REVIEW 不能保留阻塞性未决问题")
	}
	if len(assessment.Alternatives) > 10 {
		return errors.New("alternatives 最多 10 项")
	}
	for _, alternative := range assessment.Alternatives {
		if alternative.Title == "" || alternative.Tradeoff == "" || utf8.RuneCountInString(alternative.Title) > 200 || utf8.RuneCountInString(alternative.Tradeoff) > 1000 {
			return errors.New("alternative 标题和取舍不能为空且不能超过长度限制")
		}
	}
	return nil
}

// Normalized 清理充分度判断中的文本。
func (assessment ReadinessAssessment) Normalized() ReadinessAssessment {
	assessment.Assumptions = normalizeReadinessStrings(assessment.Assumptions)
	assessment.OpenQuestions = normalizeReadinessStrings(assessment.OpenQuestions)
	for index := range assessment.Alternatives {
		assessment.Alternatives[index].Title = strings.TrimSpace(assessment.Alternatives[index].Title)
		assessment.Alternatives[index].Tradeoff = strings.TrimSpace(assessment.Alternatives[index].Tradeoff)
	}
	return assessment
}

func normalizeReadinessStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if normalized := strings.TrimSpace(value); normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

func validateReadinessStrings(values []string, field string) error {
	if len(values) > 20 {
		return errors.New(field + " 最多 20 项")
	}
	for _, value := range values {
		if utf8.RuneCountInString(value) > 1000 {
			return errors.New(field + " 单项最长 1000 个字符")
		}
	}
	return nil
}

// TaskDraftInput 是待审核 Task 草案输入。
type TaskDraftInput struct {
	Title              string          `json:"title"`
	Description        string          `json:"description"`
	AcceptanceCriteria string          `json:"acceptance_criteria"`
	Priority           int             `json:"priority"`
	TestCases          []TestCaseInput `json:"test_cases"`
}

// TestCaseInput 是草案中的测试项输入。
type TestCaseInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// ReviewInput 是人工审核命令。
type ReviewInput struct {
	ExpectedTopicVersion int64  `json:"expected_topic_version"`
	RevisionID           string `json:"revision_id"`
	Comment              string `json:"comment"`
}

// Review 是 Plan Revision 的不可变人工审核记录。
type Review struct {
	ID             string    `json:"id"`
	PlanID         string    `json:"plan_id"`
	PlanRevisionID string    `json:"plan_revision_id"`
	Decision       Status    `json:"decision"`
	Comment        string    `json:"comment"`
	CreatedAt      time.Time `json:"created_at"`
}

// View 是 Topic 当前 Plan、Revision 与审核历史的读取模型。
type View struct {
	Plan     Plan     `json:"plan"`
	Revision Revision `json:"revision"`
	Reviews  []Review `json:"reviews"`
}

// ValidateChangesRequest 校验要求修改命令必须带有可执行反馈。
func (input ReviewInput) ValidateChangesRequest() error {
	if strings.TrimSpace(input.Comment) == "" {
		return errors.New("要求修改时必须填写审核意见")
	}
	return nil
}

// Validate 校验一份完整、可审核的 Plan Revision。
func (input RevisionInput) Validate() error {
	input = input.Normalized()
	if input.Summary == "" || utf8.RuneCountInString(input.Summary) > 20000 {
		return errors.New("summary 不能为空且最长 20000 个字符")
	}
	if len(input.Drafts) < 1 || len(input.Drafts) > 50 {
		return errors.New("Plan 必须包含 1 到 50 个 Task 草案")
	}
	for _, draft := range input.Drafts {
		if err := draft.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Normalized 清理 Revision 文本字段。
func (input RevisionInput) Normalized() RevisionInput {
	input.Summary = strings.TrimSpace(input.Summary)
	input.Rationale = strings.TrimSpace(input.Rationale)
	input.Risks = strings.TrimSpace(input.Risks)
	input.SourceRunID = strings.TrimSpace(input.SourceRunID)
	for index := range input.Drafts {
		input.Drafts[index] = input.Drafts[index].Normalized()
	}
	return input
}

// Validate 校验 Task 草案。
func (input TaskDraftInput) Validate() error {
	input = input.Normalized()
	if utf8.RuneCountInString(input.Title) < 1 || utf8.RuneCountInString(input.Title) > 200 {
		return errors.New("Task 草案标题长度必须为 1 到 200")
	}
	if input.Priority < 0 || input.Priority > 3 {
		return errors.New("Task 草案优先级必须为 P0 到 P3")
	}
	if len(input.TestCases) > 50 {
		return errors.New("每个 Task 草案最多包含 50 个测试项")
	}
	for _, testCase := range input.TestCases {
		if strings.TrimSpace(testCase.Title) == "" {
			return errors.New("测试项标题不能为空")
		}
	}
	return nil
}

// Normalized 清理 Task 草案字段。
func (input TaskDraftInput) Normalized() TaskDraftInput {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.AcceptanceCriteria = strings.TrimSpace(input.AcceptanceCriteria)
	for index := range input.TestCases {
		input.TestCases[index].Title = strings.TrimSpace(input.TestCases[index].Title)
		input.TestCases[index].Description = strings.TrimSpace(input.TestCases[index].Description)
	}
	return input
}
