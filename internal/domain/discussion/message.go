// Package discussion 定义 Topic 与 Task 共用的持久讨论模型。
package discussion

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxMessageLength = 20000
	maxLinkedTasks   = 20
)

// AuthorKind 表示消息作者的来源类型。
type AuthorKind string

const (
	// AuthorHuman 表示由当前用户发布的消息。
	AuthorHuman AuthorKind = "HUMAN"
	// AuthorAgent 表示由 Agent 产生的消息。
	AuthorAgent AuthorKind = "AGENT"
	// AuthorSystem 表示由系统产生的消息。
	AuthorSystem AuthorKind = "SYSTEM"
)

// Message 是讨论线程内不可静默改写的消息。
type Message struct {
	ID            string     `json:"id"`
	ThreadID      string     `json:"thread_id"`
	Sequence      int64      `json:"sequence"`
	AuthorKind    AuthorKind `json:"author_kind"`
	Content       string     `json:"content"`
	LinkedTaskIDs []string   `json:"linked_task_ids"`
	CreatedAt     time.Time  `json:"created_at"`
}

// CreateMessageInput 是用户发布消息时允许提供的字段。
type CreateMessageInput struct {
	Content       string   `json:"content"`
	LinkedTaskIDs []string `json:"linked_task_ids"`
}

// Validate 校验消息内容。
func (input CreateMessageInput) Validate() error {
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return errors.New("message content is required")
	}
	if utf8.RuneCountInString(content) > maxMessageLength {
		return errors.New("message content must not exceed 20000 characters")
	}
	if len(input.LinkedTaskIDs) > maxLinkedTasks {
		return errors.New("message must not link more than 20 tasks")
	}
	return nil
}

// Normalized 返回清除内容首尾空白后的参数。
func (input CreateMessageInput) Normalized() CreateMessageInput {
	input.Content = strings.TrimSpace(input.Content)
	input.LinkedTaskIDs = normalizeTaskIDs(input.LinkedTaskIDs)
	return input
}

func normalizeTaskIDs(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
