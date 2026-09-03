package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/domain/capability"
	"github.com/light-speak/aitodos/internal/domain/clarification"
	"github.com/light-speak/aitodos/internal/domain/plan"
	domainrun "github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/topic"
)

var (
	// ErrNoRunnableTask 表示当前没有已配置 Agent 可以领取的 Task。
	ErrNoRunnableTask = errors.New("no runnable task")
	// ErrRunCapacityReached 表示当前项目已达到 Worker 并发上限。
	ErrRunCapacityReached = errors.New("run capacity reached")
	// ErrRunClaimMismatch 表示 Claim Token 或 Lease Generation 不匹配。
	ErrRunClaimMismatch = errors.New("run claim mismatch")
	// ErrRunStateConflict 表示 Run 当前状态不允许请求的生命周期变更。
	ErrRunStateConflict = errors.New("run state conflict")
	// ErrRunNotFound 表示指定 Run 不存在。
	ErrRunNotFound = errors.New("run not found")
	// ErrRunUsageNotFound 表示指定 Run 尚未采集到实际用量。
	ErrRunUsageNotFound = errors.New("run usage not found")
	// ErrRunWorkspaceSnapshotNotFound 表示 Run 尚未完成 Workspace Finalization。
	ErrRunWorkspaceSnapshotNotFound = errors.New("run workspace snapshot not found")
	// ErrRunArtifactNotFound 表示 Run 没有指定类型的 Artifact。
	ErrRunArtifactNotFound = errors.New("run artifact not found")
	// ErrRunClosureNotFound 表示 Run 尚未形成结构化收口报告。
	ErrRunClosureNotFound = errors.New("run closure not found")
)

// RunFinish 描述 Runner 提交的终态和结构化失败。
type RunFinish struct {
	Status           domainrun.Status
	ExitCode         int
	FailureKind      string
	FailureCode      string
	FailureMessage   string
	FailureRetryable *bool
}

// FinalizationIntent 在 Agent 退出后、Workspace 收尾前冻结预期终态，供崩溃恢复幂等重放。
type FinalizationIntent struct {
	Finish        RunFinish
	Clarification *clarification.Request
	Planning      *plan.PlanningResult
	Closure       *domainrun.Closure
	TaskReply     string
}

// RecoveryRun 是 Recovery Manager 校验旧 Runner 所需的最小进程身份快照。
type RecoveryRun struct {
	Run            domainrun.Run
	RunnerPID      int
	RunnerIdentity string
	AgentPID       int
	AgentIdentity  string
	RunNonce       string
}

// RunQuery 是项目内 Run 历史的有界筛选条件。
type RunQuery struct {
	TaskID      string
	TopicID     string
	ActiveOnly  bool
	Status      domainrun.Status
	Purpose     domainrun.Purpose
	BeforeTime  time.Time
	BeforeRunID string
	Limit       int
}

// RunPage 返回一页稳定排序的 Run 和是否存在下一页。
type RunPage struct {
	Items   []domainrun.Run
	HasMore bool
}

const runColumns = `
id, purpose, COALESCE(topic_id, ''), COALESCE(task_id, ''), status,
profile_revision_id, COALESCE(retry_of_run_id, ''), COALESCE(continuation_of_run_id, ''), lease_generation,
lease_expires_at, queued_at, claimed_at,
COALESCE(started_at, ''), COALESCE(finished_at, ''), created_at, updated_at,
subject_version, exit_code, failure_kind, failure_code, failure_message, failure_retryable,
COALESCE(cancel_requested_at, ''), cancel_reason,
COALESCE(agent_session_id, ''), session_resumed, run_nonce`

// RunStore 原子领取 Task 并持久化 Run 生命周期。
type RunStore struct {
	database *sql.DB
}

// FinishNeedsInput 原子结束 Run、阻塞 Task 并创建待人工回答的问题。
func (store *RunStore) FinishNeedsInput(
	ctx context.Context,
	runID string,
	claimToken string,
	leaseGeneration int64,
	request clarification.Request,
) (domainrun.Run, clarification.Clarification, error) {
	request = request.Normalized()
	if err := request.Validate(); err != nil {
		return domainrun.Run{}, clarification.Clarification{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return domainrun.Run{}, clarification.Clarification{}, fmt.Errorf("begin run clarification: %w", err)
	}
	defer transaction.Rollback()
	current, err := authorizeRun(ctx, transaction, runID, claimToken, leaseGeneration)
	if err != nil {
		return domainrun.Run{}, clarification.Clarification{}, err
	}
	if (current.Status != domainrun.StatusRunning && current.Status != domainrun.StatusStarting && current.Status != domainrun.StatusFinalizing) ||
		(current.Purpose != domainrun.PurposePlanning && current.Purpose != domainrun.PurposeTriage &&
			current.Purpose != domainrun.PurposeImplementation && current.Purpose != domainrun.PurposeRevision) {
		return domainrun.Run{}, clarification.Clarification{}, ErrRunStateConflict
	}
	created, err := buildClarification(current, request)
	if err != nil {
		return domainrun.Run{}, clarification.Clarification{}, err
	}
	if err := insertClarification(ctx, transaction, created); err != nil {
		return domainrun.Run{}, clarification.Clarification{}, err
	}
	if current.TaskID != "" && current.Purpose != domainrun.PurposeTriage {
		if err := finishTaskRun(ctx, transaction, current, domainrun.StatusNeedsInput); err != nil {
			return domainrun.Run{}, clarification.Clarification{}, err
		}
	}
	now := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE runs SET status = 'NEEDS_INPUT', finished_at = ?, exit_code = 0, updated_at = ?
WHERE id = ? AND status IN ('STARTING', 'RUNNING', 'FINALIZING') AND lease_generation = ?`,
		formatTime(now), formatTime(now), runID, leaseGeneration)
	if err != nil {
		return domainrun.Run{}, clarification.Clarification{}, fmt.Errorf("finish run for clarification: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return domainrun.Run{}, clarification.Clarification{}, err
	}
	if _, err := appendRunStatusEvent(ctx, transaction, current.ID, current.Status, domainrun.StatusNeedsInput, ""); err != nil {
		return domainrun.Run{}, clarification.Clarification{}, err
	}
	closure, err := resolveRunClosure(current, RunFinish{Status: domainrun.StatusNeedsInput, ExitCode: 0}, &request, nil, nil, "", now)
	if err != nil {
		return domainrun.Run{}, clarification.Clarification{}, err
	}
	if err := insertRunClosure(ctx, transaction, closure); err != nil {
		return domainrun.Run{}, clarification.Clarification{}, err
	}
	if err := createRunSummary(ctx, transaction, string(domainrun.StatusNeedsInput), runID, string(current.Purpose), closureProjectionSummary(closure), now); err != nil {
		return domainrun.Run{}, clarification.Clarification{}, err
	}
	current.Status = domainrun.StatusNeedsInput
	exitCode := 0
	current.ExitCode = &exitCode
	current.FinishedAt = now
	current.UpdatedAt = now
	if err := transaction.Commit(); err != nil {
		return domainrun.Run{}, clarification.Clarification{}, fmt.Errorf("commit run clarification: %w", err)
	}
	return current, created, nil
}

// NewRunStore 创建 Run 持久化服务。
func NewRunStore(database *sql.DB) *RunStore {
	return &RunStore{database: database}
}

// RequestCancel 只记录取消意图；Runner 确认 Agent 进程组退出并完成 Finalization 后才能写 CANCELLED 终态。
func (store *RunStore) RequestCancel(ctx context.Context, runID string, reason string) (domainrun.Run, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "用户请求取消当前 Run"
	}
	if len([]rune(reason)) > 1000 {
		return domainrun.Run{}, errors.New("cancel reason must not exceed 1000 characters")
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("begin request run cancel: %w", err)
	}
	defer transaction.Rollback()
	now := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE runs SET cancel_requested_at = ?, cancel_reason = ?, updated_at = ?
WHERE id = ? AND status IN ('CLAIMED', 'STARTING', 'RUNNING') AND cancel_requested_at IS NULL`,
		formatTime(now), reason, formatTime(now), runID)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("request run cancel: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("read run cancel result: %w", err)
	}
	if changed == 0 {
		current, getErr := getRun(ctx, transaction, runID)
		if getErr != nil {
			return domainrun.Run{}, getErr
		}
		if current.CancelRequestedAt != nil {
			return current, nil
		}
		return domainrun.Run{}, ErrRunStateConflict
	}
	if _, err := appendRunEvent(ctx, transaction, runID, domainrun.EventCancelRequested, map[string]any{
		"schema_version": 1, "reason": reason,
	}); err != nil {
		return domainrun.Run{}, err
	}
	current, err := getRun(ctx, transaction, runID)
	if err != nil {
		return domainrun.Run{}, err
	}
	if err := transaction.Commit(); err != nil {
		return domainrun.Run{}, fmt.Errorf("commit request run cancel: %w", err)
	}
	return current, nil
}

// CancellationRequested 让当前持有 fencing token 的 Runner 检查取消意图。
func (store *RunStore) CancellationRequested(
	ctx context.Context,
	runID string,
	claimToken string,
	leaseGeneration int64,
) (bool, error) {
	transaction, err := store.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, fmt.Errorf("begin read run cancellation: %w", err)
	}
	defer transaction.Rollback()
	current, err := authorizeRun(ctx, transaction, runID, claimToken, leaseGeneration)
	if err != nil {
		return false, err
	}
	return current.CancelRequestedAt != nil, nil
}

