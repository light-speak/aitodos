package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/light-speak/aitodos/internal/domain/discussion"
	"github.com/light-speak/aitodos/internal/domain/topic"
)

// DiscussionStore 使用 SQLite 持久化 Topic 与 Task 的讨论线程。
type DiscussionStore struct {
	database *sql.DB
}

// NewDiscussionStore 创建讨论持久化服务。
func NewDiscussionStore(database *sql.DB) *DiscussionStore {
	return &DiscussionStore{database: database}
}

// AppendTopicMessage 原子取得或创建 Topic Thread，并追加一条用户消息。
func (store *DiscussionStore) AppendTopicMessage(
	ctx context.Context,
	topicID string,
	input discussion.CreateMessageInput,
) (discussion.Message, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return discussion.Message{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return discussion.Message{}, fmt.Errorf("begin topic message append: %w", err)
	}
	defer transaction.Rollback()
	if err := requireTopic(ctx, transaction, topicID); err != nil {
		return discussion.Message{}, err
	}
	threadID, err := findOrCreateThread(ctx, transaction, "topic_id", topicID)
	if err != nil {
		return discussion.Message{}, err
	}
	message, err := appendMessage(ctx, transaction, threadID, discussion.AuthorHuman, input)
	if err != nil {
		return discussion.Message{}, err
	}
	if err := linkTopicMessageTasks(ctx, transaction, topicID, message); err != nil {
		return discussion.Message{}, err
	}
	if err := recordHumanTopicMessage(ctx, transaction, topicID, message); err != nil {
		return discussion.Message{}, err
	}
	if err := transaction.Commit(); err != nil {
		return discussion.Message{}, fmt.Errorf("commit topic message append: %w", err)
	}
	return message, nil
}

// AppendTaskMessage 原子取得或创建 Task Thread，并追加一条用户消息。
func (store *DiscussionStore) AppendTaskMessage(
	ctx context.Context,
	taskID string,
	input discussion.CreateMessageInput,
) (discussion.Message, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return discussion.Message{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return discussion.Message{}, fmt.Errorf("begin task message append: %w", err)
	}
	defer transaction.Rollback()
	if err := requireTask(ctx, transaction, taskID); err != nil {
		return discussion.Message{}, err
	}
	threadID, err := findOrCreateThread(ctx, transaction, "task_id", taskID)
	if err != nil {
		return discussion.Message{}, err
	}
	message, err := appendMessage(ctx, transaction, threadID, discussion.AuthorHuman, input)
	if err != nil {
		return discussion.Message{}, err
	}
	if err := linkTaskMessageTasks(ctx, transaction, taskID, message); err != nil {
		return discussion.Message{}, err
	}
	if _, err := transaction.ExecContext(ctx, "UPDATE tasks SET updated_at = ? WHERE id = ?", formatTime(message.CreatedAt), taskID); err != nil {
		return discussion.Message{}, fmt.Errorf("update task activity: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return discussion.Message{}, fmt.Errorf("commit task message append: %w", err)
	}
	return message, nil
}

// ListTopicMessages 按线程序号返回 Topic 的完整讨论消息。
func (store *DiscussionStore) ListTopicMessages(ctx context.Context, topicID string) ([]discussion.Message, error) {
	if err := requireTopic(ctx, store.database, topicID); err != nil {
		return nil, err
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT messages.id, messages.thread_id, messages.sequence, messages.author_kind,
       messages.content, messages.created_at
FROM messages
JOIN threads ON threads.id = messages.thread_id
WHERE threads.topic_id = ?
ORDER BY messages.sequence`, topicID)
	if err != nil {
		return nil, fmt.Errorf("list topic messages: %w", err)
	}
	defer rows.Close()
	messages, err := scanMessages(rows)
	if err != nil {
		return nil, err
	}
	if err := store.attachLinkedTasks(ctx, messages, "topic_id", topicID); err != nil {
		return nil, err
	}
	return messages, nil
}

// ListTaskMessages 按线程序号返回 Task 的完整讨论消息。
func (store *DiscussionStore) ListTaskMessages(ctx context.Context, taskID string) ([]discussion.Message, error) {
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return nil, err
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT messages.id, messages.thread_id, messages.sequence, messages.author_kind,
       messages.content, messages.created_at
FROM messages
JOIN threads ON threads.id = messages.thread_id
WHERE threads.task_id = ?
ORDER BY messages.sequence`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task messages: %w", err)
	}
	messages, err := scanMessages(rows)
	if err != nil {
		return nil, err
	}
	if err := store.attachLinkedTasks(ctx, messages, "task_id", taskID); err != nil {
		return nil, err
	}
	return messages, nil
}

func requireTopic(ctx context.Context, queryer rowQueryer, topicID string) error {
	var exists int
	if err := queryer.QueryRowContext(ctx, "SELECT 1 FROM topics WHERE id = ?", topicID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTopicNotFound
		}
		return fmt.Errorf("find message topic: %w", err)
	}
	return nil
}

