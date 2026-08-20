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
	domainrun "github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
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

const runColumns = `
id, purpose, COALESCE(topic_id, ''), COALESCE(task_id, ''), status,
profile_revision_id, COALESCE(retry_of_run_id, ''), COALESCE(continuation_of_run_id, ''), lease_generation,
lease_expires_at, queued_at, claimed_at,
COALESCE(started_at, ''), COALESCE(finished_at, ''), created_at, updated_at,
subject_version`

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
	if (current.Status != domainrun.StatusRunning && current.Status != domainrun.StatusStarting) ||
		(current.Purpose != domainrun.PurposeImplementation && current.Purpose != domainrun.PurposeRevision) {
		return domainrun.Run{}, clarification.Clarification{}, ErrRunStateConflict
	}
	created, err := buildClarification(current, request)
	if err != nil {
		return domainrun.Run{}, clarification.Clarification{}, err
	}
	if err := insertClarification(ctx, transaction, created); err != nil {
		return domainrun.Run{}, clarification.Clarification{}, err
	}
	if err := finishTaskRun(ctx, transaction, current, domainrun.StatusNeedsInput); err != nil {
		return domainrun.Run{}, clarification.Clarification{}, err
	}
	now := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE runs SET status = 'NEEDS_INPUT', finished_at = ?, exit_code = 0, updated_at = ?
