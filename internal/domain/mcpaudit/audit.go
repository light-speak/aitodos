// Package mcpaudit 定义项目 MCP 调用的脱敏审计事实。
package mcpaudit

import "time"

// Phase 表示一次工具调用的生命周期阶段。
type Phase string

const (
	PhaseStarted   Phase = "STARTED"
	PhaseCompleted Phase = "COMPLETED"
	PhaseFailed    Phase = "FAILED"
)

// Event 是不包含参数原文和返回内容的 MCP 调用审计记录。
type Event struct {
	ID              string    `json:"id"`
	CallID          string    `json:"call_id"`
	Direction       string    `json:"direction"`
	RunID           string    `json:"run_id,omitempty"`
	ClientName      string    `json:"client_name"`
	ServerName      string    `json:"server_name,omitempty"`
	ToolName        string    `json:"tool_name"`
	Phase           Phase     `json:"phase"`
	ArgumentKeys    []string  `json:"argument_keys"`
	ArgumentsSHA256 string    `json:"arguments_sha256"`
	ResultBytes     *int64    `json:"result_bytes,omitempty"`
	ErrorMessage    string    `json:"error_message,omitempty"`
	OccurredAt      time.Time `json:"occurred_at"`
}

// ResourceLease 表示 Agent MCP 可能创建的长生命周期受管资源。
type ResourceLease struct {
	ID            string     `json:"id"`
	RunID         string     `json:"run_id"`
	ResourceKind  string     `json:"resource_kind"`
	ProviderName  string     `json:"provider_name"`
	State         string     `json:"state"`
	OpenedAt      time.Time  `json:"opened_at"`
	ReleasedAt    *time.Time `json:"released_at,omitempty"`
	CleanupReason string     `json:"cleanup_reason,omitempty"`
}