// ClaimNextTask 按“修订优先、P0-P3、FIFO”领取一个 Task。
func (store *RunStore) ClaimNextTask(ctx context.Context, maxWorkers int, leaseDuration time.Duration) (domainrun.Claim, error) {
	if maxWorkers < 1 {
		return domainrun.Claim{}, errors.New("max workers must be positive")
	}
	if leaseDuration <= 0 {
		return domainrun.Claim{}, errors.New("lease duration must be positive")
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return domainrun.Claim{}, fmt.Errorf("begin run claim: %w", err)
	}
	defer transaction.Rollback()
	if err := ensureRunCapacity(ctx, transaction, maxWorkers); err != nil {
		return domainrun.Claim{}, err
	}
	selected, err := selectRunnableWork(ctx, transaction)
	if err != nil {
		return domainrun.Claim{}, err
	}
	claim, err := createClaim(selected, leaseDuration)
	if err != nil {
		return domainrun.Claim{}, err
	}
	claim.Run.AgentSessionID, err = findCompatibleAgentSession(ctx, transaction, selected)
	if err != nil {
		return domainrun.Claim{}, err
	}
	claim.Run.SessionResumed = claim.Run.AgentSessionID != ""
	if err := insertClaimedRun(ctx, transaction, claim); err != nil {
		return domainrun.Claim{}, err
	}
	if selected.FeedbackID != "" {
		if err := claimTaskFeedback(ctx, transaction, selected.FeedbackID, claim.Run.ID); err != nil {
			return domainrun.Claim{}, err
		}
	}
	if _, err := appendRunEvent(ctx, transaction, claim.Run.ID, domainrun.EventClaimed, map[string]any{
		"schema_version": 1, "status": claim.Run.Status, "purpose": claim.Run.Purpose,
	}); err != nil {
		return domainrun.Claim{}, err
	}
	if err := createRunToolPolicySnapshot(ctx, transaction, claim.Run.ID, claim.Run.ProfileRevisionID); err != nil {
		return domainrun.Claim{}, err
	}
	if claim.Run.TaskID != "" && selected.Purpose != domainrun.PurposeTriage && selected.Purpose != domainrun.PurposeReview {
		if err := claimTask(ctx, transaction, selected, claim.Run); err != nil {
			return domainrun.Claim{}, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return domainrun.Claim{}, fmt.Errorf("commit run claim: %w", err)
	}
	return claim, nil
}

// ListTaskRuns 按新到旧返回 Task 的 Run 历史。
func (store *RunStore) ListTaskRuns(ctx context.Context, taskID string) ([]domainrun.Run, error) {
	rows, err := store.database.QueryContext(ctx, `SELECT `+runColumns+`
FROM runs WHERE task_id = ? ORDER BY created_at DESC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task runs: %w", err)
	}
	defer rows.Close()
	runs := make([]domainrun.Run, 0)
	for rows.Next() {
		item, scanErr := scanRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		runs = append(runs, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task runs: %w", err)
	}
	return runs, nil
}

// Query 按 created_at 和 id 倒序分页查询当前项目 Run。
func (store *RunStore) Query(ctx context.Context, query RunQuery) (RunPage, error) {
	if query.Limit < 1 || query.Limit > 100 {
		return RunPage{}, errors.New("run query limit must be between 1 and 100")
	}
	conditions := []string{"1 = 1"}
	arguments := make([]any, 0, 7)
	if query.TaskID != "" {
		conditions = append(conditions, "task_id = ?")
		arguments = append(arguments, query.TaskID)
	}
	if query.TopicID != "" {
		conditions = append(conditions, "topic_id = ?")
		arguments = append(arguments, query.TopicID)
	}
	if query.ActiveOnly {
		conditions = append(conditions, "status IN ('CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING')")
	}
	if query.Status != "" {
		conditions = append(conditions, "status = ?")
		arguments = append(arguments, query.Status)
	}
	if query.Purpose != "" {
		conditions = append(conditions, "purpose = ?")
		arguments = append(arguments, query.Purpose)
	}
	if !query.BeforeTime.IsZero() {
		if query.BeforeRunID == "" {
			return RunPage{}, errors.New("run query cursor ID is required")
		}
		conditions = append(conditions, "(created_at < ? OR (created_at = ? AND id < ?))")
		formatted := formatTime(query.BeforeTime)
		arguments = append(arguments, formatted, formatted, query.BeforeRunID)
	}
	arguments = append(arguments, query.Limit+1)
	rows, err := store.database.QueryContext(ctx, `SELECT `+runColumns+`
FROM runs WHERE `+strings.Join(conditions, " AND ")+`
ORDER BY created_at DESC, id DESC LIMIT ?`, arguments...)
	if err != nil {
		return RunPage{}, fmt.Errorf("query runs: %w", err)
	}
	defer rows.Close()
	items := make([]domainrun.Run, 0, query.Limit+1)
	for rows.Next() {
		item, scanErr := scanRun(rows)
		if scanErr != nil {
			return RunPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return RunPage{}, fmt.Errorf("iterate queried runs: %w", err)
	}
	hasMore := len(items) > query.Limit
	if hasMore {
		items = items[:query.Limit]
	}
	return RunPage{Items: items, HasMore: hasMore}, nil
}

// ListEvents 返回指定 sequence 之后的 Run Event，供查询和 SSE 断线续传复用。
func (store *RunStore) ListEvents(ctx context.Context, runID string, afterSequence int64, limit int) ([]domainrun.Event, error) {
	if afterSequence < 0 {
		return nil, errors.New("run event sequence must not be negative")
	}
	if limit < 1 || limit > 500 {
		return nil, errors.New("run event limit must be between 1 and 500")
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT id, run_id, sequence, event_type, payload_json, occurred_at
FROM run_events WHERE run_id = ? AND sequence > ? ORDER BY sequence LIMIT ?`, runID, afterSequence, limit)
	if err != nil {
		return nil, fmt.Errorf("list run events: %w", err)
	}
	defer rows.Close()
	events := make([]domainrun.Event, 0)
	for rows.Next() {
		var event domainrun.Event
		var payload, occurredAt string
		if err := rows.Scan(&event.ID, &event.RunID, &event.Sequence, &event.Type, &payload, &occurredAt); err != nil {
			return nil, fmt.Errorf("scan run event: %w", err)
		}
		event.Payload = json.RawMessage(payload)
		event.OccurredAt, err = parseTime(occurredAt)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run events: %w", err)
	}
	return events, nil
}

// Get 按 ID 返回 Run 当前状态。
func (store *RunStore) Get(ctx context.Context, runID string) (domainrun.Run, error) {
	return getRun(ctx, store.database, runID)
}

// ListRecoveryRuns 返回 Daemon 启动时仍处于非终态的 Run 及其 Runner 身份。
func (store *RunStore) ListRecoveryRuns(ctx context.Context) ([]RecoveryRun, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, COALESCE(runner_pid, 0), runner_identity,
       COALESCE(agent_pid, 0), agent_identity, run_nonce
FROM runs WHERE status IN ('CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING')
ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list recovery runs: %w", err)
	}
	defer rows.Close()
	result := make([]RecoveryRun, 0)
	for rows.Next() {
		var item RecoveryRun
		if err := rows.Scan(&item.Run.ID, &item.RunnerPID, &item.RunnerIdentity,
			&item.AgentPID, &item.AgentIdentity, &item.RunNonce); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range result {
		result[index].Run, err = store.Get(ctx, result[index].Run.ID)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// AttachAgentProcess 记录 Runner 实际启动且可验证的 Agent 进程组身份。
func (store *RunStore) AttachAgentProcess(
	ctx context.Context,
	runID string,
	claimToken string,
	leaseGeneration int64,
	agentPID int,
	agentIdentity string,
) error {
	if agentPID < 1 || len(agentIdentity) != 64 {
		return errors.New("invalid agent process identity")
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin attach agent process: %w", err)
	}
	defer transaction.Rollback()
	current, err := authorizeRun(ctx, transaction, runID, claimToken, leaseGeneration)
	if err != nil {
		return err
	}
	if current.Status != domainrun.StatusStarting && current.Status != domainrun.StatusRunning {
		return ErrRunStateConflict
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE runs SET agent_pid = ?, agent_started_at = ?, agent_identity = ?,
                agent_process_released_at = NULL, updated_at = ?
WHERE id = ? AND lease_generation = ? AND status IN ('STARTING', 'RUNNING')`,
		agentPID, formatTime(time.Now().UTC()), agentIdentity, formatTime(time.Now().UTC()), runID, leaseGeneration)
	if err != nil {
		return fmt.Errorf("attach agent process: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return err
	}
	return transaction.Commit()
}

// ReleaseAgentProcess 清除活跃 Agent 身份，同时保留已释放时间用于审计。
func (store *RunStore) ReleaseAgentProcess(ctx context.Context, runID, claimToken string, leaseGeneration int64) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin release agent process: %w", err)
	}
	defer transaction.Rollback()
	if _, err := authorizeRun(ctx, transaction, runID, claimToken, leaseGeneration); err != nil {
		return err
	}
	now := formatTime(time.Now().UTC())
	result, err := transaction.ExecContext(ctx, `
UPDATE runs SET agent_pid = NULL, agent_identity = '', agent_process_released_at = ?, updated_at = ?
WHERE id = ? AND lease_generation = ?`, now, now, runID, leaseGeneration)
	if err != nil {
		return fmt.Errorf("release agent process: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return err
	}
	return transaction.Commit()
}

// RecoverLost 在 Recovery Manager 证明旧 Runner 不存在后，将不明确执行标为 LOST。
func (store *RunStore) RecoverLost(
	ctx context.Context,
	runID string,
	leaseGeneration int64,
	code string,
	message string,
) (domainrun.Run, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("begin recover lost run: %w", err)
	}
	defer transaction.Rollback()
	current, err := getRun(ctx, transaction, runID)
	if err != nil {
		return domainrun.Run{}, err
	}
	if current.LeaseGeneration != leaseGeneration {
		return domainrun.Run{}, ErrRunClaimMismatch
	}
	if current.Status != domainrun.StatusClaimed && current.Status != domainrun.StatusStarting &&
		current.Status != domainrun.StatusRunning && current.Status != domainrun.StatusFinalizing {
		return current, nil
	}
	if current.Purpose == domainrun.PurposeReview {
		if err := failTaskFeedback(ctx, transaction, current.ID, message); err != nil {
			return domainrun.Run{}, err
		}
	}
	if current.TaskID != "" && current.Purpose != domainrun.PurposeTriage && current.Purpose != domainrun.PurposeReview {
		if err := finishTaskRun(ctx, transaction, current, domainrun.StatusLost); err != nil {
			return domainrun.Run{}, err
		}
	}
	now := time.Now().UTC()
	retryable := false
	result, err := transaction.ExecContext(ctx, `
UPDATE runs SET status = 'LOST', finished_at = ?, exit_code = -1,
    failure_kind = 'INFRASTRUCTURE', failure_code = ?, failure_message = ?,
    failure_retryable = ?, updated_at = ?
WHERE id = ? AND lease_generation = ? AND status IN ('CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING')`,
		formatTime(now), code, message, retryable, formatTime(now), runID, leaseGeneration)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("recover lost run: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return domainrun.Run{}, err
	}
	if err := clearOpenApprovals(ctx, transaction, runID, now); err != nil {
		return domainrun.Run{}, err
	}
	if _, err := appendRunStatusEvent(ctx, transaction, runID, current.Status, domainrun.StatusLost, code); err != nil {
		return domainrun.Run{}, err
	}
	current.Status, current.FinishedAt, current.UpdatedAt = domainrun.StatusLost, now, now
	current.ExitCode, current.FailureKind, current.FailureCode = intPointer(-1), "INFRASTRUCTURE", code
	current.FailureMessage, current.FailureRetryable = message, &retryable
	if err := transaction.Commit(); err != nil {
		return domainrun.Run{}, fmt.Errorf("commit recovered lost run: %w", err)
	}
	return current, nil
}

// GetToolPolicySnapshot 返回 Run 创建时固化的能力范围并校验内容哈希。
func (store *RunStore) GetToolPolicySnapshot(ctx context.Context, runID string) (capability.ToolPolicySnapshot, error) {
	var encoded, expectedHash string
	err := store.database.QueryRowContext(ctx, `
SELECT snapshot_json, sha256 FROM run_tool_policy_snapshots WHERE run_id = ?`, runID).Scan(&encoded, &expectedHash)
	if errors.Is(err, sql.ErrNoRows) {
		return capability.ToolPolicySnapshot{}, ErrRunNotFound
	}
	if err != nil {
		return capability.ToolPolicySnapshot{}, fmt.Errorf("read run tool policy snapshot: %w", err)
	}
	actualHash := sha256.Sum256([]byte(encoded))
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(actualHash[:])), []byte(expectedHash)) != 1 {
		return capability.ToolPolicySnapshot{}, errors.New("run tool policy snapshot hash mismatch")
	}
	var snapshot capability.ToolPolicySnapshot
	if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
		return capability.ToolPolicySnapshot{}, fmt.Errorf("decode run tool policy snapshot: %w", err)
	}
	return snapshot, nil
}

// RecordUsage 幂等保存 Adapter 从结构化事件采集到的实际用量。
func (store *RunStore) RecordUsage(ctx context.Context, usage domainrun.Usage) (domainrun.Usage, error) {
	if err := validateRunUsage(usage); err != nil {
		return domainrun.Usage{}, err
	}
	usage.CapturedAt = time.Now().UTC()
	_, err := store.database.ExecContext(ctx, `
INSERT INTO run_usage(
    run_id, input_tokens, cached_input_tokens, cache_write_input_tokens,
    output_tokens, reasoning_output_tokens, model_requests, peak_input_tokens,
    source, captured_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET
    input_tokens = excluded.input_tokens,
    cached_input_tokens = excluded.cached_input_tokens,
    cache_write_input_tokens = excluded.cache_write_input_tokens,
    output_tokens = excluded.output_tokens,
    reasoning_output_tokens = excluded.reasoning_output_tokens,
    model_requests = excluded.model_requests,
    peak_input_tokens = excluded.peak_input_tokens,
    source = excluded.source,
    captured_at = excluded.captured_at`,
		usage.RunID, usage.InputTokens, usage.CachedInputTokens, usage.CacheWriteInputTokens,
		usage.OutputTokens, usage.ReasoningOutputTokens, usage.ModelRequests, usage.PeakInputTokens,
		usage.Source, formatTime(usage.CapturedAt),
	)
	if err != nil {
		return domainrun.Usage{}, fmt.Errorf("record run usage: %w", err)
	}
	return usage, nil
}

// GetUsage 返回指定 Run 已采集的实际用量。
func (store *RunStore) GetUsage(ctx context.Context, runID string) (domainrun.Usage, error) {
	row := store.database.QueryRowContext(ctx, `
SELECT run_id, input_tokens, cached_input_tokens, cache_write_input_tokens,
       output_tokens, reasoning_output_tokens, model_requests, peak_input_tokens,
       source, captured_at
FROM run_usage WHERE run_id = ?`, runID)
	usage, err := scanRunUsage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainrun.Usage{}, ErrRunUsageNotFound
	}
	if err != nil {
		return domainrun.Usage{}, fmt.Errorf("get run usage: %w", err)
	}
	return usage, nil
}

// UsageSummary 按 Purpose 汇总项目累计实际用量和采集覆盖率。
func (store *RunStore) UsageSummary(ctx context.Context) (domainrun.UsageSummary, error) {
	summary, err := scanUsageSummary(store.database.QueryRowContext(ctx, usageSummaryQuery("")))
	if err != nil {
		return domainrun.UsageSummary{}, fmt.Errorf("read run usage summary: %w", err)
	}
	rows, err := store.database.QueryContext(ctx, usageSummaryQuery("GROUP BY r.purpose ORDER BY CASE r.purpose WHEN 'PLANNING' THEN 1 WHEN 'TRIAGE' THEN 2 WHEN 'IMPLEMENTATION' THEN 3 WHEN 'REVISION' THEN 4 ELSE 5 END"))
	if err != nil {
		return domainrun.UsageSummary{}, fmt.Errorf("list purpose usage: %w", err)
	}
	defer rows.Close()
	summary.ByPurpose = make([]domainrun.PurposeUsage, 0)
	for rows.Next() {
		purpose, item, scanErr := scanPurposeUsage(rows)
		if scanErr != nil {
			return domainrun.UsageSummary{}, scanErr
		}
		item.Purpose = domainrun.Purpose(purpose)
		summary.ByPurpose = append(summary.ByPurpose, item)
	}
	if err := rows.Err(); err != nil {
		return domainrun.UsageSummary{}, fmt.Errorf("iterate purpose usage: %w", err)
	}
	return summary, nil
}

func validateRunUsage(usage domainrun.Usage) error {
	if strings.TrimSpace(usage.RunID) == "" || usage.Source != domainrun.UsageSourceCodexJSONL {
		return errors.New("invalid run usage identity or source")
	}
	values := []*int64{usage.InputTokens, usage.CachedInputTokens, usage.CacheWriteInputTokens,
		usage.OutputTokens, usage.ReasoningOutputTokens, usage.ModelRequests, usage.PeakInputTokens}
	known := false
	for _, value := range values {
		if value != nil {
			known = true
			if *value < 0 {
				return errors.New("run usage token values must not be negative")
			}
		}
	}
	if !known || (usage.ModelRequests != nil && *usage.ModelRequests == 0) {
		return errors.New("run usage must contain at least one valid metric")
	}
	if usage.InputTokens != nil && usage.CachedInputTokens != nil && *usage.CachedInputTokens > *usage.InputTokens {
		return errors.New("cached input tokens exceed input tokens")
	}
	if usage.InputTokens != nil && usage.PeakInputTokens != nil && *usage.PeakInputTokens > *usage.InputTokens {
		return errors.New("peak input tokens exceed cumulative input tokens")
	}
	return nil
}

type usageScanner interface {
	Scan(dest ...any) error
}

func scanRunUsage(scanner usageScanner) (domainrun.Usage, error) {
	var usage domainrun.Usage
	var input, cached, cacheWrite, output, reasoning, requests, peak sql.NullInt64
	var captured string
	if err := scanner.Scan(&usage.RunID, &input, &cached, &cacheWrite, &output, &reasoning,
		&requests, &peak, &usage.Source, &captured); err != nil {
		return domainrun.Usage{}, err
	}
	usage.InputTokens = optionalInt64(input)
	usage.CachedInputTokens = optionalInt64(cached)
	usage.CacheWriteInputTokens = optionalInt64(cacheWrite)
	usage.OutputTokens = optionalInt64(output)
	usage.ReasoningOutputTokens = optionalInt64(reasoning)
	usage.ModelRequests = optionalInt64(requests)
	usage.PeakInputTokens = optionalInt64(peak)
	parsed, err := parseTime(captured)
	if err != nil {
		return domainrun.Usage{}, err
	}
	usage.CapturedAt = parsed
	return usage, nil
}

func usageSummaryQuery(suffix string) string {
	purpose := "''"
	if suffix != "" {
		purpose = "r.purpose"
	}
	return `SELECT ` + purpose + `, COUNT(r.id), COUNT(u.run_id),
       SUM(u.input_tokens), SUM(u.cached_input_tokens),
       SUM(CASE WHEN u.input_tokens IS NOT NULL AND u.cached_input_tokens IS NOT NULL
                THEN u.input_tokens - u.cached_input_tokens END),
       SUM(u.cache_write_input_tokens), SUM(u.output_tokens),
       SUM(u.reasoning_output_tokens), SUM(u.model_requests), MAX(u.peak_input_tokens)
FROM runs r LEFT JOIN run_usage u ON u.run_id = r.id ` + suffix
}

func scanUsageSummary(scanner usageScanner) (domainrun.UsageSummary, error) {
	_, item, err := scanPurposeUsage(scanner)
	if err != nil {
		return domainrun.UsageSummary{}, err
	}
	return domainrun.UsageSummary{
		TotalRuns: item.TotalRuns, RunsWithUsage: item.RunsWithUsage,
		InputTokens: item.InputTokens, CachedInputTokens: item.CachedInputTokens,
		UncachedInputTokens: item.UncachedInputTokens, CacheWriteInputTokens: item.CacheWriteInputTokens,
		OutputTokens: item.OutputTokens, ReasoningOutputTokens: item.ReasoningOutputTokens,
		ModelRequests: item.ModelRequests, PeakInputTokens: item.PeakInputTokens,
	}, nil
}

func scanPurposeUsage(scanner usageScanner) (string, domainrun.PurposeUsage, error) {
	var purpose string
	var item domainrun.PurposeUsage
	var input, cached, uncached, cacheWrite, output, reasoning, requests, peak sql.NullInt64
	if err := scanner.Scan(&purpose, &item.TotalRuns, &item.RunsWithUsage, &input, &cached,
		&uncached, &cacheWrite, &output, &reasoning, &requests, &peak); err != nil {
		return "", domainrun.PurposeUsage{}, err
	}
	item.InputTokens = optionalInt64(input)
	item.CachedInputTokens = optionalInt64(cached)
	item.UncachedInputTokens = optionalInt64(uncached)
	item.CacheWriteInputTokens = optionalInt64(cacheWrite)
	item.OutputTokens = optionalInt64(output)
	item.ReasoningOutputTokens = optionalInt64(reasoning)
	item.ModelRequests = optionalInt64(requests)
	item.PeakInputTokens = optionalInt64(peak)
	return purpose, item, nil
}

func optionalInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

// RecordArtifact 保存已原子写入 Artifact Store 的文件索引。
func (store *RunStore) RecordArtifact(ctx context.Context, input domainrun.Artifact) (domainrun.Artifact, error) {
	cleaned := filepath.ToSlash(filepath.Clean(input.RelativePath))
	if cleaned == "." || filepath.IsAbs(input.RelativePath) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return domainrun.Artifact{}, errors.New("invalid run artifact path")
	}
	if !validArtifactKind(input.Kind) || input.RunID == "" || input.SHA256 == "" || input.Size < 0 {
		return domainrun.Artifact{}, errors.New("invalid run artifact metadata")
	}
	id, err := newID()
	if err != nil {
		return domainrun.Artifact{}, err
	}
	created := input
	created.ID = id
	created.RelativePath = cleaned
	created.CreatedAt = time.Now().UTC()
	_, err = store.database.ExecContext(ctx, `
INSERT INTO run_artifacts(id, run_id, kind, relative_path, sha256, size, truncated, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, created.ID, created.RunID, created.Kind,
		created.RelativePath, created.SHA256, created.Size, created.Truncated, formatTime(created.CreatedAt))
	if err != nil {
		return domainrun.Artifact{}, fmt.Errorf("record run artifact: %w", err)
	}
	return created, nil
}

// ListArtifacts 按类型返回一个 Run 的 Artifact 索引。
func (store *RunStore) ListArtifacts(ctx context.Context, runID string) ([]domainrun.Artifact, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, run_id, kind, relative_path, sha256, size, truncated, created_at
FROM run_artifacts WHERE run_id = ? ORDER BY kind`, runID)
	if err != nil {
		return nil, fmt.Errorf("list run artifacts: %w", err)
	}
	defer rows.Close()
	result := make([]domainrun.Artifact, 0)
	for rows.Next() {
		artifact, scanErr := scanRunArtifact(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run artifacts: %w", err)
	}
	return result, nil
}

// GetArtifact 返回一个 Run 的指定类型 Artifact。
func (store *RunStore) GetArtifact(ctx context.Context, runID, kind string) (domainrun.Artifact, error) {
	artifact, err := scanRunArtifact(store.database.QueryRowContext(ctx, `
SELECT id, run_id, kind, relative_path, sha256, size, truncated, created_at
FROM run_artifacts WHERE run_id = ? AND kind = ?`, runID, kind))
	if errors.Is(err, sql.ErrNoRows) {
		return domainrun.Artifact{}, ErrRunArtifactNotFound
	}
	return artifact, err
}

func scanRunArtifact(scanner rowScanner) (domainrun.Artifact, error) {
	var artifact domainrun.Artifact
	var truncated int
	var createdAt string
	if err := scanner.Scan(&artifact.ID, &artifact.RunID, &artifact.Kind, &artifact.RelativePath,
		&artifact.SHA256, &artifact.Size, &truncated, &createdAt); err != nil {
		return domainrun.Artifact{}, err
	}
	artifact.Truncated = truncated == 1
	parsed, err := parseTime(createdAt)
	if err != nil {
		return domainrun.Artifact{}, fmt.Errorf("parse run artifact time: %w", err)
	}
	artifact.CreatedAt = parsed
	return artifact, nil
}

func validArtifactKind(kind string) bool {
	switch kind {
	case "PROMPT", "CONTEXT_MANIFEST", "STDOUT", "STDERR", "RESULT":
		return true
	default:
		return false
	}
}

// Start 校验 Claim 后将 Run 标记为运行中并延长 Lease。
func (store *RunStore) Start(
	ctx context.Context,
	runID string,
	claimToken string,
	leaseGeneration int64,
	runnerPID int,
	runnerIdentity string,
	leaseDuration time.Duration,
) (domainrun.Run, error) {
	if runnerPID < 1 || len(runnerIdentity) != 64 {
		return domainrun.Run{}, errors.New("invalid runner process identity")
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("begin run start: %w", err)
	}
	defer transaction.Rollback()
	current, err := authorizeRun(ctx, transaction, runID, claimToken, leaseGeneration)
	if err != nil {
		return domainrun.Run{}, err
	}
	if current.Status != domainrun.StatusClaimed {
		return domainrun.Run{}, ErrRunStateConflict
	}
	now := time.Now().UTC()
	current.Status = domainrun.StatusStarting
	current.StartedAt = now
	current.UpdatedAt = now
	current.LeaseExpiresAt = now.Add(leaseDuration)
	result, err := transaction.ExecContext(ctx, `
UPDATE runs SET status = ?, runner_pid = ?, runner_started_at = ?, runner_identity = ?,
                started_at = ?, lease_expires_at = ?, lease_heartbeat_at = ?, updated_at = ?
WHERE id = ? AND status = 'CLAIMED' AND lease_generation = ?`,
		current.Status, runnerPID, formatTime(now), runnerIdentity, formatTime(now),
		formatTime(current.LeaseExpiresAt), formatTime(now), formatTime(now), runID, leaseGeneration,
	)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("start run: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return domainrun.Run{}, err
	}
	if _, err := appendRunStatusEvent(ctx, transaction, current.ID, domainrun.StatusClaimed, domainrun.StatusStarting, ""); err != nil {
		return domainrun.Run{}, err
	}
	if err := transaction.Commit(); err != nil {
		return domainrun.Run{}, fmt.Errorf("commit run start: %w", err)
	}
	return current, nil
}

// RenewLease 由持有 fencing token 的 Runner 定期续租；失败后 Runner 必须停止 Agent。
func (store *RunStore) RenewLease(
	ctx context.Context,
	runID string,
	claimToken string,
	leaseGeneration int64,
	leaseDuration time.Duration,
) error {
	if leaseDuration <= 0 {
		return errors.New("lease duration must be positive")
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin renew run lease: %w", err)
	}
	defer transaction.Rollback()
	current, err := authorizeRun(ctx, transaction, runID, claimToken, leaseGeneration)
	if err != nil {
		return err
	}
	if current.Status != domainrun.StatusStarting && current.Status != domainrun.StatusRunning && current.Status != domainrun.StatusFinalizing {
		return ErrRunStateConflict
	}
	now := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE runs SET lease_expires_at = ?, lease_heartbeat_at = ?, updated_at = ?
WHERE id = ? AND lease_generation = ? AND status IN ('STARTING', 'RUNNING', 'FINALIZING')`,
		formatTime(now.Add(leaseDuration)), formatTime(now), formatTime(now), runID, leaseGeneration)
	if err != nil {
		return fmt.Errorf("renew run lease: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return err
	}
	return transaction.Commit()
}

// MarkRunning 在 Workspace 和 Context 准备完成后进入 Agent 执行阶段。
func (store *RunStore) MarkRunning(
	ctx context.Context,
	runID string,
	claimToken string,
	leaseGeneration int64,
) (domainrun.Run, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("begin mark run running: %w", err)
	}
	defer transaction.Rollback()
	current, err := authorizeRun(ctx, transaction, runID, claimToken, leaseGeneration)
	if err != nil {
		return domainrun.Run{}, err
	}
	if current.Status != domainrun.StatusStarting {
		return domainrun.Run{}, ErrRunStateConflict
	}
	now := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE runs SET status = 'RUNNING', updated_at = ?
WHERE id = ? AND status = 'STARTING' AND lease_generation = ?`,
		formatTime(now), runID, leaseGeneration)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("mark run running: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return domainrun.Run{}, err
	}
	if _, err := appendRunStatusEvent(ctx, transaction, current.ID, domainrun.StatusStarting, domainrun.StatusRunning, ""); err != nil {
		return domainrun.Run{}, err
	}
	current.Status = domainrun.StatusRunning
	current.UpdatedAt = now
	if err := transaction.Commit(); err != nil {
		return domainrun.Run{}, fmt.Errorf("commit mark run running: %w", err)
	}
	return current, nil
}

// MarkFinalizing 在 Agent 退出后进入 Artifact、Result 和 Workspace 收尾阶段。
func (store *RunStore) MarkFinalizing(
	ctx context.Context,
	runID string,
	claimToken string,
	leaseGeneration int64,
) (domainrun.Run, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("begin mark run finalizing: %w", err)
	}
	defer transaction.Rollback()
	current, err := authorizeRun(ctx, transaction, runID, claimToken, leaseGeneration)
	if err != nil {
		return domainrun.Run{}, err
	}
	if current.Status != domainrun.StatusRunning {
		return domainrun.Run{}, ErrRunStateConflict
	}
	now := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE runs SET status = 'FINALIZING', updated_at = ?
WHERE id = ? AND status = 'RUNNING' AND lease_generation = ?`, formatTime(now), runID, leaseGeneration)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("mark run finalizing: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return domainrun.Run{}, err
	}
	if _, err := appendRunStatusEvent(ctx, transaction, current.ID, domainrun.StatusRunning, domainrun.StatusFinalizing, ""); err != nil {
		return domainrun.Run{}, err
	}
	current.Status = domainrun.StatusFinalizing
	current.UpdatedAt = now
	if err := transaction.Commit(); err != nil {
		return domainrun.Run{}, fmt.Errorf("commit mark run finalizing: %w", err)
	}
	return current, nil
}

// BeginFinalization 原子冻结预期终态并进入 FINALIZING。
func (store *RunStore) BeginFinalization(
	ctx context.Context,
	runID string,
	claimToken string,
	leaseGeneration int64,
	intent FinalizationIntent,
) (domainrun.Run, error) {
	payload, err := validateFinalizationIntent(intent)
	if err != nil {
		return domainrun.Run{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("begin run finalization: %w", err)
	}
	defer transaction.Rollback()
	current, err := authorizeRun(ctx, transaction, runID, claimToken, leaseGeneration)
	if err != nil {
		return domainrun.Run{}, err
	}
	if current.Status != domainrun.StatusRunning {
		return domainrun.Run{}, ErrRunStateConflict
	}
	if err := validateFinalizationSubject(current, intent); err != nil {
		return domainrun.Run{}, err
	}
	now := time.Now().UTC()
	_, err = transaction.ExecContext(ctx, `
INSERT INTO run_finalization_intents(
    run_id, terminal_status, exit_code, failure_kind, failure_code,
    failure_message, failure_retryable, clarification_json, planning_result_json, closure_json, task_reply, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, intent.Finish.Status, intent.Finish.ExitCode, intent.Finish.FailureKind,
		intent.Finish.FailureCode, intent.Finish.FailureMessage, nullableBool(intent.Finish.FailureRetryable),
		payload.ClarificationJSON, payload.PlanningJSON, payload.ClosureJSON, payload.TaskReply, formatTime(now))
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("record finalization intent: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE runs SET status = 'FINALIZING', updated_at = ?
WHERE id = ? AND status = 'RUNNING' AND lease_generation = ?`, formatTime(now), runID, leaseGeneration)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("mark run finalizing: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return domainrun.Run{}, err
	}
	if _, err := appendRunStatusEvent(ctx, transaction, runID, domainrun.StatusRunning, domainrun.StatusFinalizing, ""); err != nil {
		return domainrun.Run{}, err
	}
	current.Status, current.UpdatedAt = domainrun.StatusFinalizing, now
	if err := transaction.Commit(); err != nil {
		return domainrun.Run{}, fmt.Errorf("commit run finalization intent: %w", err)
	}
	return current, nil
}

// CompleteFinalization 使用当前 Runner fencing token 幂等提交已冻结的终态。
func (store *RunStore) CompleteFinalization(
	ctx context.Context,
	runID string,
	claimToken string,
	leaseGeneration int64,
) (domainrun.Run, error) {
	return store.completeFinalization(ctx, runID, claimToken, leaseGeneration, false)
}

// RecoverFinalization 仅供 Recovery Manager 在证明旧 Runner 已死亡后重放终态。
func (store *RunStore) RecoverFinalization(ctx context.Context, runID string, leaseGeneration int64) (domainrun.Run, error) {
	return store.completeFinalization(ctx, runID, "", leaseGeneration, true)
}

func (store *RunStore) completeFinalization(
	ctx context.Context,
	runID string,
	claimToken string,
	leaseGeneration int64,
	recovery bool,
) (domainrun.Run, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("begin complete finalization: %w", err)
	}
	defer transaction.Rollback()
	current, err := getRun(ctx, transaction, runID)
	if err != nil {
		return domainrun.Run{}, err
	}
	if recovery {
		if current.LeaseGeneration != leaseGeneration {
			return domainrun.Run{}, ErrRunClaimMismatch
		}
	} else if _, err := authorizeRun(ctx, transaction, runID, claimToken, leaseGeneration); err != nil {
		return domainrun.Run{}, err
	}
	intent, request, planning, closure, taskReply, completedAt, err := loadFinalizationIntent(ctx, transaction, runID)
	if err != nil {
		return domainrun.Run{}, err
	}
	if completedAt != "" {
		return current, nil
	}
	if current.Status != domainrun.StatusFinalizing {
		return domainrun.Run{}, ErrRunStateConflict
	}
	if request != nil {
		created, buildErr := buildClarification(current, *request)
		if buildErr != nil {
			return domainrun.Run{}, buildErr
		}
		if err := insertClarification(ctx, transaction, created); err != nil {
			return domainrun.Run{}, err
		}
	}
	if planning != nil {
		if err := applyPlanningResult(ctx, transaction, current, *planning); err != nil {
			return domainrun.Run{}, err
		}
	}
	if current.Purpose == domainrun.PurposeReview {
		if err := finalizeTaskFeedback(ctx, transaction, current, intent, taskReply); err != nil {
			return domainrun.Run{}, err
		}
	}
	if current.TaskID != "" && current.Purpose != domainrun.PurposeTriage && current.Purpose != domainrun.PurposeReview {
		if err := finishTaskRun(ctx, transaction, current, intent.Status); err != nil {
			return domainrun.Run{}, err
		}
	}
	now := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE runs SET status = ?, finished_at = ?, exit_code = ?, failure_kind = ?,
                failure_code = ?, failure_message = ?, failure_retryable = ?, updated_at = ?
WHERE id = ? AND status = 'FINALIZING' AND lease_generation = ?`,
		intent.Status, formatTime(now), intent.ExitCode, intent.FailureKind, intent.FailureCode,
		intent.FailureMessage, nullableBool(intent.FailureRetryable), formatTime(now), runID, leaseGeneration)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("complete run finalization: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return domainrun.Run{}, err
	}
	if err := clearOpenApprovals(ctx, transaction, runID, now); err != nil {
		return domainrun.Run{}, err
	}
	if _, err := appendRunStatusEvent(ctx, transaction, runID, domainrun.StatusFinalizing, intent.Status, intent.FailureCode); err != nil {
		return domainrun.Run{}, err
	}
	resolvedClosure, err := resolveRunClosure(current, intent, request, planning, closure, taskReply, now)
	if err != nil {
		return domainrun.Run{}, err
	}
	if err := insertRunClosure(ctx, transaction, resolvedClosure); err != nil {
		return domainrun.Run{}, err
	}
	if err := createRunSummary(ctx, transaction, string(intent.Status), runID, string(current.Purpose), closureProjectionSummary(resolvedClosure), now); err != nil {
		return domainrun.Run{}, err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE run_finalization_intents SET completed_at = ? WHERE run_id = ? AND completed_at IS NULL`, formatTime(now), runID); err != nil {
		return domainrun.Run{}, err
	}
	current.Status, current.FinishedAt, current.UpdatedAt = intent.Status, now, now
	current.ExitCode, current.FailureKind, current.FailureCode = &intent.ExitCode, intent.FailureKind, intent.FailureCode
	current.FailureMessage, current.FailureRetryable = intent.FailureMessage, intent.FailureRetryable
	if err := transaction.Commit(); err != nil {
		return domainrun.Run{}, fmt.Errorf("commit completed finalization: %w", err)
	}
	return current, nil
}

type finalizationPayload struct {
	ClarificationJSON any
	PlanningJSON      any
	ClosureJSON       any
	TaskReply         any
}

func validateFinalizationIntent(intent FinalizationIntent) (finalizationPayload, error) {
	if _, err := finishTaskCommand(intent.Finish.Status); err != nil {
		return finalizationPayload{}, err
	}
	payload := finalizationPayload{}
	taskReply := strings.TrimSpace(intent.TaskReply)
	if len([]rune(taskReply)) > 20000 {
		return finalizationPayload{}, errors.New("task reply must not exceed 20000 characters")
	}
	if taskReply != "" {
		payload.TaskReply = taskReply
	}
	if intent.Finish.Status == domainrun.StatusNeedsInput {
		if intent.Clarification == nil {
			return finalizationPayload{}, errors.New("needs-input finalization requires clarification")
		}
		request := intent.Clarification.Normalized()
		if err := request.Validate(); err != nil {
			return finalizationPayload{}, err
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			return finalizationPayload{}, err
		}
		payload.ClarificationJSON = string(encoded)
	}
	if intent.Finish.Status != domainrun.StatusNeedsInput && intent.Clarification != nil {
		return finalizationPayload{}, errors.New("terminal finalization cannot include clarification")
	}
	if intent.Planning != nil {
		normalized := intent.Planning.Normalized()
		if err := normalized.Validate(); err != nil {
			return finalizationPayload{}, err
		}
		encoded, err := json.Marshal(normalized)
		if err != nil {
			return finalizationPayload{}, err
		}
		payload.PlanningJSON = string(encoded)
	}
	if intent.Closure != nil {
		normalized := intent.Closure.Normalized()
		if err := normalized.Validate(); err != nil {
			return finalizationPayload{}, err
		}
		encoded, err := json.Marshal(normalized)
		if err != nil {
			return finalizationPayload{}, err
		}
		payload.ClosureJSON = string(encoded)
	}
	return payload, nil
}

func validateFinalizationSubject(current domainrun.Run, intent FinalizationIntent) error {
	if current.Purpose == domainrun.PurposePlanning {
		if intent.Finish.Status == domainrun.StatusSucceeded && intent.Planning == nil {
			return errors.New("successful planning finalization requires planning result")
		}
		if strings.TrimSpace(intent.TaskReply) != "" || intent.Closure != nil {
			return errors.New("planning run cannot write task reply")
		}
		return nil
	}
	if current.Purpose == domainrun.PurposeReview {
		if intent.Finish.Status == domainrun.StatusSucceeded && strings.TrimSpace(intent.TaskReply) == "" {
			return errors.New("successful review finalization requires task reply")
		}
		if intent.Clarification != nil || intent.Planning != nil || intent.Closure != nil {
			return errors.New("review run can only write task reply")
		}
		return nil
	}
	if intent.Planning != nil {
		return errors.New("non-planning run cannot write planning result")
	}
	if strings.TrimSpace(intent.TaskReply) != "" {
		return errors.New("non-review run cannot write task reply")
	}
	if current.Purpose == domainrun.PurposeImplementation || current.Purpose == domainrun.PurposeRevision {
		if intent.Finish.Status == domainrun.StatusSucceeded && intent.Closure == nil {
			return errors.New("successful task finalization requires closure")
		}
		if intent.Closure != nil && domainrun.StatusForClosure(*intent.Closure) != intent.Finish.Status {
			return errors.New("task closure stop_reason conflicts with terminal status")
		}
	} else if intent.Closure != nil {
		return errors.New("only implementation and revision runs can provide closure")
	}
	return nil
}

func loadFinalizationIntent(
	ctx context.Context,
	transaction *sql.Tx,
	runID string,
) (RunFinish, *clarification.Request, *plan.PlanningResult, *domainrun.Closure, string, string, error) {
	var finish RunFinish
	var retryable sql.NullBool
	var clarificationJSON, planningJSON, closureJSON, taskReply, completedAt sql.NullString
	err := transaction.QueryRowContext(ctx, `
SELECT terminal_status, exit_code, failure_kind, failure_code, failure_message,
       failure_retryable, clarification_json, planning_result_json, closure_json, task_reply, completed_at
FROM run_finalization_intents WHERE run_id = ?`, runID).Scan(
		&finish.Status, &finish.ExitCode, &finish.FailureKind, &finish.FailureCode,
		&finish.FailureMessage, &retryable, &clarificationJSON, &planningJSON, &closureJSON, &taskReply, &completedAt)
	if err != nil {
		return RunFinish{}, nil, nil, nil, "", "", err
	}
	finish.FailureRetryable = optionalBool(retryable)
	var request *clarification.Request
	if clarificationJSON.Valid {
		request = &clarification.Request{}
		if err := json.Unmarshal([]byte(clarificationJSON.String), request); err != nil {
			return RunFinish{}, nil, nil, nil, "", "", err
		}
	}
	var planning *plan.PlanningResult
	if planningJSON.Valid {
		planning = &plan.PlanningResult{}
		if err := json.Unmarshal([]byte(planningJSON.String), planning); err != nil {
			return RunFinish{}, nil, nil, nil, "", "", err
		}
	}
	var closure *domainrun.Closure
	if closureJSON.Valid {
		closure = &domainrun.Closure{}
		if err := json.Unmarshal([]byte(closureJSON.String), closure); err != nil {
			return RunFinish{}, nil, nil, nil, "", "", err
		}
	}
	return finish, request, planning, closure, taskReply.String, completedAt.String, nil
}

func resolveRunClosure(
	current domainrun.Run,
	finish RunFinish,
	request *clarification.Request,
	planning *plan.PlanningResult,
	explicit *domainrun.Closure,
	taskReply string,
	createdAt time.Time,
) (domainrun.Closure, error) {
	closure := derivedRunClosure(current.Purpose, finish, request, planning, taskReply)
	if explicit != nil {
		closure = explicit.Normalized()
	}
	closure.RunID = current.ID
	closure.CreatedAt = createdAt
	closure = closure.Normalized()
	if err := closure.Validate(); err != nil {
		return domainrun.Closure{}, fmt.Errorf("validate run closure: %w", err)
	}
	return closure, nil
}

func derivedRunClosure(
	purpose domainrun.Purpose,
	finish RunFinish,
	request *clarification.Request,
	planning *plan.PlanningResult,
	taskReply string,
) domainrun.Closure {
	if finish.Status == domainrun.StatusNeedsInput && request != nil {
		return domainrun.Closure{StopReason: domainrun.StopReasonNeedsInput, Summary: request.Question,
			Unverified: []string{request.Question}, NextAction: "等待人工回答后继续同一职责"}
	}
	if finish.Status == domainrun.StatusSucceeded {
		return successfulRunClosure(purpose, planning, taskReply)
	}
	reason := domainrun.StopReasonProcessFailed
	switch finish.Status {
	case domainrun.StatusCancelled:
		reason = domainrun.StopReasonCancelled
	case domainrun.StatusTimedOut:
		reason = domainrun.StopReasonTimedOut
	case domainrun.StatusLost:
		reason = domainrun.StopReasonLost
	}
	summary := strings.TrimSpace(finish.FailureMessage)
	if summary == "" {
		summary = string(purpose) + " Run 未完成，状态为 " + string(finish.Status)
	}
	return domainrun.Closure{StopReason: reason, Summary: summary,
		Unverified: []string{"当前 Run 未证明完成"}, NextAction: "检查失败原因后决定重试、修订或人工处理"}
}

func successfulRunClosure(purpose domainrun.Purpose, planning *plan.PlanningResult, taskReply string) domainrun.Closure {
	if purpose == domainrun.PurposePlanning && planning != nil {
		if planning.Plan == nil {
			return domainrun.Closure{StopReason: domainrun.StopReasonDiscussionRequired, Summary: planning.Reply,
				Completed: []string{"完成本轮需求分析"}, Unverified: append([]string(nil), planning.Readiness.OpenQuestions...),
				NextAction: "继续 Topic 讨论并解决未决问题"}
		}
		return domainrun.Closure{StopReason: domainrun.StopReasonGoalReached, Summary: planning.Reply,
			Completed: []string{"形成可审核的 Plan Revision"}, NextAction: "等待人工审核 Plan"}
	}
	if purpose == domainrun.PurposeReview {
		return domainrun.Closure{StopReason: domainrun.StopReasonGoalReached, Summary: strings.TrimSpace(taskReply),
			Completed: []string{"完成当前 Task 的只读分析与回复"}}
	}
	return domainrun.Closure{StopReason: domainrun.StopReasonGoalReached,
		Summary: string(purpose) + " Run 已完成", Completed: []string{"完成当前 Run 的结构化职责"}}
}

func insertRunClosure(ctx context.Context, transaction *sql.Tx, closure domainrun.Closure) error {
	completed, _ := json.Marshal(closure.Completed)
	verified, _ := json.Marshal(closure.Verified)
	unverified, _ := json.Marshal(closure.Unverified)
	risks, _ := json.Marshal(closure.RemainingRisks)
	_, err := transaction.ExecContext(ctx, `INSERT INTO run_closures(
run_id, stop_reason, summary, completed_json, verified_json, unverified_json,
remaining_risks_json, next_action, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, closure.RunID, closure.StopReason, closure.Summary,
		string(completed), string(verified), string(unverified), string(risks), closure.NextAction, formatTime(closure.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert run closure: %w", err)
	}
	return nil
}

func closureProjectionSummary(closure domainrun.Closure) string {
	parts := []string{closure.Summary}
	appendClosureSection := func(label string, values []string) {
		if len(values) > 0 {
			parts = append(parts, label+"："+strings.Join(values, "；"))
		}
	}
	appendClosureSection("已完成", closure.Completed)
	claims := make([]string, 0, len(closure.Verified))
	for _, item := range closure.Verified {
		claims = append(claims, item.Claim)
	}
	appendClosureSection("已验证", claims)
	appendClosureSection("未验证", closure.Unverified)
	appendClosureSection("剩余风险", closure.RemainingRisks)
	if closure.NextAction != "" {
		parts = append(parts, "下一步："+closure.NextAction)
	}
	return truncateRunes(strings.Join(parts, "\n"), 4000)
}

// GetClosure 返回 Run Finalization 保存的不可变收口报告。
func (store *RunStore) GetClosure(ctx context.Context, runID string) (domainrun.Closure, error) {
	var closure domainrun.Closure
	var completed, verified, unverified, risks, createdAt string
	err := store.database.QueryRowContext(ctx, `SELECT run_id, stop_reason, summary, completed_json,
verified_json, unverified_json, remaining_risks_json, next_action, created_at
FROM run_closures WHERE run_id = ?`, runID).Scan(&closure.RunID, &closure.StopReason, &closure.Summary,
		&completed, &verified, &unverified, &risks, &closure.NextAction, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domainrun.Closure{}, ErrRunClosureNotFound
	}
	if err != nil {
		return domainrun.Closure{}, fmt.Errorf("get run closure: %w", err)
	}
	if err := decodeRunClosure(&closure, completed, verified, unverified, risks, createdAt); err != nil {
		return domainrun.Closure{}, err
	}
	return closure, nil
}

func decodeRunClosure(closure *domainrun.Closure, completed, verified, unverified, risks, createdAt string) error {
	if err := json.Unmarshal([]byte(completed), &closure.Completed); err != nil {
		return fmt.Errorf("decode run closure completed: %w", err)
	}
	if err := json.Unmarshal([]byte(verified), &closure.Verified); err != nil {
		return fmt.Errorf("decode run closure verified: %w", err)
	}
	if err := json.Unmarshal([]byte(unverified), &closure.Unverified); err != nil {
		return fmt.Errorf("decode run closure unverified: %w", err)
	}
	if err := json.Unmarshal([]byte(risks), &closure.RemainingRisks); err != nil {
		return fmt.Errorf("decode run closure risks: %w", err)
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return fmt.Errorf("parse run closure time: %w", err)
	}
	closure.CreatedAt = parsed
	return nil
}

func applyPlanningResult(
	ctx context.Context,
	transaction *sql.Tx,
	current domainrun.Run,
	result plan.PlanningResult,
) error {
	result = result.Normalized()
	if err := result.Validate(); err != nil {
		return err
	}
	if _, err := appendAgentTopicMessage(ctx, transaction, current.TopicID, result.Reply); err != nil {
		return err
	}
	if result.Plan == nil {
		return nil
	}
	currentTopic, err := getTopic(ctx, transaction, current.TopicID)
	if err != nil {
		return err
	}
	if currentTopic.Status != topic.StatusOpen || currentTopic.Version != current.SubjectVersion {
		return nil
	}
	currentPlan, creating, err := findOrCreatePlan(ctx, transaction, current.TopicID)
	if err != nil {
		return err
	}
	if !creating && currentPlan.Status != plan.StatusChangesRequested {
		return ErrPlanConflict
	}
	revisionNumber := int64(1)
	if !creating {
		previous, loadErr := loadPlanRevision(ctx, transaction, currentPlan.CurrentRevisionID)
		if loadErr != nil {
			return loadErr
		}
		revisionNumber = previous.Revision + 1
	}
	input := result.Plan.Normalized()
	input.SourceRunID = current.ID
	revision, err := buildPlanRevision(currentPlan, revisionNumber, input)
	if err != nil {
		return err
	}
	revision.Readiness = result.Readiness
	if creating {
		currentPlan.CurrentRevisionID = revision.ID
		if err := insertPlan(ctx, transaction, currentPlan); err != nil {
			return err
		}
	} else if err := activatePlanRevision(ctx, transaction, &currentPlan, revision.ID); err != nil {
		return err
	}
	if err := insertPlanRevision(ctx, transaction, revision); err != nil {
		return err
	}
	updatedTopic, event, err := prepareTopicTransition(currentTopic, topic.StatusPlanReview, topic.CommandSubmitPlan)
	if err != nil {
		return err
	}
	updatedTopic.CurrentPlanID = currentPlan.ID
	if err := updateTopicForPlan(ctx, transaction, currentTopic, updatedTopic); err != nil {
		return err
	}
	return insertTopicEvent(ctx, transaction, event)
}

// Finish 原子写入 Run 终态并迁移对应 Task 状态。
func (store *RunStore) Finish(
	ctx context.Context,
	runID string,
	claimToken string,
	leaseGeneration int64,
	finish RunFinish,
) (domainrun.Run, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("begin run finish: %w", err)
	}
	defer transaction.Rollback()
	current, err := authorizeRun(ctx, transaction, runID, claimToken, leaseGeneration)
	if err != nil {
		return domainrun.Run{}, err
	}
	if current.Status != domainrun.StatusRunning && current.Status != domainrun.StatusStarting && current.Status != domainrun.StatusClaimed && current.Status != domainrun.StatusFinalizing {
		return domainrun.Run{}, ErrRunStateConflict
	}
	if current.Purpose == domainrun.PurposeReview {
		if err := failTaskFeedback(ctx, transaction, current.ID, finish.FailureMessage); err != nil {
			return domainrun.Run{}, err
		}
	}
	if current.TaskID != "" && current.Purpose != domainrun.PurposeTriage && current.Purpose != domainrun.PurposeReview {
		if err := finishTaskRun(ctx, transaction, current, finish.Status); err != nil {
			return domainrun.Run{}, err
		}
	}
	now := time.Now().UTC()
	retryable := nullableBool(finish.FailureRetryable)
	result, err := transaction.ExecContext(ctx, `
UPDATE runs SET status = ?, finished_at = ?, exit_code = ?, failure_kind = ?,
                failure_code = ?, failure_message = ?, failure_retryable = ?, updated_at = ?
WHERE id = ? AND status IN ('CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING') AND lease_generation = ?`,
		finish.Status, formatTime(now), finish.ExitCode, finish.FailureKind,
		finish.FailureCode, finish.FailureMessage, retryable, formatTime(now), runID, leaseGeneration,
	)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("finish run: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return domainrun.Run{}, err
	}
	if _, err := appendRunStatusEvent(ctx, transaction, current.ID, current.Status, finish.Status, finish.FailureCode); err != nil {
		return domainrun.Run{}, err
	}
	closure, err := resolveRunClosure(current, finish, nil, nil, nil, "", now)
	if err != nil {
		return domainrun.Run{}, err
	}
	if err := insertRunClosure(ctx, transaction, closure); err != nil {
		return domainrun.Run{}, err
	}
	if err := createRunSummary(ctx, transaction, string(finish.Status), runID, string(current.Purpose), closureProjectionSummary(closure), now); err != nil {
		return domainrun.Run{}, err
	}
	current.Status = finish.Status
	exitCode := finish.ExitCode
	current.ExitCode = &exitCode
	current.FailureKind = finish.FailureKind
	current.FailureCode = finish.FailureCode
	current.FailureMessage = finish.FailureMessage
	current.FailureRetryable = finish.FailureRetryable
	current.FinishedAt = now
	current.UpdatedAt = now
	if err := transaction.Commit(); err != nil {
		return domainrun.Run{}, fmt.Errorf("commit run finish: %w", err)
	}
	return current, nil
}

// RecordWorkspaceSnapshot 首次保存 Run Finalization 观察到的 Workspace 前后状态；重复调用返回原始事实。
func (store *RunStore) RecordWorkspaceSnapshot(
	ctx context.Context,
	snapshot domainrun.WorkspaceSnapshot,
) (domainrun.WorkspaceSnapshot, error) {
	if snapshot.RunID == "" || snapshot.WorkspaceID == "" || snapshot.HeadBefore == "" || snapshot.HeadAfter == "" {
		return domainrun.WorkspaceSnapshot{}, errors.New("invalid run workspace snapshot")
	}
	snapshot.CapturedAt = time.Now().UTC()
	_, err := store.database.ExecContext(ctx, `
INSERT INTO run_workspace_snapshots(
    run_id, workspace_id, branch_name, target_branch, base_commit_sha,
    head_before, head_after, dirty_before, dirty_after, state_after, captured_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO NOTHING`,
		snapshot.RunID, snapshot.WorkspaceID, snapshot.BranchName, snapshot.TargetBranch,
		snapshot.BaseCommitSHA, snapshot.HeadBefore, snapshot.HeadAfter,
		boolInt(snapshot.DirtyBefore), boolInt(snapshot.DirtyAfter), snapshot.StateAfter,
		formatTime(snapshot.CapturedAt),
	)
	if err != nil {
		return domainrun.WorkspaceSnapshot{}, fmt.Errorf("record run workspace snapshot: %w", err)
	}
	return store.GetWorkspaceSnapshot(ctx, snapshot.RunID)
}

// GetWorkspaceSnapshot 返回 Run 已完成的 Workspace Finalization 快照。
func (store *RunStore) GetWorkspaceSnapshot(ctx context.Context, runID string) (domainrun.WorkspaceSnapshot, error) {
	var snapshot domainrun.WorkspaceSnapshot
	var dirtyBefore, dirtyAfter int
	var capturedAt string
	err := store.database.QueryRowContext(ctx, `
SELECT run_id, workspace_id, branch_name, target_branch, base_commit_sha,
       head_before, head_after, dirty_before, dirty_after, state_after, captured_at
FROM run_workspace_snapshots WHERE run_id = ?`, runID).Scan(
		&snapshot.RunID, &snapshot.WorkspaceID, &snapshot.BranchName, &snapshot.TargetBranch,
		&snapshot.BaseCommitSHA, &snapshot.HeadBefore, &snapshot.HeadAfter,
		&dirtyBefore, &dirtyAfter, &snapshot.StateAfter, &capturedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domainrun.WorkspaceSnapshot{}, ErrRunWorkspaceSnapshotNotFound
	}
	if err != nil {
		return domainrun.WorkspaceSnapshot{}, fmt.Errorf("get run workspace snapshot: %w", err)
	}
	snapshot.DirtyBefore = dirtyBefore == 1
	snapshot.DirtyAfter = dirtyAfter == 1
	snapshot.CapturedAt, err = parseTime(capturedAt)
	return snapshot, err
}

func finishTaskRun(
	ctx context.Context,
	transaction *sql.Tx,
	current domainrun.Run,
	status domainrun.Status,
) error {
	command, err := finishTaskCommand(status)
	if err != nil {
		return err
	}
	currentTask, err := getTask(ctx, transaction, current.TaskID)
	if err != nil {
		return err
	}
	nextStatus, err := task.Transition(currentTask.Status, command)
	if err != nil {
		return err
	}
	updated, event, err := prepareTransition(currentTask, nextStatus, command)
	if err != nil {
		return err
	}
	if err := updateTaskStatus(ctx, transaction, currentTask, updated); err != nil {
		return err
	}
	return insertTaskEvent(ctx, transaction, event)
}

func authorizeRun(
	ctx context.Context,
	transaction *sql.Tx,
	runID string,
	claimToken string,
	leaseGeneration int64,
) (domainrun.Run, error) {
	current, err := getRun(ctx, transaction, runID)
	if err != nil {
		return domainrun.Run{}, err
	}
	var storedHash string
	if err := transaction.QueryRowContext(ctx, `SELECT claim_token_hash FROM runs WHERE id = ?`, runID).Scan(&storedHash); err != nil {
		return domainrun.Run{}, fmt.Errorf("read run claim hash: %w", err)
	}
	hash := sha256.Sum256([]byte(claimToken))
	computedHash := hex.EncodeToString(hash[:])
	if subtle.ConstantTimeCompare([]byte(storedHash), []byte(computedHash)) != 1 || current.LeaseGeneration != leaseGeneration {
		return domainrun.Run{}, ErrRunClaimMismatch
	}
	return current, nil
}

func finishTaskCommand(status domainrun.Status) (task.Command, error) {
	switch status {
	case domainrun.StatusSucceeded:
		return task.CommandRunSucceeded, nil
	case domainrun.StatusCancelled:
		return task.CommandCancelRun, nil
	case domainrun.StatusFailed, domainrun.StatusTimedOut, domainrun.StatusLost:
		return task.CommandRunFailed, nil
	case domainrun.StatusNeedsInput:
		return task.CommandNeedsInput, nil
	default:
		return "", errors.New("invalid terminal run status")
	}
}

func nullableBool(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalBool(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	result := value.Bool
	return &result
}

func intPointer(value int) *int { return &value }

func clearOpenApprovals(ctx context.Context, transaction *sql.Tx, runID string, now time.Time) error {
	rows, err := transaction.QueryContext(ctx, `SELECT id FROM approval_requests WHERE run_id = ? AND status = 'OPEN'`, runID)
	if err != nil {
		return fmt.Errorf("list open approvals for finalization: %w", err)
	}
	ids := make([]string, 0, 1)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE approval_requests SET status = 'CLEARED', version = version + 1,
    updated_at = ?, resolved_at = ? WHERE run_id = ? AND status = 'OPEN'`,
		formatTime(now), formatTime(now), runID); err != nil {
		return fmt.Errorf("clear open approvals for finalization: %w", err)
	}
	for _, id := range ids {
		if _, err := appendRunEvent(ctx, transaction, runID, domainrun.EventApprovalResolve, map[string]any{
			"schema_version": 1, "approval_request_id": id, "status": "CLEARED",
		}); err != nil {
			return err
		}
	}
	return nil
}

func requireSingleChange(result sql.Result) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read run update result: %w", err)
	}
	if changed != 1 {
		return ErrRunStateConflict
	}
	return nil
}

func appendRunStatusEvent(
	ctx context.Context,
	transaction *sql.Tx,
	runID string,
	from domainrun.Status,
	to domainrun.Status,
	failureCode string,
) (domainrun.Event, error) {
	payload := map[string]any{"schema_version": 1, "from": from, "to": to}
	if failureCode != "" {
		payload["failure_code"] = failureCode
	}
	return appendRunEvent(ctx, transaction, runID, domainrun.EventStatusChanged, payload)
}

func appendRunEvent(
	ctx context.Context,
	transaction *sql.Tx,
	runID string,
	eventType domainrun.EventType,
	payloadValue any,
) (domainrun.Event, error) {
	if runID == "" || eventType == "" {
		return domainrun.Event{}, errors.New("run event identity is required")
	}
	payload, err := json.Marshal(payloadValue)
	if err != nil {
		return domainrun.Event{}, fmt.Errorf("encode run event payload: %w", err)
	}
	var sequence int64
	if err := transaction.QueryRowContext(ctx, `
SELECT COALESCE(MAX(sequence), 0) + 1 FROM run_events WHERE run_id = ?`, runID).Scan(&sequence); err != nil {
		return domainrun.Event{}, fmt.Errorf("next run event sequence: %w", err)
	}
	eventID, err := newID()
	if err != nil {
		return domainrun.Event{}, err
	}
	now := time.Now().UTC()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO run_events(id, run_id, sequence, event_type, payload_json, occurred_at)
VALUES (?, ?, ?, ?, ?, ?)`, eventID, runID, sequence, eventType, string(payload), formatTime(now)); err != nil {
		return domainrun.Event{}, fmt.Errorf("insert run event: %w", err)
	}
	return domainrun.Event{
		ID: eventID, RunID: runID, Sequence: sequence, Type: eventType,
		Payload: payload, OccurredAt: now,
	}, nil
}

type runnableWork struct {
	Topic               topic.Topic
	Task                task.Task
	ProfileRevisionID   string
	Purpose             domainrun.Purpose
	RetryOfRunID        string
	ContinuationOfRunID string
	FeedbackID          string
}

func ensureRunCapacity(ctx context.Context, transaction *sql.Tx, maxWorkers int) error {
	var active int
	err := transaction.QueryRowContext(ctx, `
SELECT COUNT(*) FROM runs
WHERE status IN ('CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING')`).Scan(&active)
	if err != nil {
		return fmt.Errorf("count active runs: %w", err)
	}
	if active >= maxWorkers {
		return ErrRunCapacityReached
	}
	return nil
}

func selectRunnableWork(ctx context.Context, transaction *sql.Tx) (runnableWork, error) {
	if selected, err := selectRevisionTask(ctx, transaction); !errors.Is(err, ErrNoRunnableTask) {
		return selected, err
	}
	if selected, err := selectReviewFeedback(ctx, transaction); !errors.Is(err, ErrNoRunnableTask) {
		return selected, err
	}
	if selected, err := selectPlanningTopic(ctx, transaction); !errors.Is(err, ErrNoRunnableTask) {
		return selected, err
	}
	if selected, err := selectTriageTask(ctx, transaction); !errors.Is(err, ErrNoRunnableTask) {
		return selected, err
	}
	return selectImplementationTask(ctx, transaction)
}

func selectRevisionTask(ctx context.Context, transaction *sql.Tx) (runnableWork, error) {
	return selectTaskForPurpose(ctx, transaction, `
SELECT t.id, r.id, 'REVISION', COALESCE((
    SELECT c.source_run_id FROM clarifications c
    WHERE c.task_id = t.id AND c.status = 'ANSWERED'
      AND c.continuation_purpose = 'REVISION' AND c.continuation_run_id IS NULL
    ORDER BY c.answered_at ASC LIMIT 1
), '')
FROM tasks t
JOIN project_agent_defaults d ON d.purpose = 'REVISION'
JOIN agent_profiles p ON p.id = d.profile_id
JOIN agent_profile_revisions r ON r.id = p.current_revision_id
WHERE t.status = 'CHANGES_REQUESTED' AND length(trim(r.command)) > 0
ORDER BY t.priority ASC, t.created_at ASC, t.id ASC LIMIT 1`)
}

func selectReviewFeedback(ctx context.Context, transaction *sql.Tx) (runnableWork, error) {
	var selected runnableWork
	var taskID string
	err := transaction.QueryRowContext(ctx, `
SELECT f.id, f.task_id, r.id, COALESCE(previous.run_id, '')
FROM task_feedback_turns f
LEFT JOIN task_feedback_turns previous ON previous.id = f.retry_of_feedback_id
JOIN tasks t ON t.id = f.task_id
JOIN project_agent_defaults d ON d.purpose = 'REVIEW'
JOIN agent_profiles p ON p.id = d.profile_id
JOIN agent_profile_revisions r ON r.id = p.current_revision_id
WHERE f.intent = 'DISCUSS' AND f.status = 'QUEUED' AND length(trim(r.command)) > 0
  AND NOT EXISTS (
      SELECT 1 FROM runs active
      WHERE active.task_id = t.id
        AND active.status IN ('CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING')
  )
	ORDER BY f.created_at ASC, f.id ASC LIMIT 1`).Scan(
		&selected.FeedbackID, &taskID, &selected.ProfileRevisionID, &selected.RetryOfRunID)
	if errors.Is(err, sql.ErrNoRows) {
		return runnableWork{}, ErrNoRunnableTask
	}
	if err != nil {
		return runnableWork{}, fmt.Errorf("select task review feedback: %w", err)
	}
	selected.Task, err = getTask(ctx, transaction, taskID)
	if err != nil {
		return runnableWork{}, err
	}
	selected.Purpose = domainrun.PurposeReview
	return selected, nil
}

func selectPlanningTopic(ctx context.Context, transaction *sql.Tx) (runnableWork, error) {
	var selected runnableWork
	var topicID string
	err := transaction.QueryRowContext(ctx, `
SELECT t.id, r.id, COALESCE((
    SELECT previous.id FROM runs previous
    WHERE previous.topic_id = t.id AND previous.purpose = 'PLANNING'
    ORDER BY previous.created_at DESC, previous.id DESC LIMIT 1
), '')
FROM topics t
JOIN project_agent_defaults d ON d.purpose = 'PLANNING'
JOIN agent_profiles p ON p.id = d.profile_id
JOIN agent_profile_revisions r ON r.id = p.current_revision_id
WHERE t.status = 'OPEN' AND length(trim(r.command)) > 0
  AND NOT EXISTS (
      SELECT 1 FROM runs attempted
      WHERE attempted.topic_id = t.id AND attempted.purpose = 'PLANNING'
        AND attempted.subject_version = t.version
  )
  AND NOT EXISTS (
      SELECT 1 FROM runs active
      WHERE active.topic_id = t.id
        AND active.status IN ('CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING')
  )
ORDER BY t.updated_at ASC, t.created_at ASC, t.id ASC LIMIT 1`).Scan(
		&topicID, &selected.ProfileRevisionID, &selected.ContinuationOfRunID)
	if errors.Is(err, sql.ErrNoRows) {
		return runnableWork{}, ErrNoRunnableTask
	}
	if err != nil {
		return runnableWork{}, fmt.Errorf("select planning topic: %w", err)
	}
	selected.Topic, err = getTopic(ctx, transaction, topicID)
	if err != nil {
		return runnableWork{}, err
	}
	selected.Purpose = domainrun.PurposePlanning
	return selected, nil
}

func selectTriageTask(ctx context.Context, transaction *sql.Tx) (runnableWork, error) {
	return selectTaskForPurpose(ctx, transaction, `
SELECT t.id, r.id, 'TRIAGE', COALESCE((
    SELECT c.source_run_id FROM clarifications c
    WHERE c.task_id = t.id AND c.status = 'ANSWERED'
      AND c.continuation_purpose = 'TRIAGE' AND c.continuation_run_id IS NULL
    ORDER BY c.answered_at ASC LIMIT 1
), '')
FROM tasks t
JOIN project_agent_defaults d ON d.purpose = 'TRIAGE'
JOIN agent_profiles p ON p.id = d.profile_id
JOIN agent_profile_revisions r ON r.id = p.current_revision_id
WHERE t.status = 'READY' AND length(trim(r.command)) > 0
  AND (
      EXISTS (
          SELECT 1 FROM clarifications c
          WHERE c.task_id = t.id AND c.status = 'ANSWERED'
            AND c.continuation_purpose = 'TRIAGE' AND c.continuation_run_id IS NULL
      )
      OR (
          NOT EXISTS (
              SELECT 1 FROM task_assessments a
              WHERE a.task_id = t.id AND a.task_assessment_version = t.assessment_input_version
          )
          AND NOT EXISTS (
              SELECT 1 FROM runs attempted
              WHERE attempted.task_id = t.id AND attempted.purpose = 'TRIAGE'
                AND attempted.subject_version = t.assessment_input_version
          )
      )
  )
ORDER BY t.priority ASC, t.created_at ASC, t.id ASC LIMIT 1`)
}

func selectImplementationTask(ctx context.Context, transaction *sql.Tx) (runnableWork, error) {
	return selectTaskForPurpose(ctx, transaction, `
SELECT t.id, r.id, 'IMPLEMENTATION', COALESCE((
    SELECT c.source_run_id FROM clarifications c
    WHERE c.task_id = t.id AND c.status = 'ANSWERED'
      AND c.continuation_purpose = 'IMPLEMENTATION' AND c.continuation_run_id IS NULL
    ORDER BY c.answered_at ASC LIMIT 1
), '')
FROM tasks t
JOIN project_agent_defaults d ON d.purpose = 'IMPLEMENTATION'
JOIN agent_profiles p ON p.id = d.profile_id
JOIN agent_profile_revisions r ON r.id = p.current_revision_id
WHERE t.status = 'READY' AND length(trim(r.command)) > 0
  AND (
      EXISTS (
          SELECT 1 FROM task_assessments a
          WHERE a.task_id = t.id AND a.task_assessment_version = t.assessment_input_version
      )
      OR EXISTS (
          SELECT 1 FROM runs attempted
          WHERE attempted.task_id = t.id AND attempted.purpose = 'TRIAGE'
            AND attempted.subject_version = t.assessment_input_version
            AND attempted.status IN ('SUCCEEDED', 'FAILED', 'CANCELLED', 'TIMED_OUT', 'LOST')
      )
      OR NOT EXISTS (
          SELECT 1
          FROM project_agent_defaults td
          JOIN agent_profiles tp ON tp.id = td.profile_id
          JOIN agent_profile_revisions tr ON tr.id = tp.current_revision_id
          WHERE td.purpose = 'TRIAGE' AND length(trim(tr.command)) > 0
      )
  )
  AND NOT EXISTS (
      SELECT 1 FROM runs active
      WHERE active.task_id = t.id
        AND active.status IN ('CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING')
  )
ORDER BY t.priority ASC, t.created_at ASC, t.id ASC LIMIT 1`)
}

func selectTaskForPurpose(ctx context.Context, transaction *sql.Tx, query string) (runnableWork, error) {
	var selected runnableWork
	var taskID string
	var purpose string
	err := transaction.QueryRowContext(ctx, query).Scan(
		&taskID, &selected.ProfileRevisionID, &purpose, &selected.ContinuationOfRunID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return runnableWork{}, ErrNoRunnableTask
	}
	if err != nil {
		return runnableWork{}, fmt.Errorf("select runnable task: %w", err)
	}
	selected.Task, err = getTask(ctx, transaction, taskID)
	if err != nil {
		return runnableWork{}, err
	}
	selected.Purpose = domainrun.Purpose(purpose)
	return selected, nil
}

func createClaim(selected runnableWork, leaseDuration time.Duration) (domainrun.Claim, error) {
	runID, err := newID()
	if err != nil {
		return domainrun.Claim{}, err
	}
	token, err := randomHex(32)
	if err != nil {
		return domainrun.Claim{}, err
	}
	nonce, err := randomHex(16)
	if err != nil {
		return domainrun.Claim{}, err
	}
	now := time.Now().UTC()
	return domainrun.Claim{
		ClaimToken: token,
		Run: domainrun.Run{
			ID: runID, Purpose: selected.Purpose, TopicID: selected.Topic.ID, TaskID: selected.Task.ID,
			Status: domainrun.StatusClaimed, ProfileRevisionID: selected.ProfileRevisionID,
			RetryOfRunID:        selected.RetryOfRunID,
			ContinuationOfRunID: selected.ContinuationOfRunID,
			SubjectVersion:      subjectVersion(selected),
			LeaseGeneration:     1, LeaseExpiresAt: now.Add(leaseDuration),
			RunNonce: nonce,
			QueuedAt: now, ClaimedAt: now, CreatedAt: now, UpdatedAt: now,
		},
	}, nil
}

func subjectVersion(selected runnableWork) int64 {
	if selected.Purpose == domainrun.PurposePlanning {
		return selected.Topic.Version
	}
	if selected.Purpose == domainrun.PurposeTriage {
		return selected.Task.AssessmentInputVersion
	}
	return selected.Task.Version
}

func insertClaimedRun(ctx context.Context, transaction *sql.Tx, claim domainrun.Claim) error {
	hash := sha256.Sum256([]byte(claim.ClaimToken))
	_, err := transaction.ExecContext(ctx, `
INSERT INTO runs(
    id, purpose, topic_id, task_id, status, profile_revision_id, retry_of_run_id,
    continuation_of_run_id, claim_token_hash, lease_generation, lease_expires_at, run_nonce,
    queued_at, claimed_at, created_at, updated_at, subject_version,
    agent_session_id, session_resumed
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		claim.Run.ID, claim.Run.Purpose, nullableString(claim.Run.TopicID), nullableString(claim.Run.TaskID), claim.Run.Status,
		claim.Run.ProfileRevisionID, nullableString(claim.Run.RetryOfRunID), nullableString(claim.Run.ContinuationOfRunID),
		hex.EncodeToString(hash[:]), claim.Run.LeaseGeneration,
		formatTime(claim.Run.LeaseExpiresAt), claim.Run.RunNonce, formatTime(claim.Run.QueuedAt),
		formatTime(claim.Run.ClaimedAt), formatTime(claim.Run.CreatedAt), formatTime(claim.Run.UpdatedAt),
		claim.Run.SubjectVersion, nullableString(claim.Run.AgentSessionID), claim.Run.SessionResumed,
	)
	if err != nil {
		return fmt.Errorf("insert claimed run: %w", err)
	}
	if claim.Run.ContinuationOfRunID != "" {
		result, updateErr := transaction.ExecContext(ctx, `
UPDATE clarifications SET continuation_run_id = ?, updated_at = ?
WHERE source_run_id = ? AND status = 'ANSWERED' AND continuation_run_id IS NULL`,
			claim.Run.ID, formatTime(claim.Run.CreatedAt), claim.Run.ContinuationOfRunID)
		if updateErr != nil {
			return fmt.Errorf("link continuation clarification: %w", updateErr)
		}
		if claim.Run.TaskID != "" {
			if updateErr := requireClarificationChange(result); updateErr != nil {
				return updateErr
			}
		} else if changed, changeErr := result.RowsAffected(); changeErr != nil {
			return fmt.Errorf("read topic clarification link result: %w", changeErr)
		} else if changed > 1 {
			return errors.New("multiple topic clarifications matched one continuation")
		}
	}
	return nil
}

func createRunToolPolicySnapshot(
	ctx context.Context,
	transaction *sql.Tx,
	runID string,
	profileRevisionID string,
) error {
	snapshot := capability.ToolPolicySnapshot{
		ProfileRevisionID: profileRevisionID,
		Skills:            []capability.SkillSnapshot{},
		MCPServers:        []capability.MCPServerSnapshot{},
	}
	skillRows, err := transaction.QueryContext(ctx, `
SELECT s.id, s.name, s.source_path, s.content_sha256, s.version, s.enabled, binding.required
FROM agent_profile_revision_skills binding
JOIN project_skills s ON s.id = binding.skill_id
WHERE binding.profile_revision_id = ? ORDER BY s.id`, profileRevisionID)
	if err != nil {
		return fmt.Errorf("resolve run skills: %w", err)
	}
	for skillRows.Next() {
		var item capability.SkillSnapshot
		if err := skillRows.Scan(
			&item.ID, &item.Name, &item.SourcePath, &item.ContentSHA256,
			&item.CatalogVersion, &item.Enabled, &item.Required,
		); err != nil {
			skillRows.Close()
			return err
		}
		snapshot.Skills = append(snapshot.Skills, item)
	}
	if err := skillRows.Err(); err != nil {
		skillRows.Close()
		return err
	}
	if err := skillRows.Close(); err != nil {
		return err
	}
	mcpRows, err := transaction.QueryContext(ctx, `
SELECT server.id, server.name, server.config_name, server.version, server.enabled,
       binding.required, binding.enabled_tools_json
FROM agent_profile_revision_mcp_servers binding
JOIN project_mcp_servers server ON server.id = binding.mcp_server_id
WHERE binding.profile_revision_id = ? ORDER BY server.id`, profileRevisionID)
	if err != nil {
		return fmt.Errorf("resolve run MCP servers: %w", err)
	}
	for mcpRows.Next() {
		var item capability.MCPServerSnapshot
		var toolsJSON string
		if err := mcpRows.Scan(
			&item.ID, &item.Name, &item.ConfigName, &item.CatalogVersion,
			&item.Enabled, &item.Required, &toolsJSON,
		); err != nil {
			mcpRows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(toolsJSON), &item.EnabledTools); err != nil {
			mcpRows.Close()
			return fmt.Errorf("decode run MCP tools: %w", err)
		}
		snapshot.MCPServers = append(snapshot.MCPServers, item)
	}
	if err := mcpRows.Err(); err != nil {
		mcpRows.Close()
		return err
	}
	if err := mcpRows.Close(); err != nil {
		return err
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode run tool policy snapshot: %w", err)
	}
	hash := sha256.Sum256(encoded)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO run_tool_policy_snapshots(run_id, snapshot_json, sha256, created_at)
VALUES (?, ?, ?, ?)`, runID, string(encoded), hex.EncodeToString(hash[:]), formatTime(time.Now().UTC())); err != nil {
		return fmt.Errorf("insert run tool policy snapshot: %w", err)
	}
	return nil
}

func claimTask(ctx context.Context, transaction *sql.Tx, selected runnableWork, claimed domainrun.Run) error {
	updated, event, err := prepareTransition(selected.Task, task.StatusRunning, task.CommandClaimRun)
	if err != nil {
		return err
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE tasks SET status = ?, latest_run_id = ?, version = ?, updated_at = ?
WHERE id = ? AND version = ? AND status = ?`,
		updated.Status, claimed.ID, updated.Version, formatTime(updated.UpdatedAt),
		selected.Task.ID, selected.Task.Version, selected.Task.Status,
	)
	if err != nil {
		return fmt.Errorf("claim task for run: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read task claim result: %w", err)
	}
	if changed != 1 {
		return ErrTaskVersionConflict
	}
	return insertTaskEvent(ctx, transaction, event)
}

