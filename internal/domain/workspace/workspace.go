// Package workspace 定义 Task 长期 Git 工作区的持久状态。
package workspace

import "time"

// State 表示 Workspace 生命周期状态。
type State string

const (
	StateProvisioning State = "PROVISIONING"
	StateReady        State = "READY"
	StateDirty        State = "DIRTY"
	StateQuarantined  State = "QUARANTINED"
	StateError        State = "ERROR"
)

// Workspace 保存 Task 分支与 linked worktree 的可审计身份。
type Workspace struct {
	ID             string     `json:"id"`
	TaskID         string     `json:"task_id"`
	Path           string     `json:"path"`
	BranchName     string     `json:"branch_name"`
	TargetBranch   string     `json:"target_branch"`
	BaseCommitSHA  string     `json:"base_commit_sha"`
	HeadSHA        string     `json:"head_sha"`
	State          State      `json:"state"`
	Dirty          bool       `json:"dirty"`
	FailureMessage string     `json:"failure_message,omitempty"`
	LastVerifiedAt *time.Time `json:"last_verified_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