func findOrCreateThread(ctx context.Context, transaction *sql.Tx, subjectColumn, subjectID string) (string, error) {
	var threadID string
	err := transaction.QueryRowContext(ctx, "SELECT id FROM threads WHERE "+subjectColumn+" = ?", subjectID).Scan(&threadID)
	if err == nil {
		return threadID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("find discussion thread: %w", err)
	}
	threadID, err = newID()
	if err != nil {
		return "", err
	}
	now := formatTime(time.Now().UTC())
	statement := "INSERT INTO threads (id, " + subjectColumn + ", created_at, updated_at) VALUES (?, ?, ?, ?)"
	if _, err := transaction.ExecContext(ctx, statement, threadID, subjectID, now, now); err != nil {
		return "", fmt.Errorf("insert discussion thread: %w", err)
	}
	return threadID, nil
}

func appendMessage(
	ctx context.Context,
	transaction *sql.Tx,
	threadID string,
	author discussion.AuthorKind,
	input discussion.CreateMessageInput,
) (discussion.Message, error) {
	messageID, err := newID()
	if err != nil {
		return discussion.Message{}, err
	}
	var sequence int64
	if err := transaction.QueryRowContext(ctx, "SELECT COALESCE(MAX(sequence), 0) + 1 FROM messages WHERE thread_id = ?", threadID).Scan(&sequence); err != nil {
		return discussion.Message{}, fmt.Errorf("next message sequence: %w", err)
	}
	created := discussion.Message{
		ID: messageID, ThreadID: threadID, Sequence: sequence,
		AuthorKind: author, Content: input.Content,
		LinkedTaskIDs: input.LinkedTaskIDs, CreatedAt: time.Now().UTC(),
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO messages (id, thread_id, sequence, author_kind, content, created_at)
VALUES (?, ?, ?, ?, ?, ?)`, created.ID, created.ThreadID, created.Sequence, created.AuthorKind, created.Content, formatTime(created.CreatedAt)); err != nil {
		return discussion.Message{}, fmt.Errorf("insert message: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "UPDATE threads SET updated_at = ? WHERE id = ?", formatTime(created.CreatedAt), threadID); err != nil {
		return discussion.Message{}, fmt.Errorf("update thread activity: %w", err)
	}
	return created, nil
}

func recordHumanTopicMessage(
	ctx context.Context,
	transaction *sql.Tx,
	topicID string,
	message discussion.Message,
) error {
	current, err := getTopic(ctx, transaction, topicID)
	if err != nil {
		return err
	}
	current.Version++
	current.UpdatedAt = message.CreatedAt
	result, err := transaction.ExecContext(ctx, `
UPDATE topics SET version = ?, updated_at = ? WHERE id = ? AND version = ?`,
		current.Version, formatTime(current.UpdatedAt), current.ID, current.Version-1)
	if err != nil {
		return fmt.Errorf("advance topic discussion version: %w", err)
	}
	if err := requireSingleChange(result); err != nil {
		return ErrTopicVersionConflict
	}
	eventID, err := newID()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1, "message_id": message.ID, "author_kind": message.AuthorKind,
	})
	if err != nil {
		return fmt.Errorf("encode topic message event: %w", err)
	}
	return insertTopicEvent(ctx, transaction, topic.Event{
		ID: eventID, TopicID: topicID, Sequence: current.Version,
		Type: topic.EventMessageAdded, Payload: payload, OccurredAt: message.CreatedAt,
	})
}

func appendAgentTopicMessage(
	ctx context.Context,
	transaction *sql.Tx,
	topicID string,
	content string,
) (discussion.Message, error) {
	input := discussion.CreateMessageInput{Content: content}.Normalized()
	if err := input.Validate(); err != nil {
		return discussion.Message{}, err
	}
	threadID, err := findOrCreateThread(ctx, transaction, "topic_id", topicID)
	if err != nil {
		return discussion.Message{}, err
	}
	message, err := appendMessage(ctx, transaction, threadID, discussion.AuthorAgent, input)
	if err != nil {
		return discussion.Message{}, err
	}
	if _, err := transaction.ExecContext(ctx, "UPDATE topics SET updated_at = ? WHERE id = ?", formatTime(message.CreatedAt), topicID); err != nil {
		return discussion.Message{}, fmt.Errorf("update topic agent activity: %w", err)
	}
	return message, nil
}

func appendAgentTaskMessage(
	ctx context.Context,
	transaction *sql.Tx,
	taskID string,
	content string,
) (discussion.Message, error) {
	input := discussion.CreateMessageInput{Content: content}.Normalized()
	if err := input.Validate(); err != nil {
		return discussion.Message{}, err
	}
	threadID, err := findOrCreateThread(ctx, transaction, "task_id", taskID)
	if err != nil {
		return discussion.Message{}, err
	}
	message, err := appendMessage(ctx, transaction, threadID, discussion.AuthorAgent, input)
	if err != nil {
		return discussion.Message{}, err
	}
	if _, err := transaction.ExecContext(ctx, "UPDATE tasks SET updated_at = ? WHERE id = ?", formatTime(message.CreatedAt), taskID); err != nil {
		return discussion.Message{}, fmt.Errorf("update task agent activity: %w", err)
	}
	return message, nil
}

func linkTopicMessageTasks(ctx context.Context, transaction *sql.Tx, topicID string, message discussion.Message) error {
	for _, taskID := range message.LinkedTaskIDs {
		if err := requireTask(ctx, transaction, taskID); err != nil {
			return err
		}
		if err := linkMessageTask(ctx, transaction, message.ID, taskID, message.CreatedAt); err != nil {
			return err
		}
		if err := linkTopicTask(ctx, transaction, topicID, taskID, message.ID); err != nil {
			return err
		}
	}
	return nil
}

func linkTaskMessageTasks(ctx context.Context, transaction *sql.Tx, ownerTaskID string, message discussion.Message) error {
	for _, taskID := range message.LinkedTaskIDs {
		if err := requireTask(ctx, transaction, taskID); err != nil {
			return err
		}
		if err := linkMessageTask(ctx, transaction, message.ID, taskID, message.CreatedAt); err != nil {
			return err
		}
		if err := linkTasks(ctx, transaction, ownerTaskID, taskID, message.ID); err != nil {
			return err
		}
	}
	return nil
}

func linkMessageTask(ctx context.Context, transaction *sql.Tx, messageID, taskID string, createdAt time.Time) error {
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO message_task_links(message_id, task_id, created_at)
VALUES (?, ?, ?)`, messageID, taskID, formatTime(createdAt)); err != nil {
		return fmt.Errorf("link message task: %w", err)
	}
	return nil
}

