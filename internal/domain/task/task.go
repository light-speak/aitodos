// Package task 定义 Task 聚合及其状态机，不依赖传输层和持久化实现。
package task

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxTitleLength         = 200
	provisionalTitleLength = 60
)

// TitleSource 标识 Task 标题由临时规则、AI 或人工产生。
type TitleSource string

const (
	TitleSourceProvisional TitleSource = "PROVISIONAL"
	TitleSourceAI          TitleSource = "AI"
	TitleSourceHuman       TitleSource = "HUMAN"
)

// Task 表示一个由人创建、等待 Agent 执行并验收的目标。
type Task struct {
	ID                     string      `json:"id"`
	Key                    string      `json:"key"`
	Title                  string      `json:"title"`
	TitleSource            TitleSource `json:"title_source"`
	TitleLocked            bool        `json:"title_locked"`
	Description            string      `json:"description"`
	AcceptanceCriteria     string      `json:"acceptance_criteria"`
	Status                 Status      `json:"status"`
	Priority               int         `json:"priority"`
	TargetBranch           string      `json:"target_branch,omitempty"`
	BaseCommitSHA          string      `json:"base_commit_sha,omitempty"`
	CurrentWorkspaceID     string      `json:"current_workspace_id,omitempty"`
	LatestRunID            string      `json:"latest_run_id,omitempty"`
	SourcePlanRevisionID   string      `json:"source_plan_revision_id,omitempty"`
	SourcePlanTaskDraftID  string      `json:"source_plan_task_draft_id,omitempty"`
	AssessmentInputVersion int64       `json:"assessment_input_version"`
	Version                int64       `json:"version"`
	CreatedAt              time.Time   `json:"created_at"`
	UpdatedAt              time.Time   `json:"updated_at"`
	ArchivedAt             *time.Time  `json:"archived_at,omitempty"`
}

// CreateInput 是创建 Task 时允许由用户提供的字段。
type CreateInput struct {
	Title                 string      `json:"title"`
	Description           string      `json:"description"`
	AcceptanceCriteria    string      `json:"acceptance_criteria"`
	Priority              int         `json:"priority"`
	TargetBranch          string      `json:"target_branch,omitempty"`
	TitleSource           TitleSource `json:"-"`
	SourcePlanRevisionID  string      `json:"-"`
	SourcePlanTaskDraftID string      `json:"-"`
}

// UpdateTitleInput 是人工锁定标题的领域输入。
type UpdateTitleInput struct {
	Title string `json:"title"`
}

// UpdateDetailsInput 是人工修订 Task 执行输入的领域命令。
type UpdateDetailsInput struct {
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptance_criteria"`
	Priority           int    `json:"priority"`
}

// Normalized 清理 Task 执行输入。
func (input UpdateDetailsInput) Normalized() UpdateDetailsInput {
	input.Description = strings.TrimSpace(input.Description)
	input.AcceptanceCriteria = strings.TrimSpace(input.AcceptanceCriteria)
	return input
}

// Validate 校验 Task 执行输入规模。
func (input UpdateDetailsInput) Validate() error {
	input = input.Normalized()
	if utf8.RuneCountInString(input.Description) > 20000 || utf8.RuneCountInString(input.AcceptanceCriteria) > 20000 {
		return errors.New("description and acceptance criteria must not exceed 20000 characters")
	}
	if input.Priority < 0 || input.Priority > 3 {
		return errors.New("priority must be between P0 and P3")
	}
	return nil
}

// UpdateTargetBranchInput 是 Workspace 创建前允许修改的目标分支。
type UpdateTargetBranchInput struct {
	TargetBranch string `json:"target_branch"`
}

// Normalized 清理目标分支名称。
func (input UpdateTargetBranchInput) Normalized() UpdateTargetBranchInput {
	input.TargetBranch = strings.TrimSpace(input.TargetBranch)
	return input
}

