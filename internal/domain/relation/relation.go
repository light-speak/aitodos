// Package relation 定义 Topic、Task 和讨论消息之间的显式关联。
package relation

import (
	"errors"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/topic"
)

// TaskAssociation 是当前主体关联的 Task 及其可追溯来源。
type TaskAssociation struct {
	Task            task.Task `json:"task"`
	SourceMessageID string    `json:"source_message_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

// TopicAssociation 是当前 Task 关联的 Topic 及其可追溯来源。
type TopicAssociation struct {
	Topic           topic.Topic `json:"topic"`
	SourceMessageID string      `json:"source_message_id,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
}

// LinkTaskInput 是创建 Task 关联时允许提供的字段。
type LinkTaskInput struct {
	TaskID string `json:"task_id"`
}

// LinkTopicInput 是从 Task 创建 Topic 关联时允许提供的字段。
type LinkTopicInput struct {
	TopicID string `json:"topic_id"`
}

// Validate 校验 Topic 关联目标。
func (input LinkTopicInput) Validate() error {
	if strings.TrimSpace(input.TopicID) == "" {
		return errors.New("topic id is required")
	}
	return nil
}

// Normalized 返回清除首尾空白后的 Topic 关联参数。
func (input LinkTopicInput) Normalized() LinkTopicInput {
	input.TopicID = strings.TrimSpace(input.TopicID)
	return input
}

// Validate 校验关联目标。
func (input LinkTaskInput) Validate() error {
	if strings.TrimSpace(input.TaskID) == "" {
		return errors.New("task id is required")
	}
	return nil
}

// Normalized 返回清除首尾空白后的关联参数。
func (input LinkTaskInput) Normalized() LinkTaskInput {
	input.TaskID = strings.TrimSpace(input.TaskID)
	return input
}
