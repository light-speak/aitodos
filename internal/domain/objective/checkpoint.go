package objective

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// CriterionStatus 表示完成条件在某个检查点的验证状态。
type CriterionStatus string

const (
	CriterionSatisfied   CriterionStatus = "SATISFIED"
	CriterionUnsatisfied CriterionStatus = "UNSATISFIED"
	CriterionUnknown     CriterionStatus = "UNKNOWN"
)

// CriterionResult 保存完成条件状态及可复查证据。
type CriterionResult struct {
	CriterionID string          `json:"criterion_id"`
	Status      CriterionStatus `json:"status"`
	Evidence    string          `json:"evidence"`
}

// StopReason 表示目标在检查点停止继续的原因。
type StopReason string

const (
	StopProgress        StopReason = "PROGRESS"
	StopNeedsInput      StopReason = "NEEDS_INPUT"
	StopReviewRequired  StopReason = "REVIEW_REQUIRED"
	StopLimitReached    StopReason = "LIMIT_REACHED"
	StopReadyToComplete StopReason = "READY_TO_COMPLETE"
	StopNoProgress      StopReason = "NO_PROGRESS"
)

// Checkpoint 是可重建但不可变的阶段摘要。
type Checkpoint struct {
	ID          string            `json:"id"`
	ObjectiveID string            `json:"objective_id"`
	Sequence    int               `json:"sequence"`
	SourceRunID string            `json:"source_run_id,omitempty"`
	Summary     string            `json:"summary"`
	Criteria    []CriterionResult `json:"criteria"`
	Completed   []string          `json:"completed"`
	Remaining   []string          `json:"remaining"`
	Risks       []string          `json:"risks"`
	StopReason  StopReason        `json:"stop_reason"`
	NextAction  string            `json:"next_action"`
	CreatedAt   time.Time         `json:"created_at"`
}

// CheckpointInput 是追加检查点所需的事实。
type CheckpointInput struct {
	SourceRunID string            `json:"source_run_id"`
	Summary     string            `json:"summary"`
	Criteria    []CriterionResult `json:"criteria"`
	Completed   []string          `json:"completed"`
	Remaining   []string          `json:"remaining"`
	Risks       []string          `json:"risks"`
	StopReason  StopReason        `json:"stop_reason"`
	NextAction  string            `json:"next_action"`
}

// Normalized 清理检查点文本。
func (input CheckpointInput) Normalized() CheckpointInput {
	input.SourceRunID = strings.TrimSpace(input.SourceRunID)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Completed = normalizeUnique(input.Completed)
	input.Remaining = normalizeUnique(input.Remaining)
	input.Risks = normalizeUnique(input.Risks)
	input.NextAction = strings.TrimSpace(input.NextAction)
	for index := range input.Criteria {
		input.Criteria[index].CriterionID = strings.TrimSpace(input.Criteria[index].CriterionID)
		input.Criteria[index].Evidence = strings.TrimSpace(input.Criteria[index].Evidence)
	}
	return input
}

// Validate 校验检查点及其完成条件引用。
func (input CheckpointInput) Validate(knownCriteria map[string]struct{}) error {
	input = input.Normalized()
	if input.Summary == "" || utf8.RuneCountInString(input.Summary) > 4000 {
		return errors.New("检查点摘要不能为空且不能超过 4000 字符")
	}
	if !input.StopReason.Valid() || utf8.RuneCountInString(input.NextAction) > 2000 {
		return errors.New("停止原因或下一步无效")
	}
	if len(input.Completed) > 50 || len(input.Remaining) > 50 || len(input.Risks) > 50 {
		return errors.New("检查点列表每类最多 50 项")
	}
	if err := validateTextItems(input.Completed, input.Remaining, input.Risks); err != nil {
		return err
	}
	return validateCriterionResults(input.Criteria, knownCriteria)
}

// Valid 判断停止原因是否受支持。
func (reason StopReason) Valid() bool {
	switch reason {
	case StopProgress, StopNeedsInput, StopReviewRequired, StopLimitReached, StopReadyToComplete, StopNoProgress:
		return true
	default:
		return false
	}
}

func validateCriterionResults(results []CriterionResult, known map[string]struct{}) error {
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		if _, exists := known[result.CriterionID]; !exists {
			return errors.New("检查点引用了未知完成条件")
		}
		if _, exists := seen[result.CriterionID]; exists {
			return errors.New("检查点重复引用完成条件")
		}
		seen[result.CriterionID] = struct{}{}
		if result.Status != CriterionSatisfied && result.Status != CriterionUnsatisfied && result.Status != CriterionUnknown {
			return errors.New("完成条件状态无效")
		}
		if result.Status == CriterionSatisfied && strings.TrimSpace(result.Evidence) == "" {
			return errors.New("已满足条件必须提供证据")
		}
		if utf8.RuneCountInString(result.Evidence) > 2000 {
			return errors.New("完成条件证据不能超过 2000 字符")
		}
	}
	return nil
}

// AllCriteriaSatisfied 判断最新检查点是否覆盖并满足全部完成条件。
func AllCriteriaSatisfied(criteria []Criterion, results []CriterionResult) bool {
	states := make(map[string]CriterionResult, len(results))
	for _, result := range results {
		states[result.CriterionID] = result
	}
	for _, criterion := range criteria {
		result, exists := states[criterion.ID]
		if !exists || result.Status != CriterionSatisfied || strings.TrimSpace(result.Evidence) == "" {
			return false
		}
	}
	return len(criteria) > 0
}
