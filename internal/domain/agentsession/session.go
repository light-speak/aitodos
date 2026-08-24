// Package agentsession 定义可替换的外部 Agent 会话身份。
package agentsession

import "time"

// Status 表示外部会话能否继续 Resume。
type Status string

const (
	StatusActive  Status = "ACTIVE"
	StatusInvalid Status = "INVALID"
)

// Session 只保存恢复身份和兼容性信息，不承载业务事实。
type Session struct {
	ID                string    `json:"id"`
	TopicID           string    `json:"topic_id,omitempty"`
	TaskID            string    `json:"task_id,omitempty"`
	ProfileRevisionID string    `json:"profile_revision_id"`
	Model             string    `json:"model,omitempty"`
	Adapter           string    `json:"adapter"`
	ExternalSessionID string    `json:"external_session_id"`
	Status            Status    `json:"status"`
	LastRunID         string    `json:"last_run_id"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
