package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const currentSchemaVersion = 8

// ProjectMetadata 保存当前项目实例的本地身份。
type ProjectMetadata struct {
	InstanceID   string
	Name         string
	RepoRoot     string
	GitCommonDir string
}

// Open 打开数据库、执行迁移并初始化项目元数据。
func Open(ctx context.Context, path string, metadata ProjectMetadata) (*sql.DB, error) {
	database, err := openDatabase(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := upsertProjectMetadata(ctx, database, metadata); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

// OpenExisting 打开已经初始化的项目数据库。
func OpenExisting(ctx context.Context, path string) (*sql.DB, error) {
	database, err := openDatabase(ctx, path)
	if err != nil {
		return nil, err
	}
	if _, err := ReadProjectMetadata(ctx, database); err != nil {
		database.Close()
		return nil, fmt.Errorf("read existing project metadata: %w", err)
	}
	return database, nil
}

func openDatabase(ctx context.Context, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	database.SetMaxOpenConns(1)

	if err := configure(ctx, database); err != nil {
		database.Close()
		return nil, err
	}
	if err := migrate(ctx, database); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func configure(ctx context.Context, database *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	}
	for _, statement := range pragmas {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure sqlite with %q: %w", statement, err)
		}
	}
	return nil
}

func migrate(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
)`); err != nil {
		return fmt.Errorf("create schema migrations table: %w", err)
	}

	version, err := schemaVersion(ctx, database)
	if err != nil {
		return err
	}
	for next := version + 1; next <= currentSchemaVersion; next++ {
		if err := applyMigration(ctx, database, next); err != nil {
			return err
		}
	}
	return nil
}

func schemaVersion(ctx context.Context, database *sql.DB) (int, error) {
	var version sql.NullInt64
	if err := database.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	if !version.Valid {
		return 0, nil
	}
	return int(version.Int64), nil
}

func applyMigration(ctx context.Context, database *sql.DB, version int) error {
	statement, ok := migrationStatement(version)
	if !ok {
		return fmt.Errorf("unsupported schema migration: %d", version)
	}

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration %d: %w", version, err)
	}
	defer transaction.Rollback()

	if _, err := transaction.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("apply schema migration %d: %w", version, err)
	}
	if _, err := transaction.ExecContext(
		ctx,
		"INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)",
		version,
		time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("record schema migration %d: %w", version, err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit schema migration %d: %w", version, err)
	}
	return nil
}

func migrationStatement(version int) (string, bool) {
	switch version {
	case 1:
		return `CREATE TABLE project_metadata (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    instance_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    repo_root TEXT NOT NULL,
    git_common_dir TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
)`, true
	case 2:
		return `CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    task_key TEXT NOT NULL UNIQUE,
    title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 200),
    description TEXT NOT NULL,
    acceptance_criteria TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN (
        'BACKLOG', 'READY', 'RUNNING', 'REVIEW', 'ACCEPTED',
        'CHANGES_REQUESTED', 'BLOCKED', 'CANCELLED'
    )),
    priority INTEGER NOT NULL DEFAULT 0,
    target_branch TEXT NOT NULL DEFAULT '',
    base_commit_sha TEXT NOT NULL DEFAULT '',
    current_workspace_id TEXT NOT NULL DEFAULT '',
    latest_run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL CHECK (version >= 1)
);

CREATE INDEX tasks_board_order_idx
ON tasks(status, priority DESC, created_at ASC);

CREATE TABLE task_events (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    event_type TEXT NOT NULL CHECK (event_type IN ('TASK_CREATED', 'TASK_STATUS_CHANGED')),
    payload_json TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    UNIQUE(task_id, sequence)
);