func scanRun(scanner rowScanner) (domainrun.Run, error) {
	var item domainrun.Run
	var exitCode sql.NullInt64
	var failureRetryable sql.NullBool
	var leaseExpiresAt, queuedAt, claimedAt, startedAt, finishedAt, createdAt, updatedAt, cancelRequestedAt string
	err := scanner.Scan(
		&item.ID, &item.Purpose, &item.TopicID, &item.TaskID, &item.Status,
		&item.ProfileRevisionID, &item.RetryOfRunID, &item.ContinuationOfRunID, &item.LeaseGeneration,
		&leaseExpiresAt, &queuedAt, &claimedAt, &startedAt, &finishedAt, &createdAt, &updatedAt,
		&item.SubjectVersion, &exitCode, &item.FailureKind, &item.FailureCode,
		&item.FailureMessage, &failureRetryable, &cancelRequestedAt, &item.CancelReason,
		&item.AgentSessionID, &item.SessionResumed, &item.RunNonce,
	)
	if err != nil {
		return domainrun.Run{}, err
	}
	if exitCode.Valid {
		value := int(exitCode.Int64)
		item.ExitCode = &value
	}
	if failureRetryable.Valid {
		value := failureRetryable.Bool
		item.FailureRetryable = &value
	}
	if cancelRequestedAt != "" {
		value, parseErr := parseTime(cancelRequestedAt)
		if parseErr != nil {
			return domainrun.Run{}, fmt.Errorf("parse run cancellation time: %w", parseErr)
		}
		item.CancelRequestedAt = &value
	}
	fields := []struct {
		value  string
		target *time.Time
	}{
		{leaseExpiresAt, &item.LeaseExpiresAt}, {queuedAt, &item.QueuedAt},
		{claimedAt, &item.ClaimedAt}, {startedAt, &item.StartedAt},
		{finishedAt, &item.FinishedAt}, {createdAt, &item.CreatedAt}, {updatedAt, &item.UpdatedAt},
	}
	for _, field := range fields {
		if field.value == "" {
			continue
		}
		parsed, parseErr := parseTime(field.value)
		if parseErr != nil {
			return domainrun.Run{}, fmt.Errorf("parse run time: %w", parseErr)
		}
		*field.target = parsed
	}
	return item, nil
}

func getRun(ctx context.Context, queryer rowQueryer, runID string) (domainrun.Run, error) {
	item, err := scanRun(queryer.QueryRowContext(ctx, "SELECT "+runColumns+" FROM runs WHERE id = ?", runID))
	if errors.Is(err, sql.ErrNoRows) {
		return domainrun.Run{}, ErrRunNotFound
	}
	return item, err
}

func randomHex(byteCount int) (string, error) {
	bytes := make([]byte, byteCount)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
