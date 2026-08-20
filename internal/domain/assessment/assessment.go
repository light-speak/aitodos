// Package assessment 定义 Task 的不可变复杂度评估及后端推导算法。
package assessment

import (
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// ComplexityLevel 是后端从六维评分推导出的复杂度等级。
type ComplexityLevel string

const (
	ComplexityC1 ComplexityLevel = "C1"
	ComplexityC2 ComplexityLevel = "C2"
	ComplexityC3 ComplexityLevel = "C3"
	ComplexityC4 ComplexityLevel = "C4"
	ComplexityC5 ComplexityLevel = "C5"
)

// AutonomyLevel 表示 AI 独立完成 Task 的适配程度，不代表权限。
type AutonomyLevel string

const (
	AutonomyA0 AutonomyLevel = "A0"
	AutonomyA1 AutonomyLevel = "A1"
	AutonomyA2 AutonomyLevel = "A2"
	AutonomyA3 AutonomyLevel = "A3"
)

// DimensionScores 是 Agent 允许提交的六个原始维度，每项范围为 0..4。
type DimensionScores struct {
	TechnicalComplexity    int `json:"technical_complexity"`
	RequirementUncertainty int `json:"requirement_uncertainty"`
	ChangeScope            int `json:"change_scope"`
	ValidationBurden       int `json:"validation_burden"`
	HumanDependency        int `json:"human_dependency"`
	RiskAndReversibility   int `json:"risk_and_reversibility"`
}

// Calculation 是可信后端根据固定算法得到的派生值。
type Calculation struct {
	WeightedScore float64
	Complexity    ComplexityLevel
	Autonomy      AutonomyLevel
}

// Input 是 Triage Agent 的不可信结构化输出。
type Input struct {
	SuggestedTitle   string          `json:"suggested_title"`
	Scores           DimensionScores `json:"scores"`
	Confidence       float64         `json:"confidence"`
	Rationale        string          `json:"rationale"`
	Assumptions      []string        `json:"assumptions"`
	SplitRecommended bool            `json:"split_recommended"`
	SplitRationale   string          `json:"split_rationale"`
}

// Normalized 清理标题、依据和假设文本。
func (input Input) Normalized() Input {
	input.SuggestedTitle = strings.TrimSpace(input.SuggestedTitle)
	input.Rationale = strings.TrimSpace(input.Rationale)
	input.SplitRationale = strings.TrimSpace(input.SplitRationale)
	assumptions := make([]string, 0, len(input.Assumptions))
	for _, assumption := range input.Assumptions {
		if cleaned := strings.TrimSpace(assumption); cleaned != "" {
			assumptions = append(assumptions, cleaned)
		}
	}
	input.Assumptions = assumptions
	return input
}

// Validate 校验 Agent 输出，不接受越界评分或无依据结论。
func (input Input) Validate() error {
	input = input.Normalized()
	if utf8.RuneCountInString(input.SuggestedTitle) < 1 || utf8.RuneCountInString(input.SuggestedTitle) > 200 {
		return errors.New("suggested_title 长度必须为 1 到 200")
	}
	if err := input.Scores.Validate(); err != nil {
		return err
	}
	if input.Confidence < 0 || input.Confidence > 1 {
		return errors.New("confidence 必须为 0 到 1")
	}
	if len(input.Rationale) < 1 || len(input.Rationale) > 4000 {
		return errors.New("rationale 长度必须为 1 到 4000")
	}
	if len(input.Assumptions) > 20 {
		return errors.New("assumptions 最多包含 20 项")
	}
	for _, assumption := range input.Assumptions {
		if len(assumption) > 1000 {
			return errors.New("每项 assumption 最长 1000 个字符")
		}
	}
	if input.SplitRecommended && input.SplitRationale == "" {
		return errors.New("建议拆分时必须提供 split_rationale")
	}
	if len(input.SplitRationale) > 4000 {
		return errors.New("split_rationale 最长 4000 个字符")
	}
	return nil
}

// Validate 校验六维评分范围。
func (scores DimensionScores) Validate() error {
	values := []int{
		scores.TechnicalComplexity, scores.RequirementUncertainty, scores.ChangeScope,
		scores.ValidationBurden, scores.HumanDependency, scores.RiskAndReversibility,
	}
	for _, value := range values {
		if value < 0 || value > 4 {
			return errors.New("complexity dimension score 必须为 0 到 4")
		}
	}
	return nil
}

// Calculate 使用 ADR-0010 的固定权重推导等级和自主度。
func Calculate(scores DimensionScores) (Calculation, error) {
	if err := scores.Validate(); err != nil {
		return Calculation{}, err
	}
	weighted := float64(scores.TechnicalComplexity)*0.25 +
		float64(scores.RequirementUncertainty)*0.20 +
		float64(scores.ChangeScope)*0.15 +
		float64(scores.ValidationBurden)*0.15 +
		float64(scores.HumanDependency)*0.15 +
		float64(scores.RiskAndReversibility)*0.10
	weighted = math.Round(weighted*100) / 100
	return Calculation{
		WeightedScore: weighted,
		Complexity:    complexityLevel(weighted),
		Autonomy:      autonomyLevel(scores),
	}, nil
}

func complexityLevel(score float64) ComplexityLevel {
	switch {
	case score < 0.8:
		return ComplexityC1
	case score < 1.6:
		return ComplexityC2
	case score < 2.4:
		return ComplexityC3
	case score < 3.2:
		return ComplexityC4
	default:
		return ComplexityC5
	}
}

func autonomyLevel(scores DimensionScores) AutonomyLevel {
	if scores.HumanDependency == 0 && scores.ValidationBurden <= 1 && scores.RequirementUncertainty <= 1 {
		return AutonomyA3
	}
	if scores.HumanDependency <= 1 {
		return AutonomyA2
	}
	if scores.HumanDependency <= 3 {
		return AutonomyA1
	}
	return AutonomyA0
}

// Assessment 是创建后不可修改的一次 Task 评估。
type Assessment struct {
	ID                    string          `json:"id"`
	TaskID                string          `json:"task_id"`
	TaskAssessmentVersion int64           `json:"task_assessment_version"`
	Revision              int64           `json:"revision"`
	SuggestedTitle        string          `json:"suggested_title"`
	AppliedTitle          string          `json:"applied_title"`
	Scores                DimensionScores `json:"scores"`
	WeightedScore         float64         `json:"weighted_score"`
	Complexity            ComplexityLevel `json:"complexity"`
	Autonomy              AutonomyLevel   `json:"autonomy"`
	Confidence            float64         `json:"confidence"`
	Rationale             string          `json:"rationale"`
	Assumptions           []string        `json:"assumptions"`
	SplitRecommended      bool            `json:"split_recommended"`
	SplitRationale        string          `json:"split_rationale"`
	SourceRunID           string          `json:"source_run_id"`
	CreatedAt             time.Time       `json:"created_at"`
}
