// Package agentprofile 定义 Agent 职责配置及不可变修订。
package agentprofile

import (
	"errors"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/domain/capability"
)

// Role 是 Control Plane 支持的固定 Agent 职责。
type Role string

const (
	RolePlanner     Role = "PLANNER"
	RoleTriager     Role = "TRIAGER"
	RoleImplementer Role = "IMPLEMENTER"
	RoleRevision    Role = "REVISION"
	RoleReviewer    Role = "REVIEWER"
)

// WorkspacePolicy 是系统强制执行的 Workspace 权限。
type WorkspacePolicy string

const (
	WorkspaceNone      WorkspacePolicy = "NONE"
	WorkspaceReadOnly  WorkspacePolicy = "READ_ONLY"
	WorkspaceWriteTask WorkspacePolicy = "WRITE_TASK"
)

// ApprovalPolicy 是系统强制执行的外部操作批准范围。
type ApprovalPolicy string

const (
	ApprovalReadOnly       ApprovalPolicy = "READ_ONLY"
	ApprovalWorkspaceWrite ApprovalPolicy = "WORKSPACE_WRITE"
)

// Profile 是一个固定职责及其当前配置修订。
type Profile struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Role            Role      `json:"role"`
	CurrentRevision Revision  `json:"current_revision"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Revision 是创建后不可修改的 Agent 配置快照。
type Revision struct {
	ID                   string                `json:"id"`
	ProfileID            string                `json:"profile_id"`
	Revision             int64                 `json:"revision"`
	Instructions         string                `json:"instructions"`
	Adapter              string                `json:"adapter"`
	Command              string                `json:"command"`
	Args                 []string              `json:"args"`
	Model                string                `json:"model"`
	MaxInputTokens       int                   `json:"max_input_tokens"`
	ReservedOutputTokens int                   `json:"reserved_output_tokens"`
	RecentMessageLimit   int                   `json:"recent_message_limit"`
	RetrievalLimit       int                   `json:"retrieval_limit"`
	WorkspacePolicy      WorkspacePolicy       `json:"workspace_policy"`
	ApprovalPolicy       ApprovalPolicy        `json:"approval_policy"`
	TimeoutSeconds       int                   `json:"timeout_seconds"`
	ToolPolicy           capability.ToolPolicy `json:"tool_policy"`
	CreatedAt            time.Time             `json:"created_at"`
}

// RevisionInput 是面板允许用户修改的配置；安全权限不在输入中。
type RevisionInput struct {
	Instructions         string                     `json:"instructions"`
	Adapter              string                     `json:"adapter"`
	Command              string                     `json:"command"`
	Args                 []string                   `json:"args"`
	Model                string                     `json:"model"`
	MaxInputTokens       int                        `json:"max_input_tokens"`
	ReservedOutputTokens int                        `json:"reserved_output_tokens"`
	RecentMessageLimit   int                        `json:"recent_message_limit"`
	RetrievalLimit       int                        `json:"retrieval_limit"`
	TimeoutSeconds       int                        `json:"timeout_seconds"`
	ToolPolicy           capability.ToolPolicyInput `json:"tool_policy"`
}

// Normalized 清理用户可编辑的文本配置。
func (input RevisionInput) Normalized() RevisionInput {
	input.Instructions = strings.TrimSpace(input.Instructions)
	input.Adapter = strings.TrimSpace(input.Adapter)
	input.Command = strings.TrimSpace(input.Command)
	input.Model = strings.TrimSpace(input.Model)
	args := make([]string, 0, len(input.Args))
	for _, argument := range input.Args {
		args = append(args, strings.TrimSpace(argument))
	}
	input.Args = args
	return input
}

// Validate 校验可编辑配置和上下文预算。
func (input RevisionInput) Validate() error {
	input = input.Normalized()
	if len(input.Instructions) < 1 || len(input.Instructions) > 20000 {
		return errors.New("instructions 长度必须为 1 到 20000")
	}
	if input.Adapter != "generic" {
		if input.Adapter != "codex" {
			return errors.New("当前只支持 generic 或 codex adapter")
		}
	}
	if input.Command == "" || len(input.Command) > 1000 {
		return errors.New("command 不能为空且最长 1000 个字符")
	}
	if len(input.Args) > 100 {
		return errors.New("args 最多包含 100 项")
	}
	if input.Adapter == "codex" && (len(input.Args) == 0 || input.Args[0] != "exec") {
		return errors.New("codex adapter 的第一个参数必须是 exec")
	}
	if input.Adapter == "codex" && containsArgument(input.Args, "--approve-for-me") &&
		(containsArgument(input.Args, "--sandbox") || containsArgument(input.Args, "-s")) {
		return errors.New("codex adapter 不能同时使用 --approve-for-me 和 --sandbox")
	}
	if input.MaxInputTokens < 4096 || input.MaxInputTokens > 2_000_000 {
		return errors.New("max_input_tokens 必须为 4096 到 2000000")
	}
	if input.ReservedOutputTokens < 512 || input.ReservedOutputTokens >= input.MaxInputTokens {
		return errors.New("reserved_output_tokens 必须至少为 512 且小于输入预算")
	}
	if input.RecentMessageLimit < 0 || input.RecentMessageLimit > 200 {
		return errors.New("recent_message_limit 必须为 0 到 200")
	}
	if input.RetrievalLimit < 0 || input.RetrievalLimit > 100 {
		return errors.New("retrieval_limit 必须为 0 到 100")
	}
	if input.TimeoutSeconds < 30 || input.TimeoutSeconds > 86400 {
		return errors.New("timeout_seconds 必须为 30 到 86400")
	}
	if err := input.ToolPolicy.Validate(); err != nil {
		return err
	}
	if len(input.ToolPolicy.MCPServers) > 0 && input.Adapter != "codex" {
		return errors.New("MCP Tool Policy 当前要求使用 codex adapter")
	}
	return nil
}

func containsArgument(arguments []string, expected string) bool {
	for _, argument := range arguments {
		if argument == expected {
			return true
		}
	}
	return false
}

// PoliciesForRole 返回不可由用户 Prompt 覆盖的安全策略。
func PoliciesForRole(role Role) (WorkspacePolicy, ApprovalPolicy, error) {
	switch role {
	case RolePlanner, RoleTriager:
		return WorkspaceNone, ApprovalReadOnly, nil
	case RoleImplementer, RoleRevision:
		return WorkspaceWriteTask, ApprovalWorkspaceWrite, nil
	case RoleReviewer:
		return WorkspaceReadOnly, ApprovalReadOnly, nil
	default:
		return "", "", errors.New("未知 Agent role")
	}
}
