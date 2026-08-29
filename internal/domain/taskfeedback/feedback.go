// Package taskfeedback 定义 Task 人类反馈与 Agent 处理状态。
package taskfeedback

import (
	"errors"
	"time"
)

// Intent 表示人类明确授权的反馈用途。
type Intent string

const (
	IntentDiscuss        Intent = "DISCUSS"
	IntentRequestChanges Intent = "REQUEST_CHANGES"
)

// Status 表示反馈从排队到处理完成的持久状态。
type Status string

const (
	StatusQueued   Status = "QUEUED"
	StatusRunning  Status = "RUNNING"
	StatusAnswered Status = "ANSWERED"
	StatusApplied  Status = "APPLIED"
	StatusFailed   Status = "FAILED"
)

// Feedback 是一条与来源消息、Run 和回复关联的反馈事实。
type Feedback struct {
	ID                string    `json:"id"`
	TaskID            string    `json:"task_id"`
	SourceMessageID   string    `json:"source_message_id"`
	RetryOfFeedbackID string    `json:"retry_of_feedback_id,omitempty"`
	Intent            Intent    `json:"intent"`
	Status            Status    `json:"status"`
	RunID             string    `json:"run_id,omitempty"`
	ResponseMessageID string    `json:"response_message_id,omitempty"`
	FailureMessage    string    `json:"failure_message,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// Event 是 Task 内按 sequence 递增的反馈状态事实。
type Event struct {
	ID                string    `json:"id"`
	TaskID            string    `json:"task_id"`
	FeedbackID        string    `json:"feedback_id"`
	Sequence          int64     `json:"sequence"`
	Status            Status    `json:"status"`
	RunID             string    `json:"run_id,omitempty"`
	ResponseMessageID string    `json:"response_message_id,omitempty"`
	FailureMessage    string    `json:"failure_message,omitempty"`
	OccurredAt        time.Time `json:"occurred_at"`
}

// CreateInput 是创建反馈时允许指定的意图。
type CreateInput struct {
	Intent Intent `json:"intent"`
}

// Validate 拒绝隐式或未知反馈意图。
func (input CreateInput) Validate() error {
	if input.Intent != IntentDiscuss && input.Intent != IntentRequestChanges {
		return errors.New("unknown task feedback intent")
	}
	return nil
}

// Terminal 判断反馈是否已经结束处理。
func (status Status) Terminal() bool {
	return status == StatusAnswered || status == StatusApplied || status == StatusFailed
}
