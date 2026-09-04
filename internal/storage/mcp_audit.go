package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/domain/mcpaudit"
)

// MCPAuditInput 是已经脱敏的 MCP 审计写入参数。
type MCPAuditInput struct {
	CallID          string
	Direction       string
	RunID           string
	ClientName      string
	ServerName      string
	ToolName        string
	Phase           mcpaudit.Phase
	ArgumentKeys    []string
	ArgumentsSHA256 string
	ResultBytes     *int64
	ErrorMessage    string
}

// MCPAuditStore 持久化不含调用参数和结果原文的 MCP 审计事件。
type MCPAuditStore struct {
	database *sql.DB
}

// NewMCPAuditStore 创建 MCP 审计存储。
func NewMCPAuditStore(database *sql.DB) *MCPAuditStore {
	return &MCPAuditStore{database: database}
}

// Append 追加一个 MCP 调用生命周期事件。
func (store *MCPAuditStore) Append(ctx context.Context, input MCPAuditInput) (mcpaudit.Event, error) {
	input = normalizeMCPAuditInput(input)
	if err := validateMCPAuditInput(input); err != nil {
		return mcpaudit.Event{}, err
	}
	id, err := newID()
	if err != nil {
		return mcpaudit.Event{}, err
	}
	now := time.Now().UTC()
	keysJSON, err := json.Marshal(input.ArgumentKeys)
	if err != nil {
		return mcpaudit.Event{}, fmt.Errorf("encode MCP argument keys: %w", err)
	}
	_, err = store.database.ExecContext(ctx, `
INSERT INTO mcp_call_events(
    id, call_id, client_name, tool_name, phase, argument_keys_json,
    arguments_sha256, result_bytes, error_message, occurred_at, direction, run_id, server_name
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?)`, id, input.CallID, input.ClientName,
		input.ToolName, input.Phase, string(keysJSON), input.ArgumentsSHA256,
		input.ResultBytes, input.ErrorMessage, formatTime(now), input.Direction, input.RunID, input.ServerName)
	if err != nil {
		return mcpaudit.Event{}, fmt.Errorf("append MCP audit event: %w", err)
	}
	return mcpaudit.Event{
		ID: id, CallID: input.CallID, Direction: input.Direction, RunID: input.RunID,
		ClientName: input.ClientName, ServerName: input.ServerName, ToolName: input.ToolName,
		Phase: input.Phase, ArgumentKeys: input.ArgumentKeys, ArgumentsSHA256: input.ArgumentsSHA256,
		ResultBytes: input.ResultBytes, ErrorMessage: input.ErrorMessage, OccurredAt: now,
	}, nil
}

// List 按发生顺序返回最近的有界 MCP 审计事件。
func (store *MCPAuditStore) List(ctx context.Context, limit int) ([]mcpaudit.Event, error) {
	if limit < 1 || limit > 500 {
		return nil, errors.New("MCP audit limit must be between 1 and 500")
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT id, call_id, direction, COALESCE(run_id, ''), client_name, server_name, tool_name, phase, argument_keys_json,
       arguments_sha256, result_bytes, error_message, occurred_at
FROM (SELECT * FROM mcp_call_events ORDER BY occurred_at DESC, id DESC LIMIT ?)
ORDER BY occurred_at, id`, limit)
	return scanMCPAuditEvents(rows, err, limit)
}

// ListRun 返回指定 Run 的出站 MCP 调用事件。
func (store *MCPAuditStore) ListRun(ctx context.Context, runID string, limit int) ([]mcpaudit.Event, error) {
	if strings.TrimSpace(runID) == "" || limit < 1 || limit > 500 {
		return nil, errors.New("invalid run MCP audit query")
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT id, call_id, direction, COALESCE(run_id, ''), client_name, server_name, tool_name, phase, argument_keys_json,
       arguments_sha256, result_bytes, error_message, occurred_at
FROM (SELECT * FROM mcp_call_events WHERE run_id = ? ORDER BY occurred_at DESC, id DESC LIMIT ?)
ORDER BY occurred_at, id`, runID, limit)
	return scanMCPAuditEvents(rows, err, limit)
}

