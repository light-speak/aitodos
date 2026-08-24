package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/light-speak/aitodos/internal/domain/approvalrequest"
	domainrun "github.com/light-speak/aitodos/internal/domain/run"
)

var (
	// ErrApprovalRequestNotFound 表示权限请求不存在。
	ErrApprovalRequestNotFound = errors.New("approval request not found")
	// ErrApprovalRequestConflict 表示请求已处理、版本冲突或决定不受支持。
	ErrApprovalRequestConflict = errors.New("approval request conflict")
)

// CreateApprovalRequest 由持有当前 Lease 的 Runner 创建一条结构化权限请求。
func (store *RunStore) CreateApprovalRequest(
	ctx context.Context,
	runID string,
	claimToken string,
	leaseGeneration int64,
	input approvalrequest.CreateInput,
) (approvalrequest.Request, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return approvalrequest.Request{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return approvalrequest.Request{}, fmt.Errorf("begin approval request: %w", err)
	}
	defer transaction.Rollback()
	current, err := authorizeRun(ctx, transaction, runID, claimToken, leaseGeneration)
	if err != nil {
		return approvalrequest.Request{}, err
	}
	if current.Status != domainrun.StatusRunning {
		return approvalrequest.Request{}, ErrRunStateConflict
	}
	created, err := buildApprovalRequest(current, input)
	if err != nil {
		return approvalrequest.Request{}, err
	}
	available, err := json.Marshal(created.Available)
	if err != nil {
		return approvalrequest.Request{}, err
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO approval_requests(
    id, run_id, external_request_id, item_id, kind, reason, command_text, cwd,
    host, protocol, grant_root, available_decisions_json, status, decision,
    version, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'OPEN', '', 1, ?, ?)`,
		created.ID, created.RunID, created.ExternalRequestID, created.ItemID, created.Kind,
		created.Reason, created.Command, created.CWD, created.Host, created.Protocol,
		created.GrantRoot, string(available), formatTime(created.CreatedAt), formatTime(created.UpdatedAt))
	if err != nil {
		return approvalrequest.Request{}, fmt.Errorf("insert approval request: %w", err)
	}
	if _, err := appendRunEvent(ctx, transaction, current.ID, domainrun.EventApprovalRequest, map[string]any{
		"schema_version": 1, "approval_request_id": created.ID, "kind": created.Kind,
	}); err != nil {
		return approvalrequest.Request{}, err
	}
	if err := transaction.Commit(); err != nil {
		return approvalrequest.Request{}, fmt.Errorf("commit approval request: %w", err)
	}
	return created, nil
}

func buildApprovalRequest(current domainrun.Run, input approvalrequest.CreateInput) (approvalrequest.Request, error) {
	id, err := newID()
	if err != nil {
		return approvalrequest.Request{}, err
	}
	now := time.Now().UTC()
	return approvalrequest.Request{
		ID: id, RunID: current.ID, TaskID: current.TaskID,
		ExternalRequestID: input.ExternalRequestID, ItemID: input.ItemID, Kind: input.Kind,
		Reason: input.Reason, Command: input.Command, CWD: input.CWD, Host: input.Host,
		Protocol: input.Protocol, GrantRoot: input.GrantRoot,
		Available: append([]approvalrequest.Decision(nil), input.Available...),
		Status:    approvalrequest.StatusOpen, Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// ListOpenApprovalRequests 返回全项目按等待时间排序的待处理请求。
func (store *RunStore) ListOpenApprovalRequests(ctx context.Context) ([]approvalrequest.Request, error) {
	return listApprovalRequests(ctx, store.database, "WHERE request.status = 'OPEN' ORDER BY request.created_at ASC", nil)
}

// ListRunApprovalRequests 返回一个 Run 的完整权限请求历史。
func (store *RunStore) ListRunApprovalRequests(ctx context.Context, runID string) ([]approvalrequest.Request, error) {
	return listApprovalRequests(ctx, store.database, "WHERE request.run_id = ? ORDER BY request.created_at ASC", []any{runID})
}

// GetApprovalRequest 返回单个请求，供 Runner 等待决定。
func (store *RunStore) GetApprovalRequest(ctx context.Context, id string) (approvalrequest.Request, error) {
	return scanApprovalRequest(store.database.QueryRowContext(ctx, approvalRequestSelect+" WHERE request.id = ?", id))
}

// ResolveApprovalRequest 以乐观锁保存人类决定。
func (store *RunStore) ResolveApprovalRequest(
	ctx context.Context,
	id string,
	expectedVersion int64,
	decision approvalrequest.Decision,
) (approvalrequest.Request, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return approvalrequest.Request{}, fmt.Errorf("begin resolve approval request: %w", err)
	}
	defer transaction.Rollback()
	current, err := scanApprovalRequest(transaction.QueryRowContext(ctx, approvalRequestSelect+" WHERE request.id = ?", id))
	if err != nil {
		return approvalrequest.Request{}, err
	}
	if current.Status != approvalrequest.StatusOpen || current.Version != expectedVersion || !current.Allows(decision) {
		return approvalrequest.Request{}, ErrApprovalRequestConflict
	}
	now := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE approval_requests
SET status = 'RESOLVED', decision = ?, version = version + 1, updated_at = ?, resolved_at = ?
WHERE id = ? AND status = 'OPEN' AND version = ?`,
		decision, formatTime(now), formatTime(now), id, expectedVersion)
	if err != nil {
		return approvalrequest.Request{}, fmt.Errorf("resolve approval request: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return approvalrequest.Request{}, ErrApprovalRequestConflict
	}
	if decision == approvalrequest.DecisionCancelRun {
		if err := requestCancelInTransaction(ctx, transaction, current.RunID, "人工在权限请求中停止 Run", now); err != nil {
			return approvalrequest.Request{}, err
		}
	}
	if _, err := appendRunEvent(ctx, transaction, current.RunID, domainrun.EventApprovalResolve, map[string]any{
		"schema_version": 1, "approval_request_id": current.ID, "decision": decision,
	}); err != nil {
		return approvalrequest.Request{}, err
	}
	if err := transaction.Commit(); err != nil {
		return approvalrequest.Request{}, fmt.Errorf("commit approval decision: %w", err)
	}
	current.Status = approvalrequest.StatusResolved
	current.Decision = decision
	current.Version++
	current.UpdatedAt = now
	current.ResolvedAt = &now
	return current, nil
}

// ClearApprovalRequest 由当前 Runner 在上游请求消失或进程退出时关闭待处理请求。
func (store *RunStore) ClearApprovalRequest(
	ctx context.Context,
	id string,
	claimToken string,
	leaseGeneration int64,
) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin clear approval request: %w", err)
	}
	defer transaction.Rollback()
	current, err := scanApprovalRequest(transaction.QueryRowContext(ctx, approvalRequestSelect+" WHERE request.id = ?", id))
	if err != nil {
		return err
	}
	if _, err := authorizeRun(ctx, transaction, current.RunID, claimToken, leaseGeneration); err != nil {
		return err
	}
	if current.Status != approvalrequest.StatusOpen {
		return nil
	}
	now := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE approval_requests
