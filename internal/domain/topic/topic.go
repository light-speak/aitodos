// Package topic 定义长期讨论与需求规划的 Topic 聚合。
package topic

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

// Topic 表示一个可持续讨论和形成 Plan 的议题。
type Topic struct {
	ID               string    `json:"id"`
	Key              string    `json:"key"`
	Title            string    `json:"title"`
	Description      string    `json:"description"`
	Status           Status    `json:"status"`
	CurrentPlanID    string    `json:"current_plan_id,omitempty"`
	CurrentSummaryID string    `json:"current_summary_id,omitempty"`
	Version          int64     `json:"version"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// CreateInput 是创建 Topic 时允许由用户提供的字段。
type CreateInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// Validate 校验创建 Topic 所需的不变量。
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

// EventType 标识 Topic 审计事件的类型。
type EventType string

const (
	EventCreated       EventType = "TOPIC_CREATED"
	EventStatusChanged EventType = "TOPIC_STATUS_CHANGED"
	EventMessageAdded  EventType = "TOPIC_MESSAGE_ADDED"
	EventPlanningAsked EventType = "TOPIC_PLANNING_REQUESTED"
)

// Event 是 Topic 聚合内按序追加的审计记录。
type Event struct {
	ID         string          `json:"id"`
	TopicID    string          `json:"topic_id"`
	Sequence   int64           `json:"sequence"`
	Type       EventType       `json:"type"`
	Payload    json.RawMessage `json:"payload"`
	OccurredAt time.Time       `json:"occurred_at"`
}
