package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentsession"
	domainrun "github.com/light-speak/aitodos/internal/domain/run"
)

// ErrAgentSessionNotFound 表示 Run 没有可恢复的外部 Session。
var ErrAgentSessionNotFound = errors.New("agent session not found")

func findCompatibleAgentSession(
	ctx context.Context,
	transaction *sql.Tx,
	selected runnableWork,
) (string, error) {
	var id string
	query := `
SELECT id FROM agent_sessions
WHERE task_id = ? AND profile_revision_id = ? AND status = 'ACTIVE'
ORDER BY updated_at DESC LIMIT 1`
	subjectID := selected.Task.ID
	if selected.Purpose == domainrun.PurposePlanning {
		query = `
SELECT id FROM agent_sessions
WHERE topic_id = ? AND profile_revision_id = ? AND status = 'ACTIVE'
ORDER BY updated_at DESC LIMIT 1`
		subjectID = selected.Topic.ID
	}
	err := transaction.QueryRowContext(ctx, query, subjectID, selected.ProfileRevisionID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("find compatible agent session: %w", err)
	}
	return id, nil
}

// RecordAgentSession 保存 Codex JSONL 返回的外部 Session ID，并绑定当前 Run。
func (store *RunStore) RecordAgentSession(
	ctx context.Context,
	runID string,
	externalSessionID string,
) (agentsession.Session, error) {
	externalSessionID = strings.TrimSpace(externalSessionID)
	if externalSessionID == "" || len(externalSessionID) > 255 {
		return agentsession.Session{}, errors.New("invalid external agent session id")
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return agentsession.Session{}, fmt.Errorf("begin record agent session: %w", err)
	}
	defer transaction.Rollback()
	current, err := getRun(ctx, transaction, runID)
	if err != nil {
		return agentsession.Session{}, err
	}
	if current.AgentSessionID != "" {
		return store.updateResumedSession(ctx, transaction, current, externalSessionID)
	}
	created, err := createAgentSession(ctx, transaction, current, externalSessionID)
	if err != nil {
		return agentsession.Session{}, err
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE runs SET agent_session_id = ?, session_resumed = 0, updated_at = ?
WHERE id = ? AND agent_session_id IS NULL`, created.ID, formatTime(created.UpdatedAt), current.ID); err != nil {
		return agentsession.Session{}, fmt.Errorf("bind new agent session to run: %w", err)
	}
	if _, err := appendRunEvent(ctx, transaction, current.ID, domainrun.EventSessionAttached, map[string]any{
		"schema_version": 1, "agent_session_id": created.ID, "resumed": false,
	}); err != nil {
		return agentsession.Session{}, err
	}
	if err := transaction.Commit(); err != nil {
		return agentsession.Session{}, fmt.Errorf("commit new agent session: %w", err)
	}
	return created, nil
}

func (store *RunStore) updateResumedSession(
	ctx context.Context,
	transaction *sql.Tx,
	current domainrun.Run,
	externalSessionID string,
) (agentsession.Session, error) {
	session, err := getAgentSession(ctx, transaction, current.AgentSessionID)
	if err != nil {
		return agentsession.Session{}, err
	}
	if session.Status != agentsession.StatusActive || session.ExternalSessionID != externalSessionID {
		return agentsession.Session{}, errors.New("resumed agent session identity mismatch")
	}
	now := time.Now().UTC()
	if _, err := transaction.ExecContext(ctx, `
UPDATE agent_sessions SET last_run_id = ?, updated_at = ? WHERE id = ? AND status = 'ACTIVE'`,
		current.ID, formatTime(now), session.ID); err != nil {
		return agentsession.Session{}, fmt.Errorf("update resumed agent session: %w", err)
	}
	if _, err := appendRunEvent(ctx, transaction, current.ID, domainrun.EventSessionAttached, map[string]any{
		"schema_version": 1, "agent_session_id": session.ID, "resumed": true,
	}); err != nil {
		return agentsession.Session{}, err
	}
	if err := transaction.Commit(); err != nil {
		return agentsession.Session{}, fmt.Errorf("commit resumed agent session: %w", err)
	}
	session.LastRunID = current.ID
	session.UpdatedAt = now
	return session, nil
}

func createAgentSession(
	ctx context.Context,
	transaction *sql.Tx,
	current domainrun.Run,
	externalSessionID string,
) (agentsession.Session, error) {
	var adapter, model string
	if err := transaction.QueryRowContext(ctx, `
SELECT adapter, model FROM agent_profile_revisions WHERE id = ?`, current.ProfileRevisionID).Scan(&adapter, &model); err != nil {
		return agentsession.Session{}, fmt.Errorf("load agent session compatibility: %w", err)
	}
	id, err := newID()
	if err != nil {
		return agentsession.Session{}, err
	}
	now := time.Now().UTC()
	created := agentsession.Session{
		ID: id, TopicID: current.TopicID, TaskID: current.TaskID,
		ProfileRevisionID: current.ProfileRevisionID, Model: model, Adapter: adapter,
		ExternalSessionID: externalSessionID, Status: agentsession.StatusActive,
		LastRunID: current.ID, CreatedAt: now, UpdatedAt: now,
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO agent_sessions(
    id, topic_id, task_id, profile_revision_id, model, adapter, external_session_id,
    status, last_run_id, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, 'ACTIVE', ?, ?, ?)`,
		created.ID, nullableString(created.TopicID), nullableString(created.TaskID),
		created.ProfileRevisionID, created.Model, created.Adapter, created.ExternalSessionID,
		created.LastRunID, formatTime(created.CreatedAt), formatTime(created.UpdatedAt))
	if err != nil {
		return agentsession.Session{}, fmt.Errorf("insert agent session: %w", err)
	}
	return created, nil
}