func scanMessages(rows *sql.Rows) ([]discussion.Message, error) {
	defer rows.Close()
	messages := make([]discussion.Message, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		message.LinkedTaskIDs = make([]string, 0)
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	return messages, nil
}

func (store *DiscussionStore) attachLinkedTasks(
	ctx context.Context,
	messages []discussion.Message,
	subjectColumn, subjectID string,
) error {
	if len(messages) == 0 {
		return nil
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT message_task_links.message_id, message_task_links.task_id
FROM message_task_links
JOIN messages ON messages.id = message_task_links.message_id
JOIN threads ON threads.id = messages.thread_id
WHERE threads.`+subjectColumn+` = ?
ORDER BY messages.sequence, message_task_links.created_at`, subjectID)
	if err != nil {
		return fmt.Errorf("list message task links: %w", err)
	}
	defer rows.Close()
	byID := make(map[string]*discussion.Message, len(messages))
	for index := range messages {
		byID[messages[index].ID] = &messages[index]
	}
	for rows.Next() {
		var messageID, taskID string
		if err := rows.Scan(&messageID, &taskID); err != nil {
			return fmt.Errorf("scan message task link: %w", err)
		}
		if message := byID[messageID]; message != nil {
			message.LinkedTaskIDs = append(message.LinkedTaskIDs, taskID)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate message task links: %w", err)
	}
	return nil
}

func scanMessage(scanner rowScanner) (discussion.Message, error) {
	var message discussion.Message
	var createdAt string
	if err := scanner.Scan(
		&message.ID, &message.ThreadID, &message.Sequence, &message.AuthorKind,
		&message.Content, &createdAt,
	); err != nil {
		return discussion.Message{}, fmt.Errorf("scan message: %w", err)
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return discussion.Message{}, fmt.Errorf("parse message created time: %w", err)
	}
	message.CreatedAt = parsed
	return message, nil
}