WHERE id = ? AND status IN ('STARTING', 'RUNNING') AND lease_generation = ?`,
		formatTime(now), formatTime(now), runID, leaseGeneration)
	if err != nil {
		return domainrun.Run{}, clarification.Clarification{}, fmt.Errorf("finish run for clarification: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return domainrun.Run{}, clarification.Clarification{}, err
	}
	current.Status = domainrun.StatusNeedsInput
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
	selected, err := selectRunnableTask(ctx, transaction)
	if err != nil {
		return domainrun.Claim{}, err
	}
	claim, err := createClaim(selected, leaseDuration)
	if err != nil {
		return domainrun.Claim{}, err
	}
	if err := insertClaimedRun(ctx, transaction, claim); err != nil {
		return domainrun.Claim{}, err
	}
	if err := createRunToolPolicySnapshot(ctx, transaction, claim.Run.ID, claim.Run.ProfileRevisionID); err != nil {
		return domainrun.Claim{}, err
	}
	if selected.Purpose != domainrun.PurposeTriage {
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

// Get 按 ID 返回 Run 当前状态。
func (store *RunStore) Get(ctx context.Context, runID string) (domainrun.Run, error) {
	return getRun(ctx, store.database, runID)
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
	leaseDuration time.Duration,
) (domainrun.Run, error) {
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
UPDATE runs SET status = ?, runner_pid = ?, runner_started_at = ?, started_at = ?,
                lease_expires_at = ?, updated_at = ?
WHERE id = ? AND status = 'CLAIMED' AND lease_generation = ?`,
		current.Status, runnerPID, formatTime(now), formatTime(now),
		formatTime(current.LeaseExpiresAt), formatTime(now), runID, leaseGeneration,
	)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("start run: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return domainrun.Run{}, err
	}
	if err := transaction.Commit(); err != nil {
		return domainrun.Run{}, fmt.Errorf("commit run start: %w", err)
	}
	return current, nil
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
	current.Status = domainrun.StatusRunning
	current.UpdatedAt = now
	if err := transaction.Commit(); err != nil {
		return domainrun.Run{}, fmt.Errorf("commit mark run running: %w", err)
	}
	return current, nil
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
	if current.Status != domainrun.StatusRunning && current.Status != domainrun.StatusStarting && current.Status != domainrun.StatusClaimed {
		return domainrun.Run{}, ErrRunStateConflict
	}
	if current.Purpose != domainrun.PurposeTriage {
		if err := finishTaskRun(ctx, transaction, current, finish.Status); err != nil {
			return domainrun.Run{}, err
		}
	}
	now := time.Now().UTC()
	retryable := nullableBool(finish.FailureRetryable)
	result, err := transaction.ExecContext(ctx, `
UPDATE runs SET status = ?, finished_at = ?, exit_code = ?, failure_kind = ?,
                failure_code = ?, failure_message = ?, failure_retryable = ?, updated_at = ?
WHERE id = ? AND status IN ('CLAIMED', 'STARTING', 'RUNNING') AND lease_generation = ?`,
		finish.Status, formatTime(now), finish.ExitCode, finish.FailureKind,
		finish.FailureCode, finish.FailureMessage, retryable, formatTime(now), runID, leaseGeneration,
	)
	if err != nil {
		return domainrun.Run{}, fmt.Errorf("finish run: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return domainrun.Run{}, err
	}
	current.Status = finish.Status
	current.FinishedAt = now
	current.UpdatedAt = now
	if err := transaction.Commit(); err != nil {
		return domainrun.Run{}, fmt.Errorf("commit run finish: %w", err)
	}
	return current, nil
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

type runnableTask struct {
	Task                task.Task
	ProfileRevisionID   string
	Purpose             domainrun.Purpose
	ContinuationOfRunID string
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

func selectRunnableTask(ctx context.Context, transaction *sql.Tx) (runnableTask, error) {
	if selected, err := selectRevisionTask(ctx, transaction); !errors.Is(err, ErrNoRunnableTask) {
		return selected, err
	}
	if selected, err := selectTriageTask(ctx, transaction); !errors.Is(err, ErrNoRunnableTask) {
		return selected, err
	}
	return selectImplementationTask(ctx, transaction)
}

func selectRevisionTask(ctx context.Context, transaction *sql.Tx) (runnableTask, error) {
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

func selectTriageTask(ctx context.Context, transaction *sql.Tx) (runnableTask, error) {
	return selectTaskForPurpose(ctx, transaction, `
SELECT t.id, r.id, 'TRIAGE', ''
FROM tasks t
JOIN project_agent_defaults d ON d.purpose = 'TRIAGE'
JOIN agent_profiles p ON p.id = d.profile_id
JOIN agent_profile_revisions r ON r.id = p.current_revision_id
WHERE t.status = 'READY' AND length(trim(r.command)) > 0
  AND NOT EXISTS (
      SELECT 1 FROM task_assessments a
      WHERE a.task_id = t.id AND a.task_assessment_version = t.assessment_input_version
  )
  AND NOT EXISTS (
      SELECT 1 FROM runs attempted
      WHERE attempted.task_id = t.id AND attempted.purpose = 'TRIAGE'
        AND attempted.subject_version = t.assessment_input_version
  )
ORDER BY t.priority ASC, t.created_at ASC, t.id ASC LIMIT 1`)
}

func selectImplementationTask(ctx context.Context, transaction *sql.Tx) (runnableTask, error) {
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

func selectTaskForPurpose(ctx context.Context, transaction *sql.Tx, query string) (runnableTask, error) {
	var selected runnableTask
	var taskID string
	var purpose string
	err := transaction.QueryRowContext(ctx, query).Scan(
		&taskID, &selected.ProfileRevisionID, &purpose, &selected.ContinuationOfRunID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return runnableTask{}, ErrNoRunnableTask
	}
	if err != nil {
		return runnableTask{}, fmt.Errorf("select runnable task: %w", err)
	}
	selected.Task, err = getTask(ctx, transaction, taskID)
	if err != nil {
		return runnableTask{}, err
	}
	selected.Purpose = domainrun.Purpose(purpose)
	return selected, nil
}

func createClaim(selected runnableTask, leaseDuration time.Duration) (domainrun.Claim, error) {
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
			ID: runID, Purpose: selected.Purpose, TaskID: selected.Task.ID,
			Status: domainrun.StatusClaimed, ProfileRevisionID: selected.ProfileRevisionID,
			ContinuationOfRunID: selected.ContinuationOfRunID,
			SubjectVersion:      subjectVersion(selected),
			LeaseGeneration:     1, LeaseExpiresAt: now.Add(leaseDuration),
			RunNonce: nonce,
			QueuedAt: now, ClaimedAt: now, CreatedAt: now, UpdatedAt: now,
		},
	}, nil
}

func subjectVersion(selected runnableTask) int64 {
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
    queued_at, claimed_at, created_at, updated_at, subject_version
) VALUES (?, ?, NULL, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		claim.Run.ID, claim.Run.Purpose, claim.Run.TaskID, claim.Run.Status,
		claim.Run.ProfileRevisionID, nullableString(claim.Run.ContinuationOfRunID),
		hex.EncodeToString(hash[:]), claim.Run.LeaseGeneration,
		formatTime(claim.Run.LeaseExpiresAt), claim.Run.RunNonce, formatTime(claim.Run.QueuedAt),
		formatTime(claim.Run.ClaimedAt), formatTime(claim.Run.CreatedAt), formatTime(claim.Run.UpdatedAt),
		claim.Run.SubjectVersion,
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
		if updateErr := requireClarificationChange(result); updateErr != nil {
			return updateErr
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

func claimTask(ctx context.Context, transaction *sql.Tx, selected runnableTask, claimed domainrun.Run) error {
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
	var leaseExpiresAt, queuedAt, claimedAt, startedAt, finishedAt, createdAt, updatedAt string
	err := scanner.Scan(
		&item.ID, &item.Purpose, &item.TopicID, &item.TaskID, &item.Status,
		&item.ProfileRevisionID, &item.RetryOfRunID, &item.ContinuationOfRunID, &item.LeaseGeneration,
		&leaseExpiresAt, &queuedAt, &claimedAt, &startedAt, &finishedAt, &createdAt, &updatedAt,
		&item.SubjectVersion,
	)
	if err != nil {
		return domainrun.Run{}, err
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
