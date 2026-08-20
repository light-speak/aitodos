package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/light-speak/aitodos/internal/domain/relation"
)

var ErrSelfTaskLink = errors.New("task cannot link to itself")

const relatedTaskColumns = `
tasks.id, tasks.task_key, tasks.title, tasks.title_source, tasks.title_locked,
tasks.description, tasks.acceptance_criteria,
tasks.status, tasks.priority, tasks.target_branch, tasks.base_commit_sha,
tasks.current_workspace_id, tasks.latest_run_id,
COALESCE(tasks.source_plan_revision_id, ''), COALESCE(tasks.source_plan_task_draft_id, ''),
tasks.assessment_input_version, tasks.version,
tasks.created_at, tasks.updated_at`

const relatedTopicColumns = `
topics.id, topics.topic_key, topics.title, topics.description, topics.status,
topics.current_plan_id, topics.current_summary_id, topics.version,
topics.created_at, topics.updated_at`

// RelationStore 使用真实外键持久化 Topic 与 Task、Task 与 Task 的关联。
type RelationStore struct {
	database *sql.DB
}

// NewRelationStore 创建关联持久化服务。
func NewRelationStore(database *sql.DB) *RelationStore {
	return &RelationStore{database: database}
}

// LinkTopicTask 幂等关联 Topic 与 Task。
func (store *RelationStore) LinkTopicTask(ctx context.Context, topicID, taskID string) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin topic task link: %w", err)
	}
	defer transaction.Rollback()
	if err := requireTopic(ctx, transaction, topicID); err != nil {
		return err
	}
	if err := requireTask(ctx, transaction, taskID); err != nil {
		return err
	}
	if err := linkTopicTask(ctx, transaction, topicID, taskID, ""); err != nil {
		return err
	}
	return commitRelation(transaction)
}

// UnlinkTopicTask 移除 Topic 与 Task 的直接关联，不删除历史消息引用。
func (store *RelationStore) UnlinkTopicTask(ctx context.Context, topicID, taskID string) error {
	if err := requireTopic(ctx, store.database, topicID); err != nil {
		return err
	}
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return err
	}
	if _, err := store.database.ExecContext(ctx, "DELETE FROM topic_task_links WHERE topic_id = ? AND task_id = ?", topicID, taskID); err != nil {
		return fmt.Errorf("unlink topic task: %w", err)
	}
	return nil
}

// ListTopicTasks 返回 Topic 的当前 Task 关联。
func (store *RelationStore) ListTopicTasks(ctx context.Context, topicID string) ([]relation.TaskAssociation, error) {
	if err := requireTopic(ctx, store.database, topicID); err != nil {
		return nil, err
	}
	rows, err := store.database.QueryContext(ctx, `SELECT `+relatedTaskColumns+`,
       topic_task_links.source_message_id, topic_task_links.created_at
FROM topic_task_links
JOIN tasks ON tasks.id = topic_task_links.task_id
WHERE topic_task_links.topic_id = ?
ORDER BY topic_task_links.created_at, tasks.task_key`, topicID)
	if err != nil {
		return nil, fmt.Errorf("list topic task links: %w", err)
	}
	return scanTaskAssociations(rows)
}

// ListTaskTopics 返回 Task 关联的 Topic。
func (store *RelationStore) ListTaskTopics(ctx context.Context, taskID string) ([]relation.TopicAssociation, error) {
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return nil, err
	}
	rows, err := store.database.QueryContext(ctx, `SELECT `+relatedTopicColumns+`,
       topic_task_links.source_message_id, topic_task_links.created_at
FROM topic_task_links
JOIN topics ON topics.id = topic_task_links.topic_id
WHERE topic_task_links.task_id = ?
ORDER BY topic_task_links.created_at, topics.topic_key`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task topic links: %w", err)
	}
	return scanTopicAssociations(rows)
}

// LinkTasks 幂等创建对称的 Task 关联。
func (store *RelationStore) LinkTasks(ctx context.Context, taskID, relatedTaskID string) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin task link: %w", err)
	}
	defer transaction.Rollback()
	if err := requireTask(ctx, transaction, taskID); err != nil {
		return err
	}
	if err := requireTask(ctx, transaction, relatedTaskID); err != nil {
		return err
	}
	if err := linkTasks(ctx, transaction, taskID, relatedTaskID, ""); err != nil {
		return err
	}
	return commitRelation(transaction)
}

// UnlinkTasks 移除两个 Task 的直接关联，不删除历史消息引用。
func (store *RelationStore) UnlinkTasks(ctx context.Context, taskID, relatedTaskID string) error {
	first, second, err := orderedTaskPair(taskID, relatedTaskID)
	if err != nil {
		return err
	}
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return err
	}
	if err := requireTask(ctx, store.database, relatedTaskID); err != nil {
		return err
	}
	if _, err := store.database.ExecContext(ctx, "DELETE FROM task_links WHERE task_a_id = ? AND task_b_id = ?", first, second); err != nil {
		return fmt.Errorf("unlink tasks: %w", err)
	}
	return nil
}

