// Package approvalrequest 定义 Agent 执行期间需要人工决定的结构化权限请求。
package approvalrequest

import (
	"errors"
	"strings"
	"time"
)

// Kind 表示请求影响的资源类型。
type Kind string

const (
	KindCommand     Kind = "COMMAND"
	KindFileChange  Kind = "FILE_CHANGE"
	KindNetwork     Kind = "NETWORK"
	KindPermissions Kind = "PERMISSIONS"
)

// Status 表示请求是否仍在等待人工决定。
type Status string

const (
	StatusOpen     Status = "OPEN"
	StatusResolved Status = "RESOLVED"
	StatusCleared  Status = "CLEARED"
)

// Decision 是面板允许人类做出的有限决定。
type Decision string

const (
	DecisionAcceptOnce    Decision = "ACCEPT_ONCE"
	DecisionAcceptSession Decision = "ACCEPT_SESSION"
	DecisionDecline       Decision = "DECLINE"
	DecisionCancelRun     Decision = "CANCEL_RUN"
)

// Request 是可审计、可恢复的权限等待状态。
type Request struct {
	ID                string     `json:"id"`
	RunID             string     `json:"run_id"`
	TaskID            string     `json:"task_id,omitempty"`
	ExternalRequestID string     `json:"-"`
	ItemID            string     `json:"item_id,omitempty"`
	Kind              Kind       `json:"kind"`
	Reason            string     `json:"reason,omitempty"`
	Command           string     `json:"command,omitempty"`
	CWD               string     `json:"cwd,omitempty"`
	Host              string     `json:"host,omitempty"`
	Protocol          string     `json:"protocol,omitempty"`
	GrantRoot         string     `json:"grant_root,omitempty"`
	Available         []Decision `json:"available_decisions"`
	Status            Status     `json:"status"`
	Decision          Decision   `json:"decision,omitempty"`
	Version           int64      `json:"version"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	ResolvedAt        *time.Time `json:"resolved_at,omitempty"`
}

// CreateInput 是 Runner 从 Adapter 协议映射出的受限请求。
type CreateInput struct {
	ExternalRequestID string
	ItemID            string
	Kind              Kind
	Reason            string
	Command           string
	CWD               string
	Host              string
	Protocol          string
	GrantRoot         string
	Available         []Decision
}

// Normalized 清理 Adapter 提供的非可信文本。
func (input CreateInput) Normalized() CreateInput {
	input.ExternalRequestID = strings.TrimSpace(input.ExternalRequestID)
	input.ItemID = strings.TrimSpace(input.ItemID)
	input.Reason = strings.TrimSpace(input.Reason)
	input.Command = strings.TrimSpace(input.Command)
	input.CWD = strings.TrimSpace(input.CWD)
	input.Host = strings.TrimSpace(input.Host)
	input.Protocol = strings.TrimSpace(input.Protocol)
	input.GrantRoot = strings.TrimSpace(input.GrantRoot)
	return input
}

// Validate 阻止无界或无法解释的权限请求进入数据库和 UI。
func (input CreateInput) Validate() error {
	input = input.Normalized()
	if input.ExternalRequestID == "" || len(input.ExternalRequestID) > 255 {
		return errors.New("external request id is required")
	}
	if !validKind(input.Kind) || len(input.Reason) > 4000 || len(input.Command) > 16000 ||
		len(input.CWD) > 4000 || len(input.Host) > 1000 || len(input.Protocol) > 100 || len(input.GrantRoot) > 4000 {
		return errors.New("invalid approval request")
	}
	if len(input.Available) < 1 || len(input.Available) > 4 {
		return errors.New("approval decisions are required")
	}
	seen := map[Decision]bool{}
	for _, decision := range input.Available {
		if !validDecision(decision) || seen[decision] {
			return errors.New("invalid approval decision")
		}
		seen[decision] = true
	}
	return nil
}

// Allows 判断 Adapter 实际支持的决定是否包含人类选择。
func (request Request) Allows(decision Decision) bool {
	for _, available := range request.Available {
		if available == decision {
			return true
		}
	}
	return false
}

func validKind(kind Kind) bool {
	return kind == KindCommand || kind == KindFileChange || kind == KindNetwork || kind == KindPermissions
}

func validDecision(decision Decision) bool {
	return decision == DecisionAcceptOnce || decision == DecisionAcceptSession ||
		decision == DecisionDecline || decision == DecisionCancelRun
}
