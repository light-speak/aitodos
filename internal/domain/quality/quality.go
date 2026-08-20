// Package quality 定义 Task 估算、测试项和可审计测试结果。
package quality

import (
	"errors"
	"strings"
	"time"
)

// EstimateSource 标识估算由谁产生。
type EstimateSource string

const (
	EstimateAI    EstimateSource = "AI"
	EstimateHuman EstimateSource = "HUMAN"
)

// Estimate 是创建后不可修改的 Task 估算修订。
type Estimate struct {
	ID              string         `json:"id"`
	TaskID          string         `json:"task_id"`
	Revision        int64          `json:"revision"`
	Points          int            `json:"points"`
	RemainingPoints int            `json:"remaining_points"`
	Confidence      float64        `json:"confidence"`
	Rationale       string         `json:"rationale"`
	Source          EstimateSource `json:"source"`
	SourceRunID     string         `json:"source_run_id,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
}

// EstimateInput 创建一个新的估算修订。
type EstimateInput struct {
	Points          int            `json:"points"`
	RemainingPoints int            `json:"remaining_points"`
	Confidence      float64        `json:"confidence"`
	Rationale       string         `json:"rationale"`
	Source          EstimateSource `json:"source"`
	SourceRunID     string         `json:"source_run_id,omitempty"`
}

// Validate 校验点数、置信度和来源证据。
func (input EstimateInput) Validate() error {
	if !validPoints(input.Points) {
		return errors.New("points 必须为 1、2、3、5、8 或 13")
	}
	if input.RemainingPoints < 0 || input.RemainingPoints > input.Points {
		return errors.New("remaining_points 必须为 0 到 points")
	}
	if input.Confidence < 0 || input.Confidence > 1 {
		return errors.New("confidence 必须为 0 到 1")
	}
	if len(strings.TrimSpace(input.Rationale)) < 1 || len(strings.TrimSpace(input.Rationale)) > 4000 {
		return errors.New("rationale 长度必须为 1 到 4000")
	}
	if input.Source != EstimateAI && input.Source != EstimateHuman {
		return errors.New("未知 estimate source")
	}
	if input.Source == EstimateAI && strings.TrimSpace(input.SourceRunID) == "" {
		return errors.New("AI estimate 必须关联 source_run_id")
	}
	return nil
}

func validPoints(points int) bool {
	for _, allowed := range []int{1, 2, 3, 5, 8, 13} {
		if points == allowed {
			return true
		}
	}
	return false
}

// TestCreator 标识测试项的创建来源。
type TestCreator string

const (
	TestCreatorHuman TestCreator = "HUMAN"
	TestCreatorAgent TestCreator = "AGENT"
)

// TestOutcome 是一次测试执行的结论。
type TestOutcome string

const (
	OutcomePassed  TestOutcome = "PASSED"
	OutcomeFailed  TestOutcome = "FAILED"
	OutcomeBlocked TestOutcome = "BLOCKED"
)

// EvidenceKind 表示测试结论的证据强度和来源。
type EvidenceKind string

const (
	EvidenceCommand     EvidenceKind = "COMMAND"
	EvidenceHuman       EvidenceKind = "HUMAN"
	EvidenceAgentReport EvidenceKind = "AGENT_REPORT"
)

// TestCase 是 Task 的一个长期测试或检查项。
type TestCase struct {
	ID           string      `json:"id"`
	TaskID       string      `json:"task_id"`
	Title        string      `json:"title"`
	Description  string      `json:"description"`
	Required     bool        `json:"required"`
	SortOrder    int         `json:"sort_order"`
	CreatedBy    TestCreator `json:"created_by"`
	SourceRunID  string      `json:"source_run_id,omitempty"`
	LatestResult *TestResult `json:"latest_result,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

// TestCaseInput 创建一个测试项。
type TestCaseInput struct {
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Required    bool        `json:"required"`
	SortOrder   int         `json:"sort_order"`
	CreatedBy   TestCreator `json:"created_by"`
	SourceRunID string      `json:"source_run_id,omitempty"`
}

// Validate 校验测试项。
func (input TestCaseInput) Validate() error {
	if len(strings.TrimSpace(input.Title)) < 1 || len(strings.TrimSpace(input.Title)) > 300 {
		return errors.New("test title 长度必须为 1 到 300")
	}
	if len(input.Description) > 4000 || input.SortOrder < 0 {
		return errors.New("test description 或 sort_order 无效")
	}
	if input.CreatedBy != TestCreatorHuman && input.CreatedBy != TestCreatorAgent {
		return errors.New("未知 test creator")
	}
	if input.CreatedBy == TestCreatorAgent && strings.TrimSpace(input.SourceRunID) == "" {
		return errors.New("Agent 创建的测试项必须关联 source_run_id")
	}
	return nil
}

// TestResult 是追加式保存的一次测试结论。
type TestResult struct {
	ID           string       `json:"id"`
	TestCaseID   string       `json:"test_case_id"`
	TaskID       string       `json:"task_id"`
	Outcome      TestOutcome  `json:"outcome"`
	EvidenceKind EvidenceKind `json:"evidence_kind"`
	Summary      string       `json:"summary"`
	Command      string       `json:"command,omitempty"`
	ArtifactRef  string       `json:"artifact_ref,omitempty"`
	SourceRunID  string       `json:"source_run_id,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
}

// TestResultInput 追加一个测试结论。
type TestResultInput struct {
	Outcome      TestOutcome  `json:"outcome"`
	EvidenceKind EvidenceKind `json:"evidence_kind"`
	Summary      string       `json:"summary"`
	Command      string       `json:"command,omitempty"`
	ArtifactRef  string       `json:"artifact_ref,omitempty"`
	SourceRunID  string       `json:"source_run_id,omitempty"`
}

// Validate 校验结论和证据。
func (input TestResultInput) Validate() error {
	if input.Outcome != OutcomePassed && input.Outcome != OutcomeFailed && input.Outcome != OutcomeBlocked {
		return errors.New("未知 test outcome")
	}
	if input.EvidenceKind != EvidenceCommand && input.EvidenceKind != EvidenceHuman && input.EvidenceKind != EvidenceAgentReport {
		return errors.New("未知 evidence kind")
	}
	if len(strings.TrimSpace(input.Summary)) < 1 || len(strings.TrimSpace(input.Summary)) > 4000 {
		return errors.New("summary 长度必须为 1 到 4000")
	}
	if input.EvidenceKind == EvidenceCommand && strings.TrimSpace(input.Command) == "" {
		return errors.New("COMMAND evidence 必须记录 command")
	}
	if input.EvidenceKind == EvidenceAgentReport && strings.TrimSpace(input.SourceRunID) == "" {
		return errors.New("AGENT_REPORT evidence 必须关联 source_run_id")
	}
	return nil
}

// TaskQuality 汇总 Task 当前估算和测试证据。
type TaskQuality struct {
	Estimate        *Estimate  `json:"estimate,omitempty"`
	EstimateHistory []Estimate `json:"estimate_history"`
	TestCases       []TestCase `json:"test_cases"`
}

// ProjectProgress 是可解释的项目进度投影。
type ProjectProgress struct {
	TotalTasks               int      `json:"total_tasks"`
	AcceptedTasks            int      `json:"accepted_tasks"`
	StrictPercent            float64  `json:"strict_percent"`
	EstimatedTasks           int      `json:"estimated_tasks"`
	EstimateCoverage         float64  `json:"estimate_coverage"`
	TotalPoints              int      `json:"total_points"`
	RemainingPoints          int      `json:"remaining_points"`
	ForecastPercent          *float64 `json:"forecast_percent"`
	RequiredTests            int      `json:"required_tests"`
	VerifiedPassedTests      int      `json:"verified_passed_tests"`
	AgentReportedPassedTests int      `json:"agent_reported_passed_tests"`
}
