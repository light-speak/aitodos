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

// Task 表示一个由人创建、等待 Agent 执行并验收的目标。
type Task struct {
	ID                 string    `json:"id"`
	Key                string    `json:"key"`
	Title              string    `json:"title"`
	Description        string    `json:"description"`
	AcceptanceCriteria string    `json:"acceptance_criteria"`
	Status             Status    `json:"status"`
	Priority           int       `json:"priority"`
	TargetBranch       string    `json:"target_branch,omitempty"`
	BaseCommitSHA      string    `json:"base_commit_sha,omitempty"`
	CurrentWorkspaceID string    `json:"current_workspace_id,omitempty"`
	LatestRunID        string    `json:"latest_run_id,omitempty"`
	Version            int64     `json:"version"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// CreateInput 是创建 Task 时允许由用户提供的字段。
type CreateInput struct {
	Title              string `json:"title"`
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptance_criteria"`
	Priority           int    `json:"priority"`
	TargetBranch       string `json:"target_branch,omitempty"`
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
	return nil
}

// Normalized 返回清除字段首尾空白后的创建参数。
func (input CreateInput) Normalized() CreateInput {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.AcceptanceCriteria = strings.TrimSpace(input.AcceptanceCriteria)
	input.TargetBranch = strings.TrimSpace(input.TargetBranch)
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