CREATE INDEX task_events_timeline_idx
ON task_events(task_id, sequence);`, true
	case 3:
		return `CREATE TABLE topics (
    id TEXT PRIMARY KEY,
    topic_key TEXT NOT NULL UNIQUE,
    title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 200),
    description TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN (
        'OPEN', 'NEEDS_CLARIFICATION', 'PLAN_REVIEW', 'PLANNED', 'CLOSED'
    )),
    current_plan_id TEXT NOT NULL DEFAULT '',
    current_summary_id TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL CHECK (version >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX topics_activity_idx
ON topics(status, updated_at DESC);

CREATE TABLE topic_events (
    id TEXT PRIMARY KEY,
    topic_id TEXT NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    event_type TEXT NOT NULL CHECK (event_type IN ('TOPIC_CREATED', 'TOPIC_STATUS_CHANGED')),
    payload_json TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    UNIQUE(topic_id, sequence)
);

CREATE INDEX topic_events_timeline_idx
ON topic_events(topic_id, sequence);`, true
	case 4:
		return `CREATE TABLE threads (
    id TEXT PRIMARY KEY,
    topic_id TEXT REFERENCES topics(id) ON DELETE CASCADE,
    task_id TEXT REFERENCES tasks(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (
        (topic_id IS NOT NULL AND task_id IS NULL) OR
        (topic_id IS NULL AND task_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX threads_topic_unique_idx
ON threads(topic_id) WHERE topic_id IS NOT NULL;

CREATE UNIQUE INDEX threads_task_unique_idx
ON threads(task_id) WHERE task_id IS NOT NULL;

CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    author_kind TEXT NOT NULL CHECK (author_kind IN ('HUMAN', 'AGENT', 'SYSTEM')),
    content TEXT NOT NULL CHECK (length(trim(content)) BETWEEN 1 AND 20000),
    created_at TEXT NOT NULL,
    UNIQUE(thread_id, sequence)
);

CREATE INDEX messages_thread_timeline_idx
ON messages(thread_id, sequence);`, true
	case 5:
		return `CREATE TABLE artifacts (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('IMAGE')),
    original_name TEXT NOT NULL,
    original_media_type TEXT NOT NULL,
    original_relative_path TEXT NOT NULL UNIQUE,
    original_size INTEGER NOT NULL CHECK (original_size > 0),
    original_sha256 TEXT NOT NULL,
    optimized_media_type TEXT NOT NULL,
    optimized_relative_path TEXT NOT NULL UNIQUE,
    optimized_size INTEGER NOT NULL CHECK (optimized_size > 0),
    optimized_sha256 TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX artifacts_created_idx
ON artifacts(created_at DESC);`, true
	case 6:
		return `CREATE TABLE message_task_links (
    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    PRIMARY KEY(message_id, task_id)
);

CREATE INDEX message_task_links_task_idx
ON message_task_links(task_id, created_at);

CREATE TABLE topic_task_links (
    topic_id TEXT NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    source_message_id TEXT REFERENCES messages(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(topic_id, task_id)
);

CREATE INDEX topic_task_links_task_idx
ON topic_task_links(task_id, created_at);

CREATE TABLE task_links (
    task_a_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    task_b_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    source_message_id TEXT REFERENCES messages(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    CHECK(task_a_id < task_b_id),
    PRIMARY KEY(task_a_id, task_b_id)
);

CREATE INDEX task_links_b_idx
ON task_links(task_b_id, created_at);`, true
	case 7:
		return `CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL UNIQUE REFERENCES tasks(id) ON DELETE CASCADE,
    path TEXT NOT NULL UNIQUE,
    branch_name TEXT NOT NULL UNIQUE,
    target_branch TEXT NOT NULL,
    base_commit_sha TEXT NOT NULL,
    head_sha TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK (state IN ('PROVISIONING', 'READY', 'DIRTY', 'QUARANTINED', 'ERROR')),
    dirty INTEGER NOT NULL DEFAULT 0 CHECK (dirty IN (0, 1)),
    failure_message TEXT NOT NULL DEFAULT '',
    last_verified_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX workspaces_state_idx ON workspaces(state, updated_at);`, true
	case 8:
		return `CREATE TABLE releases (
    id TEXT PRIMARY KEY,
    version TEXT NOT NULL UNIQUE,
    tag_name TEXT NOT NULL UNIQUE,
    source_branch TEXT NOT NULL,
    commit_sha TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('CREATING', 'TAGGED', 'FAILED')),
    failure_message TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    tagged_at TEXT
);

CREATE INDEX releases_created_idx ON releases(created_at DESC);

CREATE TABLE release_tasks (
    release_id TEXT NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    PRIMARY KEY(release_id, task_id)
);

CREATE INDEX release_tasks_task_idx ON release_tasks(task_id, created_at);`, true
	default:
		return "", false
	}
}

func upsertProjectMetadata(ctx context.Context, database *sql.DB, metadata ProjectMetadata) error {
	if metadata.InstanceID == "" {
		return errors.New("project instance ID is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := database.ExecContext(ctx, `
INSERT INTO project_metadata (
    id, instance_id, name, repo_root, git_common_dir, created_at, updated_at
) VALUES (1, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    repo_root = excluded.repo_root,
    git_common_dir = excluded.git_common_dir,
    updated_at = excluded.updated_at`,
		metadata.InstanceID,
		metadata.Name,
		metadata.RepoRoot,
		metadata.GitCommonDir,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("upsert project metadata: %w", err)
	}
	return nil
}

// ReadProjectMetadata 读取当前项目的本地身份。
func ReadProjectMetadata(ctx context.Context, database *sql.DB) (ProjectMetadata, error) {
	var metadata ProjectMetadata
	err := database.QueryRowContext(ctx, `
SELECT instance_id, name, repo_root, git_common_dir
FROM project_metadata
WHERE id = 1`).Scan(
		&metadata.InstanceID,
		&metadata.Name,
		&metadata.RepoRoot,
		&metadata.GitCommonDir,
	)
	if err != nil {
		return ProjectMetadata{}, fmt.Errorf("query project metadata: %w", err)
	}
	return metadata, nil
}