// ListTaskRelations 返回与指定 Task 对称关联的其他 Task。
func (store *RelationStore) ListTaskRelations(ctx context.Context, taskID string) ([]relation.TaskAssociation, error) {
	if err := requireTask(ctx, store.database, taskID); err != nil {
		return nil, err
	}
	rows, err := store.database.QueryContext(ctx, `SELECT `+relatedTaskColumns+`,
       task_links.source_message_id, task_links.created_at
FROM task_links
JOIN tasks ON tasks.id = CASE
    WHEN task_links.task_a_id = ? THEN task_links.task_b_id
    ELSE task_links.task_a_id
END
WHERE task_links.task_a_id = ? OR task_links.task_b_id = ?
ORDER BY task_links.created_at, tasks.task_key`, taskID, taskID, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task links: %w", err)
	}
	return scanTaskAssociations(rows)
}

func linkTopicTask(ctx context.Context, transaction *sql.Tx, topicID, taskID, sourceMessageID string) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO topic_task_links(topic_id, task_id, source_message_id, created_at)
VALUES (?, ?, NULLIF(?, ''), ?)
ON CONFLICT(topic_id, task_id) DO NOTHING`, topicID, taskID, sourceMessageID, formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("link topic task: %w", err)
	}
	return nil
}

func linkTasks(ctx context.Context, transaction *sql.Tx, taskID, relatedTaskID, sourceMessageID string) error {
	first, second, err := orderedTaskPair(taskID, relatedTaskID)
	if err != nil {
		return err
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO task_links(task_a_id, task_b_id, source_message_id, created_at)
VALUES (?, ?, NULLIF(?, ''), ?)
ON CONFLICT(task_a_id, task_b_id) DO NOTHING`, first, second, sourceMessageID, formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("link tasks: %w", err)
	}
	return nil
}

func orderedTaskPair(first, second string) (string, string, error) {
	if first == second {
		return "", "", ErrSelfTaskLink
	}
	if first < second {
		return first, second, nil
	}
	return second, first, nil
}

func scanTaskAssociations(rows *sql.Rows) ([]relation.TaskAssociation, error) {
	defer rows.Close()
	result := make([]relation.TaskAssociation, 0)
	for rows.Next() {
		var item relation.TaskAssociation
		var sourceMessageID sql.NullString
		var createdAt string
		if err := scanTaskAssociation(rows, &item, &sourceMessageID, &createdAt); err != nil {
			return nil, err
		}
		item.SourceMessageID = sourceMessageID.String
		parsed, err := parseTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse task link time: %w", err)
		}
		item.CreatedAt = parsed
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task links: %w", err)
	}
	return result, nil
}

func scanTaskAssociation(scanner rowScanner, item *relation.TaskAssociation, sourceMessageID *sql.NullString, createdAt *string) error {
	var taskCreatedAt, taskUpdatedAt string
	if err := scanner.Scan(
		&item.Task.ID, &item.Task.Key, &item.Task.Title, &item.Task.TitleSource,
		&item.Task.TitleLocked, &item.Task.Description,
		&item.Task.AcceptanceCriteria, &item.Task.Status, &item.Task.Priority,
		&item.Task.TargetBranch, &item.Task.BaseCommitSHA, &item.Task.CurrentWorkspaceID,
		&item.Task.LatestRunID, &item.Task.SourcePlanRevisionID, &item.Task.SourcePlanTaskDraftID,
		&item.Task.AssessmentInputVersion, &item.Task.Version, &taskCreatedAt, &taskUpdatedAt,
		sourceMessageID, createdAt,
	); err != nil {
		return fmt.Errorf("scan task link: %w", err)
	}
	var err error
	if item.Task.CreatedAt, err = parseTime(taskCreatedAt); err != nil {
		return fmt.Errorf("parse linked task created time: %w", err)
	}
	if item.Task.UpdatedAt, err = parseTime(taskUpdatedAt); err != nil {
		return fmt.Errorf("parse linked task updated time: %w", err)
	}
	return nil
}

func scanTopicAssociations(rows *sql.Rows) ([]relation.TopicAssociation, error) {
	defer rows.Close()
	result := make([]relation.TopicAssociation, 0)
	for rows.Next() {
		var item relation.TopicAssociation
		var sourceMessageID sql.NullString
		var createdAt, topicCreatedAt, topicUpdatedAt string
		if err := rows.Scan(
			&item.Topic.ID, &item.Topic.Key, &item.Topic.Title, &item.Topic.Description,
			&item.Topic.Status, &item.Topic.CurrentPlanID, &item.Topic.CurrentSummaryID,
			&item.Topic.Version, &topicCreatedAt, &topicUpdatedAt,
			&sourceMessageID, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan topic link: %w", err)
		}
		var err error
		item.Topic.CreatedAt, err = parseTime(topicCreatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse linked topic created time: %w", err)
		}
		item.Topic.UpdatedAt, err = parseTime(topicUpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse linked topic updated time: %w", err)
		}
		item.SourceMessageID = sourceMessageID.String
		item.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse topic link time: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate topic links: %w", err)
	}
	return result, nil
}

func commitRelation(transaction *sql.Tx) error {
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit relation: %w", err)
	}
	return nil
}

func requireTask(ctx context.Context, queryer rowQueryer, taskID string) error {
	var exists int
	if err := queryer.QueryRowContext(ctx, "SELECT 1 FROM tasks WHERE id = ?", taskID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrTaskNotFound
		}
		return fmt.Errorf("find related task: %w", err)
	}
	return nil
}
