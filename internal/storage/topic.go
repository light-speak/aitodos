package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/domain/topic"
)

var (
	// ErrTopicNotFound 表示指定 Topic 不存在。
	ErrTopicNotFound = errors.New("topic not found")
	// ErrTopicVersionConflict 表示写入基于过期的 Topic 版本。
	ErrTopicVersionConflict = errors.New("topic version conflict")
)

const topicColumns = `
id, topic_key, title, description, status,
current_plan_id, current_summary_id, version, created_at, updated_at`

// TopicStore 使用 SQLite 持久化 Topic 当前状态和审计事件。
type TopicStore struct {
	database *sql.DB
}

// NewTopicStore 创建 Topic 持久化服务。
func NewTopicStore(database *sql.DB) *TopicStore {
	return &TopicStore{database: database}
}

// Create 原子创建 Topic 及其首条审计事件。
func (store *TopicStore) Create(ctx context.Context, input topic.CreateInput) (topic.Topic, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return topic.Topic{}, err
	}
	created, event, err := newTopicAndEvent(input)
	if err != nil {
		return topic.Topic{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return topic.Topic{}, fmt.Errorf("begin topic creation: %w", err)
	}
	defer transaction.Rollback()
	if err := insertTopic(ctx, transaction, created); err != nil {
		return topic.Topic{}, err
	}
	if err := insertTopicEvent(ctx, transaction, event); err != nil {
		return topic.Topic{}, err
	}
	if err := transaction.Commit(); err != nil {
		return topic.Topic{}, fmt.Errorf("commit topic creation: %w", err)
	}
	return created, nil
}

// Get 根据 ID 读取 Topic。
func (store *TopicStore) Get(ctx context.Context, id string) (topic.Topic, error) {
	return getTopic(ctx, store.database, id)
}

// List 按最近更新时间返回当前项目的全部 Topic。
func (store *TopicStore) List(ctx context.Context) ([]topic.Topic, error) {
	rows, err := store.database.QueryContext(ctx, "SELECT "+topicColumns+" FROM topics ORDER BY updated_at DESC, created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}
	defer rows.Close()
	result := make([]topic.Topic, 0)
	for rows.Next() {
		item, err := scanTopic(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate topics: %w", err)
	}
	return result, nil
}

// ApplyCommand 原子更新 Topic 状态并追加审计事件。
func (store *TopicStore) ApplyCommand(ctx context.Context, id string, expectedVersion int64, command topic.Command) (topic.Topic, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return topic.Topic{}, fmt.Errorf("begin topic command: %w", err)
	}
	defer transaction.Rollback()
	current, err := getTopic(ctx, transaction, id)
	if err != nil {
		return topic.Topic{}, err
	}
	if current.Version != expectedVersion {
		return topic.Topic{}, ErrTopicVersionConflict
	}
	next, err := topic.Transition(current.Status, command)
	if err != nil {
		return topic.Topic{}, err
	}
	updated, event, err := prepareTopicTransition(current, next, command)
	if err != nil {
		return topic.Topic{}, err
	}
	if err := updateTopicStatus(ctx, transaction, current, updated); err != nil {
		return topic.Topic{}, err
	}
	if err := insertTopicEvent(ctx, transaction, event); err != nil {
		return topic.Topic{}, err
	}
	if err := transaction.Commit(); err != nil {
		return topic.Topic{}, fmt.Errorf("commit topic command: %w", err)
	}
	return updated, nil
}

// RequestPlanning 将当前 OPEN Topic 的最新内容标记为需要新的规划轮次。
func (store *TopicStore) RequestPlanning(ctx context.Context, id string, expectedVersion int64) (topic.Topic, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return topic.Topic{}, fmt.Errorf("begin planning request: %w", err)
	}
	defer transaction.Rollback()
	current, err := getTopic(ctx, transaction, id)
	if err != nil {
		return topic.Topic{}, err
	}
	if current.Version != expectedVersion {
		return topic.Topic{}, ErrTopicVersionConflict
	}
	if current.Status != topic.StatusOpen {
		return topic.Topic{}, errors.New("only open topic can request planning")
	}
	updated := current
	updated.Version++
	updated.UpdatedAt = time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
UPDATE topics SET version = ?, updated_at = ? WHERE id = ? AND status = 'OPEN' AND version = ?`,
		updated.Version, formatTime(updated.UpdatedAt), current.ID, current.Version)
	if err != nil {
		return topic.Topic{}, fmt.Errorf("request topic planning: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return topic.Topic{}, ErrTopicVersionConflict
	}
	event, err := newTopicActivityEvent(updated, topic.EventPlanningAsked, map[string]any{"schema_version": 1})
	if err != nil {
		return topic.Topic{}, err
	}
	if err := insertTopicEvent(ctx, transaction, event); err != nil {
		return topic.Topic{}, err
	}
	if err := transaction.Commit(); err != nil {
		return topic.Topic{}, fmt.Errorf("commit planning request: %w", err)
	}
	return updated, nil
}

// ListEvents 返回 Topic 的完整审计记录。
func (store *TopicStore) ListEvents(ctx context.Context, topicID string) ([]topic.Event, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, topic_id, sequence, event_type, payload_json, occurred_at
FROM topic_events WHERE topic_id = ? ORDER BY sequence`, topicID)
	if err != nil {
		return nil, fmt.Errorf("list topic events: %w", err)
	}
	defer rows.Close()
	result := make([]topic.Event, 0)
	for rows.Next() {
		event, err := scanTopicEvent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate topic events: %w", err)
	}
	return result, nil
}