// GetAgentSessionForRun 返回 Run 在创建时冻结的 Resume 身份。
func (store *RunStore) GetAgentSessionForRun(ctx context.Context, runID string) (agentsession.Session, error) {
	var sessionID string
	err := store.database.QueryRowContext(ctx, `
SELECT COALESCE(agent_session_id, '') FROM runs WHERE id = ?`, runID).Scan(&sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return agentsession.Session{}, ErrRunNotFound
	}
	if err != nil {
		return agentsession.Session{}, fmt.Errorf("read run agent session: %w", err)
	}
	if sessionID == "" {
		return agentsession.Session{}, ErrAgentSessionNotFound
	}
	return getAgentSession(ctx, store.database, sessionID)
}

// InvalidateAgentSessionForRun 使失败的原生 Resume 不会在后续 Run 中反复使用。
func (store *RunStore) InvalidateAgentSessionForRun(ctx context.Context, runID string) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin invalidate agent session: %w", err)
	}
	defer transaction.Rollback()
	current, err := getRun(ctx, transaction, runID)
	if err != nil {
		return err
	}
	if current.AgentSessionID == "" {
		return nil
	}
	now := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE agent_sessions SET status = 'INVALID', updated_at = ?
WHERE id = ? AND status = 'ACTIVE'`, formatTime(now), current.AgentSessionID)
	if err != nil {
		return fmt.Errorf("invalidate agent session: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 {
		if _, err := appendRunEvent(ctx, transaction, current.ID, domainrun.EventSessionInvalid, map[string]any{
			"schema_version": 1, "agent_session_id": current.AgentSessionID,
		}); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit invalidate agent session: %w", err)
	}
	return nil
}

func getAgentSession(ctx context.Context, queryer rowQueryer, id string) (agentsession.Session, error) {
	var item agentsession.Session
	var createdAt, updatedAt string
	err := queryer.QueryRowContext(ctx, `
SELECT id, COALESCE(topic_id, ''), COALESCE(task_id, ''), profile_revision_id,
       model, adapter, external_session_id, status, last_run_id, created_at, updated_at
FROM agent_sessions WHERE id = ?`, id).Scan(
		&item.ID, &item.TopicID, &item.TaskID, &item.ProfileRevisionID,
		&item.Model, &item.Adapter, &item.ExternalSessionID, &item.Status,
		&item.LastRunID, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return agentsession.Session{}, ErrAgentSessionNotFound
	}
	if err != nil {
		return agentsession.Session{}, err
	}
	item.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return agentsession.Session{}, err
	}
	item.UpdatedAt, err = parseTime(updatedAt)
	return item, err
}