SET status = 'CLEARED', version = version + 1, updated_at = ?, resolved_at = ?
WHERE id = ? AND status = 'OPEN' AND version = ?`,
		formatTime(now), formatTime(now), id, current.Version)
	if err != nil {
		return fmt.Errorf("clear approval request: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return ErrApprovalRequestConflict
	}
	if _, err := appendRunEvent(ctx, transaction, current.RunID, domainrun.EventApprovalResolve, map[string]any{
		"schema_version": 1, "approval_request_id": current.ID, "status": approvalrequest.StatusCleared,
	}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit cleared approval request: %w", err)
	}
	return nil
}

const approvalRequestSelect = `
SELECT request.id, request.run_id, COALESCE(run.task_id, ''), request.external_request_id,
       request.item_id, request.kind, request.reason, request.command_text, request.cwd,
       request.host, request.protocol, request.grant_root, request.available_decisions_json,
       request.status, request.decision, request.version, request.created_at,
       request.updated_at, COALESCE(request.resolved_at, '')
FROM approval_requests request JOIN runs run ON run.id = request.run_id`

type approvalRowsQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func listApprovalRequests(ctx context.Context, queryer approvalRowsQueryer, clause string, args []any) ([]approvalrequest.Request, error) {
	rows, err := queryer.QueryContext(ctx, approvalRequestSelect+" "+clause, args...)
	if err != nil {
		return nil, fmt.Errorf("list approval requests: %w", err)
	}
	defer rows.Close()
	result := make([]approvalrequest.Request, 0)
	for rows.Next() {
		item, scanErr := scanApprovalRequest(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func scanApprovalRequest(scanner rowScanner) (approvalrequest.Request, error) {
	var item approvalrequest.Request
	var availableJSON, createdAt, updatedAt, resolvedAt string
	err := scanner.Scan(
		&item.ID, &item.RunID, &item.TaskID, &item.ExternalRequestID, &item.ItemID,
		&item.Kind, &item.Reason, &item.Command, &item.CWD, &item.Host, &item.Protocol,
		&item.GrantRoot, &availableJSON, &item.Status, &item.Decision, &item.Version,
		&createdAt, &updatedAt, &resolvedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return approvalrequest.Request{}, ErrApprovalRequestNotFound
	}
	if err != nil {
		return approvalrequest.Request{}, err
	}
	if err := json.Unmarshal([]byte(availableJSON), &item.Available); err != nil {
		return approvalrequest.Request{}, fmt.Errorf("decode approval decisions: %w", err)
	}
	item.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return approvalrequest.Request{}, err
	}
	item.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return approvalrequest.Request{}, err
	}
	if resolvedAt != "" {
		value, parseErr := parseTime(resolvedAt)
		if parseErr != nil {
			return approvalrequest.Request{}, parseErr
		}
		item.ResolvedAt = &value
	}
	return item, nil
}

func requestCancelInTransaction(ctx context.Context, transaction *sql.Tx, runID, reason string, now time.Time) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE runs SET cancel_requested_at = ?, cancel_reason = ?, updated_at = ?
WHERE id = ? AND cancel_requested_at IS NULL
  AND status IN ('CLAIMED', 'STARTING', 'RUNNING')`,
		formatTime(now), reason, formatTime(now), runID)
	if err != nil {
		return fmt.Errorf("request cancellation from approval: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 {
		_, err = appendRunEvent(ctx, transaction, runID, domainrun.EventCancelRequested, map[string]any{
			"schema_version": 1, "reason": reason,
		})
	}
	return err
}
