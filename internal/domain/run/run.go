// Package run 定义一次不可变 Agent 执行及其生命周期。
package run

import "time"

// Purpose 表示 Run 在 Agent Workflow 中承担的职责。
type Purpose string

const (
	PurposePlanning       Purpose = "PLANNING"
	PurposeTriage         Purpose = "TRIAGE"
	PurposeImplementation Purpose = "IMPLEMENTATION"
	PurposeRevision       Purpose = "REVISION"
	PurposeReview         Purpose = "REVIEW"
)

// Status 表示执行生命周期状态，与 Task 业务状态分离。
type Status string

const (
	StatusClaimed    Status = "CLAIMED"
	StatusStarting   Status = "STARTING"
	StatusRunning    Status = "RUNNING"
	StatusFinalizing Status = "FINALIZING"
	StatusSucceeded  Status = "SUCCEEDED"
	StatusFailed     Status = "FAILED"
	StatusCancelled  Status = "CANCELLED"
	StatusTimedOut   Status = "TIMED_OUT"
	StatusLost       Status = "LOST"
	StatusNeedsInput Status = "NEEDS_INPUT"
)

// Run 保存一次执行的身份、绑定主体和调度快照。
type Run struct {
	ID                  string    `json:"id"`
	Purpose             Purpose   `json:"purpose"`
	TopicID             string    `json:"topic_id,omitempty"`
	TaskID              string    `json:"task_id,omitempty"`
	Status              Status    `json:"status"`
	ProfileRevisionID   string    `json:"profile_revision_id"`
	SubjectVersion      int64     `json:"subject_version"`
	RetryOfRunID        string    `json:"retry_of_run_id,omitempty"`
	ContinuationOfRunID string    `json:"continuation_of_run_id,omitempty"`
	LeaseGeneration     int64     `json:"lease_generation"`
	LeaseExpiresAt      time.Time `json:"lease_expires_at"`
	RunNonce            string    `json:"-"`
	QueuedAt            time.Time `json:"queued_at"`
	ClaimedAt           time.Time `json:"claimed_at"`
	StartedAt           time.Time `json:"started_at,omitempty"`
	FinishedAt          time.Time `json:"finished_at,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// Claim 是只向领取方返回一次的明文 Claim Token。
type Claim struct {
	Run        Run    `json:"run"`
	ClaimToken string `json:"claim_token"`
}

// Artifact 是 Run 产生的大内容文件索引。
type Artifact struct {
	ID           string    `json:"id"`
	RunID        string    `json:"run_id"`
	Kind         string    `json:"kind"`
	RelativePath string    `json:"relative_path"`
	SHA256       string    `json:"sha256"`
	Size         int64     `json:"size"`
	Truncated    bool      `json:"truncated"`
	CreatedAt    time.Time `json:"created_at"`
}

// UsageSource 标识实际 Token 用量的采集来源。
type UsageSource string

const (
	// UsageSourceCodexJSONL 表示用量来自 Codex CLI 的结构化事件。
	UsageSourceCodexJSONL UsageSource = "CODEX_JSONL"
)

// Usage 保存一次 Run 可观测到的真实模型用量；无法采集的字段保持 nil。
type Usage struct {
	RunID                 string      `json:"run_id"`
	InputTokens           *int64      `json:"input_tokens"`
	CachedInputTokens     *int64      `json:"cached_input_tokens"`
	CacheWriteInputTokens *int64      `json:"cache_write_input_tokens"`
	OutputTokens          *int64      `json:"output_tokens"`
	ReasoningOutputTokens *int64      `json:"reasoning_output_tokens"`
	ModelRequests         *int64      `json:"model_requests"`
	PeakInputTokens       *int64      `json:"peak_input_tokens"`
	Source                UsageSource `json:"source"`
	CapturedAt            time.Time   `json:"captured_at"`
}

// PurposeUsage 汇总一种 Run Purpose 的累计实际用量。
type PurposeUsage struct {
	Purpose               Purpose `json:"purpose"`
	TotalRuns             int     `json:"total_runs"`
	RunsWithUsage         int     `json:"runs_with_usage"`
	InputTokens           *int64  `json:"input_tokens"`
	CachedInputTokens     *int64  `json:"cached_input_tokens"`
	UncachedInputTokens   *int64  `json:"uncached_input_tokens"`
	CacheWriteInputTokens *int64  `json:"cache_write_input_tokens"`
	OutputTokens          *int64  `json:"output_tokens"`
	ReasoningOutputTokens *int64  `json:"reasoning_output_tokens"`
	ModelRequests         *int64  `json:"model_requests"`
	PeakInputTokens       *int64  `json:"peak_input_tokens"`
}

// UsageSummary 汇总当前项目的实际 Token 用量和采集覆盖率。
type UsageSummary struct {
	TotalRuns             int            `json:"total_runs"`
	RunsWithUsage         int            `json:"runs_with_usage"`
	InputTokens           *int64         `json:"input_tokens"`
	CachedInputTokens     *int64         `json:"cached_input_tokens"`
	UncachedInputTokens   *int64         `json:"uncached_input_tokens"`
	CacheWriteInputTokens *int64         `json:"cache_write_input_tokens"`
	OutputTokens          *int64         `json:"output_tokens"`
	ReasoningOutputTokens *int64         `json:"reasoning_output_tokens"`
	ModelRequests         *int64         `json:"model_requests"`
	PeakInputTokens       *int64         `json:"peak_input_tokens"`
	ByPurpose             []PurposeUsage `json:"by_purpose"`
}