func newTopicAndEvent(input topic.CreateInput) (topic.Topic, topic.Event, error) {
	id, err := newID()
	if err != nil {
		return topic.Topic{}, topic.Event{}, err
	}
	eventID, err := newID()
	if err != nil {
		return topic.Topic{}, topic.Event{}, err
	}
	now := time.Now().UTC()
	created := topic.Topic{
		ID: id, Key: topicKey(id), Title: input.Title, Description: input.Description,
		Status: topic.StatusOpen, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	payload, err := json.Marshal(map[string]any{"schema_version": 1, "title": created.Title})
	if err != nil {
		return topic.Topic{}, topic.Event{}, fmt.Errorf("encode topic created event: %w", err)
	}
	event := topic.Event{ID: eventID, TopicID: id, Sequence: 1, Type: topic.EventCreated, Payload: payload, OccurredAt: now}
	return created, event, nil
}

func prepareTopicTransition(current topic.Topic, next topic.Status, command topic.Command) (topic.Topic, topic.Event, error) {
	eventID, err := newID()
	if err != nil {
		return topic.Topic{}, topic.Event{}, err
	}
	now := time.Now().UTC()
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1, "command": command, "from": current.Status, "to": next,
	})
	if err != nil {
		return topic.Topic{}, topic.Event{}, fmt.Errorf("encode topic status event: %w", err)
	}
	updated := current
	updated.Status = next
	updated.Version++
	updated.UpdatedAt = now
	event := topic.Event{
		ID: eventID, TopicID: current.ID, Sequence: updated.Version,
		Type: topic.EventStatusChanged, Payload: payload, OccurredAt: now,
	}
	return updated, event, nil
}

func newTopicActivityEvent(current topic.Topic, eventType topic.EventType, payloadValue map[string]any) (topic.Event, error) {
	eventID, err := newID()
	if err != nil {
		return topic.Event{}, err
	}
	payload, err := json.Marshal(payloadValue)
	if err != nil {
		return topic.Event{}, fmt.Errorf("encode topic activity event: %w", err)
	}
	return topic.Event{
		ID: eventID, TopicID: current.ID, Sequence: current.Version,
		Type: eventType, Payload: payload, OccurredAt: current.UpdatedAt,
	}, nil
}

func insertTopic(ctx context.Context, transaction *sql.Tx, item topic.Topic) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO topics (
    id, topic_key, title, description, status, current_plan_id,
    current_summary_id, version, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.Key, item.Title, item.Description, item.Status,
		item.CurrentPlanID, item.CurrentSummaryID, item.Version,
		formatTime(item.CreatedAt), formatTime(item.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert topic: %w", err)
	}
	return nil
}

func updateTopicStatus(ctx context.Context, transaction *sql.Tx, current, updated topic.Topic) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE topics SET status = ?, version = ?, updated_at = ?
WHERE id = ? AND status = ? AND version = ?`,
		updated.Status, updated.Version, formatTime(updated.UpdatedAt),
		current.ID, current.Status, current.Version,
	)
	if err != nil {
		return fmt.Errorf("update topic status: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read topic update result: %w", err)
	}
	if count != 1 {
		return ErrTopicVersionConflict
	}
	return nil
}

func insertTopicEvent(ctx context.Context, transaction *sql.Tx, event topic.Event) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO topic_events (id, topic_id, sequence, event_type, payload_json, occurred_at)
VALUES (?, ?, ?, ?, ?, ?)`, event.ID, event.TopicID, event.Sequence, event.Type, string(event.Payload), formatTime(event.OccurredAt))
	if err != nil {
		return fmt.Errorf("insert topic event: %w", err)
	}
	return nil
}

func getTopic(ctx context.Context, queryer rowQueryer, id string) (topic.Topic, error) {
	item, err := scanTopic(queryer.QueryRowContext(ctx, "SELECT "+topicColumns+" FROM topics WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return topic.Topic{}, ErrTopicNotFound
	}
	return item, err
}

func scanTopic(scanner rowScanner) (topic.Topic, error) {
	var item topic.Topic
	var createdAt, updatedAt string
	err := scanner.Scan(
		&item.ID, &item.Key, &item.Title, &item.Description, &item.Status,
		&item.CurrentPlanID, &item.CurrentSummaryID, &item.Version, &createdAt, &updatedAt,
	)
	if err != nil {
		return topic.Topic{}, err
	}
	item.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return topic.Topic{}, fmt.Errorf("parse topic created time: %w", err)
	}
	item.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return topic.Topic{}, fmt.Errorf("parse topic updated time: %w", err)
	}
	return item, nil
}

func scanTopicEvent(scanner rowScanner) (topic.Event, error) {
	var event topic.Event
	var payload, occurredAt string
	if err := scanner.Scan(&event.ID, &event.TopicID, &event.Sequence, &event.Type, &payload, &occurredAt); err != nil {
		return topic.Event{}, fmt.Errorf("scan topic event: %w", err)
	}
	event.Payload = json.RawMessage(payload)
	parsed, err := parseTime(occurredAt)
	if err != nil {
		return topic.Event{}, fmt.Errorf("parse topic event time: %w", err)
	}
	event.OccurredAt = parsed
	return event, nil
}

func topicKey(id string) string {
	compact := strings.ReplaceAll(id, "-", "")
	return "TOP-" + strings.ToUpper(compact[:8])
}