func scanMCPAuditEvents(rows *sql.Rows, err error, capacity int) ([]mcpaudit.Event, error) {
	if err != nil {
		return nil, fmt.Errorf("list MCP audit events: %w", err)
	}
	defer rows.Close()
	items := make([]mcpaudit.Event, 0, capacity)
	for rows.Next() {
		item, scanErr := scanMCPAuditEvent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// OpenResourceLease 幂等登记一个 Run 使用中的受管资源。
func (store *MCPAuditStore) OpenResourceLease(ctx context.Context, runID, kind, provider string) (mcpaudit.ResourceLease, error) {
	runID, kind, provider = strings.TrimSpace(runID), strings.TrimSpace(kind), strings.TrimSpace(provider)
	if runID == "" || kind != "BROWSER_SESSION" || provider == "" || len(provider) > 200 {
		return mcpaudit.ResourceLease{}, errors.New("invalid MCP resource lease")
	}
	id, err := newID()
	if err != nil {
		return mcpaudit.ResourceLease{}, err
	}
	now := time.Now().UTC()
	_, err = store.database.ExecContext(ctx, `
INSERT INTO run_resource_leases(id, run_id, resource_kind, provider_name, state, opened_at)
VALUES (?, ?, ?, ?, 'ACTIVE', ?)
ON CONFLICT(run_id, resource_kind, provider_name) DO UPDATE SET
    state = 'ACTIVE', opened_at = excluded.opened_at, released_at = NULL, cleanup_reason = ''`,
		id, runID, kind, provider, formatTime(now))
	if err != nil {
		return mcpaudit.ResourceLease{}, fmt.Errorf("open MCP resource lease: %w", err)
	}
	return store.getRunResourceLease(ctx, runID, kind, provider)
}

// ReleaseRunResources 将 Run 的全部活跃资源标记为已释放或无法确认的遗留资源。
func (store *MCPAuditStore) ReleaseRunResources(ctx context.Context, runID string, abandoned bool, reason string) error {
	state := "RELEASED"
	if abandoned {
		state = "ABANDONED"
	}
	reason = strings.TrimSpace(reason)
	if strings.TrimSpace(runID) == "" || len(reason) > 1000 {
		return errors.New("invalid MCP resource release")
	}
	_, err := store.database.ExecContext(ctx, `
UPDATE run_resource_leases SET state = ?, released_at = ?, cleanup_reason = ?
WHERE run_id = ? AND state = 'ACTIVE'`, state, formatTime(time.Now().UTC()), reason, runID)
	if err != nil {
		return fmt.Errorf("release MCP resource leases: %w", err)
	}
	return nil
}

// ListRunResourceLeases 返回 Run 的资源生命周期记录。
func (store *MCPAuditStore) ListRunResourceLeases(ctx context.Context, runID string) ([]mcpaudit.ResourceLease, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, run_id, resource_kind, provider_name, state, opened_at, released_at, cleanup_reason
FROM run_resource_leases WHERE run_id = ? ORDER BY opened_at, id`, runID)
	if err != nil {
		return nil, fmt.Errorf("list MCP resource leases: %w", err)
	}
	defer rows.Close()
	items := make([]mcpaudit.ResourceLease, 0)
	for rows.Next() {
		item, scanErr := scanMCPResourceLease(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *MCPAuditStore) getRunResourceLease(ctx context.Context, runID, kind, provider string) (mcpaudit.ResourceLease, error) {
	return scanMCPResourceLease(store.database.QueryRowContext(ctx, `
SELECT id, run_id, resource_kind, provider_name, state, opened_at, released_at, cleanup_reason
FROM run_resource_leases WHERE run_id = ? AND resource_kind = ? AND provider_name = ?`, runID, kind, provider))
}

func scanMCPResourceLease(scanner rowScanner) (mcpaudit.ResourceLease, error) {
	var item mcpaudit.ResourceLease
	var openedAt string
	var releasedAt sql.NullString
	if err := scanner.Scan(&item.ID, &item.RunID, &item.ResourceKind, &item.ProviderName,
		&item.State, &openedAt, &releasedAt, &item.CleanupReason); err != nil {
		return mcpaudit.ResourceLease{}, err
	}
	parsed, err := parseTime(openedAt)
	if err != nil {
		return mcpaudit.ResourceLease{}, fmt.Errorf("parse MCP resource open time: %w", err)
	}
	item.OpenedAt = parsed
	if releasedAt.Valid {
		parsed, err = parseTime(releasedAt.String)
		if err != nil {
			return mcpaudit.ResourceLease{}, fmt.Errorf("parse MCP resource release time: %w", err)
		}
		item.ReleasedAt = &parsed
	}
	return item, nil
}

func normalizeMCPAuditInput(input MCPAuditInput) MCPAuditInput {
	input.CallID = strings.TrimSpace(input.CallID)
	input.Direction = strings.TrimSpace(input.Direction)
	if input.Direction == "" {
		input.Direction = "INBOUND"
	}
	input.RunID = strings.TrimSpace(input.RunID)
	input.ClientName = strings.TrimSpace(input.ClientName)
	input.ServerName = strings.TrimSpace(input.ServerName)
	input.ToolName = strings.TrimSpace(input.ToolName)
	input.ErrorMessage = truncateRunes(strings.TrimSpace(input.ErrorMessage), 2000)
	keys := make([]string, 0, len(input.ArgumentKeys))
	seen := make(map[string]struct{}, len(input.ArgumentKeys))
	for _, key := range input.ArgumentKeys {
		key = truncateRunes(strings.TrimSpace(key), 200)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 100 {
		keys = keys[:100]
	}
	input.ArgumentKeys = keys
	return input
}

func truncateRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}

func validateMCPAuditInput(input MCPAuditInput) error {
	if input.CallID == "" || len(input.CallID) > 200 || input.ClientName == "" || len(input.ClientName) > 200 ||
		input.ToolName == "" || len(input.ToolName) > 200 || len(input.ArgumentsSHA256) != 64 {
		return errors.New("invalid MCP audit identity")
	}
	if input.Direction != "INBOUND" && input.Direction != "OUTBOUND" {
		return errors.New("invalid MCP audit direction")
	}
	if len(input.RunID) > 200 || len(input.ServerName) > 200 || (input.Direction == "OUTBOUND" && (input.RunID == "" || input.ServerName == "")) {
		return errors.New("invalid MCP audit subject")
	}
	if input.Phase != mcpaudit.PhaseStarted && input.Phase != mcpaudit.PhaseCompleted && input.Phase != mcpaudit.PhaseFailed {
		return errors.New("invalid MCP audit phase")
	}
	return nil
}

func scanMCPAuditEvent(scanner rowScanner) (mcpaudit.Event, error) {
	var item mcpaudit.Event
	var keysJSON, occurredAt string
	var resultBytes sql.NullInt64
	if err := scanner.Scan(&item.ID, &item.CallID, &item.Direction, &item.RunID, &item.ClientName, &item.ServerName, &item.ToolName, &item.Phase,
		&keysJSON, &item.ArgumentsSHA256, &resultBytes, &item.ErrorMessage, &occurredAt); err != nil {
		return mcpaudit.Event{}, fmt.Errorf("scan MCP audit event: %w", err)
	}
	if err := json.Unmarshal([]byte(keysJSON), &item.ArgumentKeys); err != nil {
		return mcpaudit.Event{}, fmt.Errorf("decode MCP audit argument keys: %w", err)
	}
	if resultBytes.Valid {
		item.ResultBytes = &resultBytes.Int64
	}
	parsed, err := parseTime(occurredAt)
	if err != nil {
		return mcpaudit.Event{}, fmt.Errorf("parse MCP audit time: %w", err)
	}
	item.OccurredAt = parsed
	return item, nil
}
