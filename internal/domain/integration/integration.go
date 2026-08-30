// Package integration 定义 Task Branch 到目标分支的可审计集成尝试。
package integration

import "time"

// Operation 表示一次集成尝试要执行的 Git 高层动作。
type Operation string

const (
	OperationIntegrate Operation = "INTEGRATE"
	OperationSync      Operation = "SYNC"
)

// Status 表示集成尝试的持久状态。
type Status string

const (
	StatusRunning   Status = "RUNNING"
	StatusSucceeded Status = "SUCCEEDED"
	StatusNeedsSync Status = "NEEDS_SYNC"
	StatusSynced    Status = "SYNCED"
	StatusConflict  Status = "CONFLICT"
	StatusFailed    Status = "FAILED"
)

// Attempt 保存一次 Git 操作的不可变输入和可恢复结果。
type Attempt struct {
	ID                string    `json:"id"`
	TaskID            string    `json:"task_id"`
	ReviewID          string    `json:"review_id"`
	Operation         Operation `json:"operation"`
	Status            Status    `json:"status"`
	TargetBranch      string    `json:"target_branch"`
	SourceCommitSHA   string    `json:"source_commit_sha"`
	TargetBeforeSHA   string    `json:"target_before_sha"`
	TargetAfterSHA    string    `json:"target_after_sha,omitempty"`
	WorkspaceAfterSHA string    `json:"workspace_after_sha,omitempty"`
	FailureKind       string    `json:"failure_kind,omitempty"`
	FailureMessage    string    `json:"failure_message,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// ReserveInput 是改变 Git 前必须固化的输入。
type ReserveInput struct {
	TaskID          string
	ReviewID        string
	Operation       Operation
	TargetBranch    string
	SourceCommitSHA string
	TargetBeforeSHA string
}
