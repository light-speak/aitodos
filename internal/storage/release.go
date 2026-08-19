package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/light-speak/aitodos/internal/domain/release"
)

var (
	ErrReleaseNotFound = errors.New("release not found")
	ErrReleaseConflict = errors.New("release version already targets different git state")
)

// ReleaseStore 持久化 Release 与 Git Tag 创建状态。
type ReleaseStore struct {
	database *sql.DB
}

// NewReleaseStore 创建 Release 持久化服务。
func NewReleaseStore(database *sql.DB) *ReleaseStore {
	return &ReleaseStore{database: database}
}

// Reserve 幂等保留 Release，并固定来源 Branch 与 Commit。
func (store *ReleaseStore) Reserve(
	ctx context.Context,
	input release.CreateInput,
	commitSHA string,
) (release.Release, error) {
	input = input.Normalized()
	existing, err := store.getByVersion(ctx, store.database, input.Version)
	if err == nil {
		existingTaskIDs := slices.Clone(existing.TaskIDs)
		requestedTaskIDs := slices.Clone(input.TaskIDs)
		slices.Sort(existingTaskIDs)
		slices.Sort(requestedTaskIDs)
		if existing.SourceBranch != input.SourceBranch || existing.CommitSHA != commitSHA ||
			!slices.Equal(existingTaskIDs, requestedTaskIDs) {
			return release.Release{}, ErrReleaseConflict
		}
		return existing, nil
	}
	if !errors.Is(err, ErrReleaseNotFound) {
		return release.Release{}, err
	}
	return store.insert(ctx, input, commitSHA)
}

// List 按创建时间倒序返回 Release。
func (store *ReleaseStore) List(ctx context.Context) ([]release.Release, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, version, tag_name, source_branch, commit_sha, status, failure_message,
       created_at, updated_at, tagged_at
FROM releases ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list releases: %w", err)
	}
	defer rows.Close()
	result := make([]release.Release, 0)
	for rows.Next() {
		item, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate releases: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close release rows: %w", err)
	}
	for index := range result {
		if err := store.attachTasks(ctx, &result[index]); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// MarkTagged 标记 annotated tag 已可验证地指向固定 Commit。
func (store *ReleaseStore) MarkTagged(ctx context.Context, id string) (release.Release, error) {
	now := time.Now().UTC()
	if _, err := store.database.ExecContext(ctx, `
UPDATE releases
SET status = ?, failure_message = '', tagged_at = ?, updated_at = ?
WHERE id = ?`, release.StatusTagged, formatTime(now), formatTime(now), id); err != nil {
		return release.Release{}, fmt.Errorf("mark release tagged: %w", err)
	}
	return store.getByID(ctx, store.database, id)
}

// MarkFailed 保存 Tag 创建失败原因。
func (store *ReleaseStore) MarkFailed(ctx context.Context, id, message string) error {
	_, err := store.database.ExecContext(ctx, `
UPDATE releases SET status = ?, failure_message = ?, updated_at = ? WHERE id = ?`,
		release.StatusFailed, message, formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("mark release failed: %w", err)
	}
	return nil
}

func (store *ReleaseStore) insert(ctx context.Context, input release.CreateInput, commitSHA string) (release.Release, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return release.Release{}, fmt.Errorf("begin release reserve: %w", err)
	}
	defer transaction.Rollback()
	for _, taskID := range input.TaskIDs {
		if err := requireTask(ctx, transaction, taskID); err != nil {
			return release.Release{}, err
		}
	}
	id, err := newID()
	if err != nil {
		return release.Release{}, err
	}
	now := time.Now().UTC()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO releases(
    id, version, tag_name, source_branch, commit_sha, status, failure_message,
    created_at, updated_at, tagged_at
) VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, NULL)`,
		id, input.Version, input.TagName(), input.SourceBranch, commitSHA,
		release.StatusCreating, formatTime(now), formatTime(now),
	); err != nil {
		return release.Release{}, fmt.Errorf("insert release: %w", err)
	}
	for _, taskID := range input.TaskIDs {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO release_tasks(release_id, task_id, created_at) VALUES (?, ?, ?)`, id, taskID, formatTime(now)); err != nil {
			return release.Release{}, fmt.Errorf("link release task: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return release.Release{}, fmt.Errorf("commit release reserve: %w", err)
	}
	return store.getByID(ctx, store.database, id)
}

func (store *ReleaseStore) getByID(ctx context.Context, queryer rowQueryer, id string) (release.Release, error) {
	item, err := scanRelease(queryer.QueryRowContext(ctx, `
SELECT id, version, tag_name, source_branch, commit_sha, status, failure_message,
       created_at, updated_at, tagged_at
FROM releases WHERE id = ?`, id))
	if err != nil {
		return release.Release{}, err
	}
	if err := store.attachTasks(ctx, &item); err != nil {
		return release.Release{}, err
	}
	return item, nil
}

func (store *ReleaseStore) getByVersion(ctx context.Context, queryer rowQueryer, version string) (release.Release, error) {
	item, err := scanRelease(queryer.QueryRowContext(ctx, `
SELECT id, version, tag_name, source_branch, commit_sha, status, failure_message,
       created_at, updated_at, tagged_at
FROM releases WHERE version = ?`, version))
	if err != nil {
		return release.Release{}, err
	}
	if err := store.attachTasks(ctx, &item); err != nil {
		return release.Release{}, err
	}
	return item, nil
}

func scanRelease(scanner rowScanner) (release.Release, error) {
	var item release.Release
	var createdAt, updatedAt string
	var taggedAt sql.NullString
	err := scanner.Scan(
		&item.ID, &item.Version, &item.TagName, &item.SourceBranch, &item.CommitSHA,
		&item.Status, &item.FailureMessage, &createdAt, &updatedAt, &taggedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return release.Release{}, ErrReleaseNotFound
	}
	if err != nil {
		return release.Release{}, fmt.Errorf("scan release: %w", err)
	}
	item.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return release.Release{}, err
	}
	item.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return release.Release{}, err
	}
	if taggedAt.Valid {
		parsed, parseErr := parseTime(taggedAt.String)
		err = parseErr
		if err != nil {
			return release.Release{}, err
		}
		item.TaggedAt = &parsed
	}
	item.TaskIDs = make([]string, 0)
	return item, nil
}

func (store *ReleaseStore) attachTasks(ctx context.Context, item *release.Release) error {
	rows, err := store.database.QueryContext(ctx, `
SELECT task_id FROM release_tasks WHERE release_id = ? ORDER BY created_at, task_id`, item.ID)
	if err != nil {
		return fmt.Errorf("list release tasks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return fmt.Errorf("scan release task: %w", err)
		}
		item.TaskIDs = append(item.TaskIDs, taskID)
	}
	return rows.Err()
}