// Validate 校验目标分支的领域层长度约束；Git ref 规则由 Git Workflow 校验。
func (input UpdateTargetBranchInput) Validate() error {
	input = input.Normalized()
	if utf8.RuneCountInString(input.TargetBranch) < 1 || utf8.RuneCountInString(input.TargetBranch) > 255 {
		return errors.New("target branch 长度必须为 1 到 255")
	}
	return nil
}

// Normalized 清理人工标题。
func (input UpdateTitleInput) Normalized() UpdateTitleInput {
	input.Title = strings.TrimSpace(input.Title)
	return input
}

// Validate 校验人工标题。
func (input UpdateTitleInput) Validate() error {
	input = input.Normalized()
	if utf8.RuneCountInString(input.Title) < 1 || utf8.RuneCountInString(input.Title) > maxTitleLength {
		return errors.New("title 长度必须为 1 到 200")
	}
	return nil
}

// Validate 校验创建 Task 所需的不变量。
func (input CreateInput) Validate() error {
	normalized := input.Normalized()
	if normalized.Title == "" {
		return errors.New("title or description is required")
	}
	if utf8.RuneCountInString(normalized.Title) > maxTitleLength {
		return errors.New("title must not exceed 200 characters")
	}
	if normalized.Priority < 0 || normalized.Priority > 3 {
		return errors.New("priority must be between P0 and P3")
	}
	if normalized.TitleSource != TitleSourceProvisional && normalized.TitleSource != TitleSourceAI && normalized.TitleSource != TitleSourceHuman {
		return errors.New("title source must be PROVISIONAL, AI, or HUMAN")
	}
	return nil
}

// Normalized 返回清除字段首尾空白后的创建参数。
func (input CreateInput) Normalized() CreateInput {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.AcceptanceCriteria = strings.TrimSpace(input.AcceptanceCriteria)
	input.TargetBranch = strings.TrimSpace(input.TargetBranch)
	input.SourcePlanRevisionID = strings.TrimSpace(input.SourcePlanRevisionID)
	input.SourcePlanTaskDraftID = strings.TrimSpace(input.SourcePlanTaskDraftID)
	if input.TitleSource == "" && input.Title == "" {
		input.TitleSource = TitleSourceProvisional
	}
	if input.TitleSource == "" {
		input.TitleSource = TitleSourceHuman
	}
	if input.Title == "" {
		input.Title = provisionalTitle(input.Description)
	}
	return input
}

func provisionalTitle(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) <= provisionalTitleLength {
			return line
		}
		return string(runes[:provisionalTitleLength-1]) + "…"
	}
	return ""
}

// EventType 标识 Task 审计事件的类型。
type EventType string

const (
	// EventCreated 表示 Task 已创建。
	EventCreated EventType = "TASK_CREATED"
	// EventStatusChanged 表示 Task 状态已通过领域命令迁移。
	EventStatusChanged EventType = "TASK_STATUS_CHANGED"
	// EventTitleChanged 表示标题被 AI 应用或由人工锁定。
	EventTitleChanged EventType = "TASK_TITLE_CHANGED"
	// EventTargetBranchChanged 表示 Workspace 创建前更换了目标分支。
	EventTargetBranchChanged EventType = "TASK_TARGET_BRANCH_CHANGED"
	// EventDetailsChanged 表示人工修订了描述、验收标准或优先级。
	EventDetailsChanged EventType = "TASK_DETAILS_CHANGED"
	// EventArchived 表示终态 Task 已从默认工作视图归档。
	EventArchived EventType = "TASK_ARCHIVED"
)

// Event 是 Task 聚合内按序追加的审计记录。
type Event struct {
	ID         string          `json:"id"`
	TaskID     string          `json:"task_id"`
	Sequence   int64           `json:"sequence"`
	Type       EventType       `json:"type"`
	Payload    json.RawMessage `json:"payload"`
	OccurredAt time.Time       `json:"occurred_at"`
}
