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

const currentSchemaVersion = 31

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
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
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
	if version == 16 || version == 17 || version == 21 || version == 30 {
		return applyRebuildMigration(ctx, database, version, statement)
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

func applyRebuildMigration(ctx context.Context, database *sql.DB, version int, statement string) error {
	if _, err := database.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disable foreign keys for migration %d: %w", version, err)
	}
	if _, err := database.ExecContext(ctx, "PRAGMA legacy_alter_table = ON"); err != nil {
		_, _ = database.ExecContext(ctx, "PRAGMA foreign_keys = ON")
		return fmt.Errorf("enable legacy alter table for migration %d: %w", version, err)
	}
	restore := func() error {
		if _, err := database.ExecContext(ctx, "PRAGMA legacy_alter_table = OFF"); err != nil {
			return err
		}
		_, err := database.ExecContext(ctx, "PRAGMA foreign_keys = ON")
		return err
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		_ = restore()
		return fmt.Errorf("begin schema migration %d: %w", version, err)
	}
	rollback := func(cause error) error {
		_ = transaction.Rollback()
		_ = restore()
		return cause
	}
	if _, err := transaction.ExecContext(ctx, statement); err != nil {
		return rollback(fmt.Errorf("apply schema migration %d: %w", version, err))
	}
	if err := checkForeignKeys(ctx, transaction); err != nil {
		return rollback(fmt.Errorf("validate schema migration %d: %w", version, err))
	}
	if _, err := transaction.ExecContext(ctx,
		"INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)",
		version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return rollback(fmt.Errorf("record schema migration %d: %w", version, err))
	}
	if err := transaction.Commit(); err != nil {
		_ = transaction.Rollback()
		_ = restore()
		return fmt.Errorf("commit schema migration %d: %w", version, err)
	}
	if err := restore(); err != nil {
		return fmt.Errorf("restore sqlite constraints after migration %d: %w", version, err)
	}
	return nil
}

func checkForeignKeys(ctx context.Context, transaction *sql.Tx) error {
	rows, err := transaction.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent string
		var rowID sql.NullInt64
		var foreignKeyID int
		if err := rows.Scan(&table, &rowID, &parent, &foreignKeyID); err != nil {
			return err
		}
		return fmt.Errorf("foreign key violation in %s row %v referencing %s constraint %d", table, rowID, parent, foreignKeyID)
	}
	return rows.Err()
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
	case 9:
		return `CREATE TABLE task_reviews (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    decision TEXT NOT NULL CHECK (decision IN ('ACCEPTED', 'REJECTED')),
    comment TEXT NOT NULL,
    commit_sha TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE INDEX task_reviews_task_idx ON task_reviews(task_id, created_at DESC);`, true
	case 10:
		return `UPDATE tasks
SET priority = CASE
    WHEN priority = 0 THEN 2
    WHEN priority > 3 THEN 0
    WHEN priority < 0 THEN 3
    ELSE priority
END;

DROP INDEX IF EXISTS tasks_board_order_idx;
CREATE INDEX tasks_board_order_idx
ON tasks(status, priority ASC, created_at ASC);`, true
	case 11:
		return `CREATE TABLE agent_profiles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    role TEXT NOT NULL UNIQUE CHECK (role IN ('PLANNER', 'IMPLEMENTER', 'REVISION', 'REVIEWER')),
    current_revision_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY(current_revision_id) REFERENCES agent_profile_revisions(id) DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE agent_profile_revisions (
    id TEXT PRIMARY KEY,
    profile_id TEXT NOT NULL REFERENCES agent_profiles(id) ON DELETE RESTRICT,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    instructions TEXT NOT NULL,
    adapter TEXT NOT NULL,
    command TEXT NOT NULL,
    args_json TEXT NOT NULL,
    model TEXT NOT NULL,
    max_input_tokens INTEGER NOT NULL CHECK (max_input_tokens >= 4096),
    reserved_output_tokens INTEGER NOT NULL CHECK (
        reserved_output_tokens >= 512 AND reserved_output_tokens < max_input_tokens
    ),
    recent_message_limit INTEGER NOT NULL CHECK (recent_message_limit BETWEEN 0 AND 200),
    retrieval_limit INTEGER NOT NULL CHECK (retrieval_limit BETWEEN 0 AND 100),
    workspace_policy TEXT NOT NULL CHECK (workspace_policy IN ('NONE', 'READ_ONLY', 'WRITE_TASK')),
    approval_policy TEXT NOT NULL CHECK (approval_policy IN ('READ_ONLY', 'WORKSPACE_WRITE')),
    timeout_seconds INTEGER NOT NULL CHECK (timeout_seconds BETWEEN 30 AND 86400),
    created_at TEXT NOT NULL,
    UNIQUE(profile_id, revision)
);

CREATE INDEX agent_profile_revisions_history_idx
ON agent_profile_revisions(profile_id, revision DESC);

INSERT INTO agent_profiles(id, name, role, current_revision_id, created_at, updated_at) VALUES
('profile-planner', '规划 Agent', 'PLANNER', 'profile-planner-r1', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
('profile-implementer', '实现 Agent', 'IMPLEMENTER', 'profile-implementer-r1', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
('profile-revision', '修订 Agent', 'REVISION', 'profile-revision-r1', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
('profile-reviewer', '审查 Agent', 'REVIEWER', 'profile-reviewer-r1', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

INSERT INTO agent_profile_revisions(
    id, profile_id, revision, instructions, adapter, command, args_json, model,
    max_input_tokens, reserved_output_tokens, recent_message_limit, retrieval_limit,
    workspace_policy, approval_policy, timeout_seconds, created_at
) VALUES
('profile-planner-r1', 'profile-planner', 1, '分析 Topic，提出必要澄清，生成可审核的 Plan、Task 草案、验收标准、测试项和估算。未经人工批准不得执行正式 Task。', 'generic', '', '[]', '', 64000, 12000, 30, 10, 'NONE', 'READ_ONLY', 3600, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
('profile-implementer-r1', 'profile-implementer', 1, '只实现当前 Task，在受管 Workspace 内工作，满足验收标准并报告实际执行的测试证据。', 'generic', '', '[]', '', 64000, 12000, 20, 8, 'WRITE_TASK', 'WORKSPACE_WRITE', 3600, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
('profile-revision-r1', 'profile-revision', 1, '针对最近一次驳回意见修订当前 Task，保留正确改动，重新运行受影响测试并报告证据。', 'generic', '', '[]', '', 64000, 12000, 20, 8, 'WRITE_TASK', 'WORKSPACE_WRITE', 3600, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
('profile-reviewer-r1', 'profile-reviewer', 1, '只读审查当前 Task 的验收标准、Diff 和测试证据，明确区分已验证事实与推断。', 'generic', '', '[]', '', 48000, 10000, 15, 8, 'READ_ONLY', 'READ_ONLY', 1800, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

CREATE TABLE project_agent_defaults (
    purpose TEXT PRIMARY KEY CHECK (purpose IN ('PLANNING', 'IMPLEMENTATION', 'REVISION', 'REVIEW')),
    profile_id TEXT NOT NULL REFERENCES agent_profiles(id) ON DELETE RESTRICT
);

INSERT INTO project_agent_defaults(purpose, profile_id) VALUES
('PLANNING', 'profile-planner'),
('IMPLEMENTATION', 'profile-implementer'),
('REVISION', 'profile-revision'),
('REVIEW', 'profile-reviewer');`, true
	case 12:
		return `CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    purpose TEXT NOT NULL CHECK (purpose IN ('PLANNING', 'IMPLEMENTATION', 'REVISION', 'REVIEW')),
    topic_id TEXT REFERENCES topics(id) ON DELETE RESTRICT,
    task_id TEXT REFERENCES tasks(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN (
        'CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING',
        'SUCCEEDED', 'FAILED', 'CANCELLED', 'TIMED_OUT', 'LOST'
    )),
    profile_revision_id TEXT NOT NULL REFERENCES agent_profile_revisions(id) ON DELETE RESTRICT,
    retry_of_run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    claim_token_hash TEXT NOT NULL,
    lease_generation INTEGER NOT NULL CHECK (lease_generation >= 1),
    lease_expires_at TEXT NOT NULL,
    runner_pid INTEGER,
    runner_started_at TEXT,
    run_nonce TEXT NOT NULL,
    queued_at TEXT NOT NULL,
    claimed_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    exit_code INTEGER,
    failure_kind TEXT NOT NULL DEFAULT '',
    failure_code TEXT NOT NULL DEFAULT '',
    failure_message TEXT NOT NULL DEFAULT '',
    failure_retryable INTEGER CHECK (failure_retryable IS NULL OR failure_retryable IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (
        (purpose = 'PLANNING' AND topic_id IS NOT NULL AND task_id IS NULL) OR
        (purpose != 'PLANNING' AND topic_id IS NULL AND task_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX runs_one_active_task_idx
ON runs(task_id) WHERE task_id IS NOT NULL AND status IN ('CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING');

CREATE UNIQUE INDEX runs_one_active_topic_idx
ON runs(topic_id) WHERE topic_id IS NOT NULL AND status IN ('CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING');

CREATE INDEX runs_status_queue_idx ON runs(status, queued_at);
CREATE INDEX runs_task_history_idx ON runs(task_id, created_at DESC);`, true
	case 13:
		return `CREATE TABLE task_estimates (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    points INTEGER NOT NULL CHECK (points IN (1, 2, 3, 5, 8, 13)),
    remaining_points INTEGER NOT NULL CHECK (remaining_points BETWEEN 0 AND points),
    confidence REAL NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    rationale TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('AI', 'HUMAN')),
    source_run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    CHECK (source != 'AI' OR source_run_id IS NOT NULL),
    UNIQUE(task_id, revision)
);

CREATE INDEX task_estimates_history_idx ON task_estimates(task_id, revision DESC);

CREATE TABLE task_test_cases (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    required INTEGER NOT NULL CHECK (required IN (0, 1)),
    sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
    created_by TEXT NOT NULL CHECK (created_by IN ('HUMAN', 'AGENT')),
    source_run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (created_by != 'AGENT' OR source_run_id IS NOT NULL)
);

CREATE INDEX task_test_cases_order_idx ON task_test_cases(task_id, sort_order, created_at);

CREATE TABLE task_test_results (
    id TEXT PRIMARY KEY,
    test_case_id TEXT NOT NULL REFERENCES task_test_cases(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    outcome TEXT NOT NULL CHECK (outcome IN ('PASSED', 'FAILED', 'BLOCKED')),
    evidence_kind TEXT NOT NULL CHECK (evidence_kind IN ('COMMAND', 'HUMAN', 'AGENT_REPORT')),
    summary TEXT NOT NULL,
    command TEXT NOT NULL DEFAULT '',
    artifact_ref TEXT NOT NULL DEFAULT '',
    source_run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    CHECK (evidence_kind != 'COMMAND' OR length(trim(command)) > 0),
    CHECK (evidence_kind != 'AGENT_REPORT' OR source_run_id IS NOT NULL)
);

CREATE INDEX task_test_results_latest_idx
ON task_test_results(test_case_id, created_at DESC, id DESC);`, true
	case 14:
		return `CREATE TABLE run_artifacts (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('PROMPT', 'CONTEXT_MANIFEST', 'STDOUT', 'STDERR', 'RESULT')),
    relative_path TEXT NOT NULL UNIQUE,
    sha256 TEXT NOT NULL,
    size INTEGER NOT NULL CHECK (size >= 0),
    truncated INTEGER NOT NULL CHECK (truncated IN (0, 1)),
    created_at TEXT NOT NULL,
    UNIQUE(run_id, kind)
);

CREATE INDEX run_artifacts_run_idx ON run_artifacts(run_id, kind);`, true
	case 15:
		return `INSERT INTO task_events(id, task_id, sequence, event_type, payload_json, occurred_at)
SELECT lower(hex(randomblob(16))), id, version + 1, 'TASK_STATUS_CHANGED',
       '{"schema_version":1,"command":"MIGRATE_AUTO_READY","from":"BACKLOG","to":"READY"}',
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM tasks WHERE status = 'BACKLOG';

UPDATE tasks
SET status = 'READY', version = version + 1, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE status = 'BACKLOG';`, true
	case 16:
		return `ALTER TABLE tasks ADD COLUMN title_source TEXT NOT NULL DEFAULT 'HUMAN'
    CHECK (title_source IN ('PROVISIONAL', 'AI', 'HUMAN'));
ALTER TABLE tasks ADD COLUMN title_locked INTEGER NOT NULL DEFAULT 1
    CHECK (title_locked IN (0, 1));
ALTER TABLE tasks ADD COLUMN assessment_input_version INTEGER NOT NULL DEFAULT 1
    CHECK (assessment_input_version >= 1);

ALTER TABLE task_events RENAME TO task_events_v15;
CREATE TABLE task_events (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    event_type TEXT NOT NULL CHECK (event_type IN (
        'TASK_CREATED', 'TASK_STATUS_CHANGED', 'TASK_TITLE_CHANGED'
    )),
    payload_json TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    UNIQUE(task_id, sequence)
);
INSERT INTO task_events SELECT * FROM task_events_v15;
DROP TABLE task_events_v15;
CREATE INDEX task_events_timeline_idx ON task_events(task_id, sequence);

ALTER TABLE agent_profiles RENAME TO agent_profiles_v15;
CREATE TABLE agent_profiles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    role TEXT NOT NULL UNIQUE CHECK (role IN (
        'PLANNER', 'TRIAGER', 'IMPLEMENTER', 'REVISION', 'REVIEWER'
    )),
    current_revision_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY(current_revision_id) REFERENCES agent_profile_revisions(id) DEFERRABLE INITIALLY DEFERRED
);
INSERT INTO agent_profiles SELECT * FROM agent_profiles_v15;
DROP TABLE agent_profiles_v15;

ALTER TABLE project_agent_defaults RENAME TO project_agent_defaults_v15;
CREATE TABLE project_agent_defaults (
    purpose TEXT PRIMARY KEY CHECK (purpose IN (
        'PLANNING', 'TRIAGE', 'IMPLEMENTATION', 'REVISION', 'REVIEW'
    )),
    profile_id TEXT NOT NULL REFERENCES agent_profiles(id) ON DELETE RESTRICT
);
INSERT INTO project_agent_defaults SELECT * FROM project_agent_defaults_v15;
DROP TABLE project_agent_defaults_v15;

INSERT INTO agent_profiles(id, name, role, current_revision_id, created_at, updated_at)
VALUES ('profile-triager', '任务评估 Agent', 'TRIAGER', 'profile-triager-r1',
        strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
INSERT INTO agent_profile_revisions(
    id, profile_id, revision, instructions, adapter, command, args_json, model,
    max_input_tokens, reserved_output_tokens, recent_message_limit, retrieval_limit,
    workspace_policy, approval_policy, timeout_seconds, created_at
) VALUES (
    'profile-triager-r1', 'profile-triager', 1,
    '只评估当前正式 Task：生成简洁动宾标题，按固定六维标准给出 0 到 4 的原始评分、置信度、依据、假设和拆分建议。不得修改代码、优先级、模型或权限。',
    'generic', '', '[]', '', 32000, 6000, 20, 8, 'NONE', 'READ_ONLY', 900,
    strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
);
INSERT INTO project_agent_defaults(purpose, profile_id) VALUES ('TRIAGE', 'profile-triager');

ALTER TABLE runs RENAME TO runs_v15;
CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    purpose TEXT NOT NULL CHECK (purpose IN (
        'PLANNING', 'TRIAGE', 'IMPLEMENTATION', 'REVISION', 'REVIEW'
    )),
    topic_id TEXT REFERENCES topics(id) ON DELETE RESTRICT,
    task_id TEXT REFERENCES tasks(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN (
        'CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING',
        'SUCCEEDED', 'FAILED', 'CANCELLED', 'TIMED_OUT', 'LOST'
    )),
    profile_revision_id TEXT NOT NULL REFERENCES agent_profile_revisions(id) ON DELETE RESTRICT,
    retry_of_run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    claim_token_hash TEXT NOT NULL,
    lease_generation INTEGER NOT NULL CHECK (lease_generation >= 1),
    lease_expires_at TEXT NOT NULL,
    runner_pid INTEGER,
    runner_started_at TEXT,
    run_nonce TEXT NOT NULL,
    queued_at TEXT NOT NULL,
    claimed_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    exit_code INTEGER,
    failure_kind TEXT NOT NULL DEFAULT '',
    failure_code TEXT NOT NULL DEFAULT '',
    failure_message TEXT NOT NULL DEFAULT '',
    failure_retryable INTEGER CHECK (failure_retryable IS NULL OR failure_retryable IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    subject_version INTEGER NOT NULL DEFAULT 0 CHECK (subject_version >= 0),
    CHECK (
        (purpose = 'PLANNING' AND topic_id IS NOT NULL AND task_id IS NULL) OR
        (purpose != 'PLANNING' AND topic_id IS NULL AND task_id IS NOT NULL)
    )
);
INSERT INTO runs(
    id, purpose, topic_id, task_id, status, profile_revision_id, retry_of_run_id,
    claim_token_hash, lease_generation, lease_expires_at, runner_pid, runner_started_at,
    run_nonce, queued_at, claimed_at, started_at, finished_at, exit_code,
    failure_kind, failure_code, failure_message, failure_retryable, created_at, updated_at,
    subject_version
)
SELECT id, purpose, topic_id, task_id, status, profile_revision_id, retry_of_run_id,
       claim_token_hash, lease_generation, lease_expires_at, runner_pid, runner_started_at,
       run_nonce, queued_at, claimed_at, started_at, finished_at, exit_code,
       failure_kind, failure_code, failure_message, failure_retryable, created_at, updated_at,
       CASE WHEN task_id IS NULL THEN 0 ELSE 1 END
FROM runs_v15;
DROP TABLE runs_v15;
CREATE UNIQUE INDEX runs_one_active_task_idx
ON runs(task_id) WHERE task_id IS NOT NULL AND status IN ('CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING');
CREATE UNIQUE INDEX runs_one_active_topic_idx
ON runs(topic_id) WHERE topic_id IS NOT NULL AND status IN ('CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING');
CREATE INDEX runs_status_queue_idx ON runs(status, queued_at);
CREATE INDEX runs_task_history_idx ON runs(task_id, created_at DESC);

CREATE TABLE task_assessments (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    task_assessment_version INTEGER NOT NULL CHECK (task_assessment_version >= 1),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    suggested_title TEXT NOT NULL,
    applied_title TEXT NOT NULL,
    technical_complexity INTEGER NOT NULL CHECK (technical_complexity BETWEEN 0 AND 4),
    requirement_uncertainty INTEGER NOT NULL CHECK (requirement_uncertainty BETWEEN 0 AND 4),
    change_scope INTEGER NOT NULL CHECK (change_scope BETWEEN 0 AND 4),
    validation_burden INTEGER NOT NULL CHECK (validation_burden BETWEEN 0 AND 4),
    human_dependency INTEGER NOT NULL CHECK (human_dependency BETWEEN 0 AND 4),
    risk_and_reversibility INTEGER NOT NULL CHECK (risk_and_reversibility BETWEEN 0 AND 4),
    weighted_score REAL NOT NULL CHECK (weighted_score BETWEEN 0 AND 4),
    complexity TEXT NOT NULL CHECK (complexity IN ('C1', 'C2', 'C3', 'C4', 'C5')),
    autonomy TEXT NOT NULL CHECK (autonomy IN ('A0', 'A1', 'A2', 'A3')),
    confidence REAL NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    rationale TEXT NOT NULL,
    assumptions_json TEXT NOT NULL,
    split_recommended INTEGER NOT NULL CHECK (split_recommended IN (0, 1)),
    split_rationale TEXT NOT NULL,
    source_run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    UNIQUE(task_id, revision),
    UNIQUE(source_run_id)
);
CREATE INDEX task_assessments_current_idx
ON task_assessments(task_id, revision DESC);`, true
	case 17:
		return `ALTER TABLE runs RENAME TO runs_v16;
CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    purpose TEXT NOT NULL CHECK (purpose IN (
        'PLANNING', 'TRIAGE', 'IMPLEMENTATION', 'REVISION', 'REVIEW'
    )),
    topic_id TEXT REFERENCES topics(id) ON DELETE RESTRICT,
    task_id TEXT REFERENCES tasks(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN (
        'CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING', 'NEEDS_INPUT',
        'SUCCEEDED', 'FAILED', 'CANCELLED', 'TIMED_OUT', 'LOST'
    )),
    profile_revision_id TEXT NOT NULL REFERENCES agent_profile_revisions(id) ON DELETE RESTRICT,
    retry_of_run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    continuation_of_run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    claim_token_hash TEXT NOT NULL,
    lease_generation INTEGER NOT NULL CHECK (lease_generation >= 1),
    lease_expires_at TEXT NOT NULL,
    runner_pid INTEGER,
    runner_started_at TEXT,
    run_nonce TEXT NOT NULL,
    queued_at TEXT NOT NULL,
    claimed_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    exit_code INTEGER,
    failure_kind TEXT NOT NULL DEFAULT '',
    failure_code TEXT NOT NULL DEFAULT '',
    failure_message TEXT NOT NULL DEFAULT '',
    failure_retryable INTEGER CHECK (failure_retryable IS NULL OR failure_retryable IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    subject_version INTEGER NOT NULL DEFAULT 0 CHECK (subject_version >= 0),
    CHECK (
        (purpose = 'PLANNING' AND topic_id IS NOT NULL AND task_id IS NULL) OR
        (purpose != 'PLANNING' AND topic_id IS NULL AND task_id IS NOT NULL)
    )
);
INSERT INTO runs(
    id, purpose, topic_id, task_id, status, profile_revision_id, retry_of_run_id,
    continuation_of_run_id, claim_token_hash, lease_generation, lease_expires_at,
    runner_pid, runner_started_at, run_nonce, queued_at, claimed_at, started_at,
    finished_at, exit_code, failure_kind, failure_code, failure_message,
    failure_retryable, created_at, updated_at, subject_version
)
SELECT id, purpose, topic_id, task_id, status, profile_revision_id, retry_of_run_id,
       NULL, claim_token_hash, lease_generation, lease_expires_at, runner_pid,
       runner_started_at, run_nonce, queued_at, claimed_at, started_at, finished_at,
       exit_code, failure_kind, failure_code, failure_message, failure_retryable,
       created_at, updated_at, subject_version
FROM runs_v16;
DROP TABLE runs_v16;
CREATE UNIQUE INDEX runs_one_active_task_idx
ON runs(task_id) WHERE task_id IS NOT NULL AND status IN ('CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING');
CREATE UNIQUE INDEX runs_one_active_topic_idx
ON runs(topic_id) WHERE topic_id IS NOT NULL AND status IN ('CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING');
CREATE INDEX runs_status_queue_idx ON runs(status, queued_at);
CREATE INDEX runs_task_history_idx ON runs(task_id, created_at DESC);

CREATE TABLE clarifications (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    source_run_id TEXT NOT NULL UNIQUE REFERENCES runs(id) ON DELETE RESTRICT,
    continuation_run_id TEXT UNIQUE REFERENCES runs(id) ON DELETE RESTRICT,
    continuation_purpose TEXT NOT NULL CHECK (continuation_purpose IN ('IMPLEMENTATION', 'REVISION')),
    category TEXT NOT NULL CHECK (category IN ('REQUIREMENT', 'DECISION', 'ENVIRONMENT', 'VALIDATION')),
    question TEXT NOT NULL CHECK (length(trim(question)) BETWEEN 1 AND 4000),
    options_json TEXT NOT NULL,
    recommended_option_id TEXT NOT NULL DEFAULT '',
    allow_custom_answer INTEGER NOT NULL CHECK (allow_custom_answer IN (0, 1)),
    status TEXT NOT NULL CHECK (status IN ('OPEN', 'ANSWERED')),
    selected_option_id TEXT NOT NULL DEFAULT '',
    custom_answer TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL CHECK (version >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    answered_at TEXT,
    CHECK (
        (status = 'OPEN' AND selected_option_id = '' AND custom_answer = '' AND answered_at IS NULL) OR
        (status = 'ANSWERED' AND ((selected_option_id != '' AND custom_answer = '') OR
                                  (selected_option_id = '' AND custom_answer != '')) AND answered_at IS NOT NULL)
    )
);
CREATE UNIQUE INDEX clarifications_one_open_task_idx
ON clarifications(task_id) WHERE status = 'OPEN';
CREATE INDEX clarifications_open_queue_idx ON clarifications(status, created_at);`, true
	case 18:
		return `CREATE TABLE plans (
    id TEXT PRIMARY KEY,
    plan_key TEXT NOT NULL UNIQUE,
    topic_id TEXT NOT NULL UNIQUE REFERENCES topics(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('IN_REVIEW', 'CHANGES_REQUESTED', 'APPROVED')),
    current_revision_id TEXT NOT NULL,
    approved_revision_id TEXT,
    version INTEGER NOT NULL CHECK (version >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY(current_revision_id) REFERENCES plan_revisions(id) DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY(approved_revision_id) REFERENCES plan_revisions(id) ON DELETE RESTRICT
);

CREATE TABLE plan_revisions (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE RESTRICT,
    revision_number INTEGER NOT NULL CHECK (revision_number >= 1),
    summary TEXT NOT NULL CHECK (length(trim(summary)) BETWEEN 1 AND 20000),
    rationale TEXT NOT NULL,
    risks TEXT NOT NULL,
    source_run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    previous_revision_id TEXT REFERENCES plan_revisions(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    UNIQUE(plan_id, revision_number)
);

CREATE INDEX plan_revisions_history_idx ON plan_revisions(plan_id, revision_number DESC);

CREATE TABLE plan_task_drafts (
    id TEXT PRIMARY KEY,
    plan_revision_id TEXT NOT NULL REFERENCES plan_revisions(id) ON DELETE RESTRICT,
    draft_key TEXT NOT NULL,
    title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 200),
    description TEXT NOT NULL,
    acceptance_criteria TEXT NOT NULL,
    priority INTEGER NOT NULL CHECK (priority BETWEEN 0 AND 3),
    proposed_order INTEGER NOT NULL CHECK (proposed_order >= 0),
    UNIQUE(plan_revision_id, draft_key),
    UNIQUE(plan_revision_id, proposed_order)
);

CREATE TABLE plan_task_draft_test_cases (
    id TEXT PRIMARY KEY,
    task_draft_id TEXT NOT NULL REFERENCES plan_task_drafts(id) ON DELETE RESTRICT,
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    description TEXT NOT NULL,
    required INTEGER NOT NULL CHECK (required IN (0, 1)),
    sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
    UNIQUE(task_draft_id, sort_order)
);

CREATE TABLE plan_reviews (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE RESTRICT,
    plan_revision_id TEXT NOT NULL REFERENCES plan_revisions(id) ON DELETE RESTRICT,
    decision TEXT NOT NULL CHECK (decision IN ('APPROVED', 'CHANGES_REQUESTED')),
    comment TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX plan_reviews_history_idx ON plan_reviews(plan_id, created_at DESC);

ALTER TABLE tasks ADD COLUMN source_plan_revision_id TEXT REFERENCES plan_revisions(id) ON DELETE RESTRICT;
ALTER TABLE tasks ADD COLUMN source_plan_task_draft_id TEXT REFERENCES plan_task_drafts(id) ON DELETE RESTRICT;
CREATE UNIQUE INDEX tasks_source_plan_draft_unique_idx
ON tasks(source_plan_task_draft_id) WHERE source_plan_task_draft_id IS NOT NULL;`, true
	case 19:
		return `CREATE TABLE project_skills (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    source_path TEXT NOT NULL UNIQUE,
    content_sha256 TEXT NOT NULL CHECK (length(content_sha256) = 64),
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    version INTEGER NOT NULL CHECK (version >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE project_mcp_servers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    config_name TEXT NOT NULL UNIQUE,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    version INTEGER NOT NULL CHECK (version >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE agent_profile_revision_skills (
    profile_revision_id TEXT NOT NULL REFERENCES agent_profile_revisions(id) ON DELETE RESTRICT,
    skill_id TEXT NOT NULL REFERENCES project_skills(id) ON DELETE RESTRICT,
    required INTEGER NOT NULL CHECK (required IN (0, 1)),
    PRIMARY KEY(profile_revision_id, skill_id)
);

CREATE TABLE agent_profile_revision_mcp_servers (
    profile_revision_id TEXT NOT NULL REFERENCES agent_profile_revisions(id) ON DELETE RESTRICT,
    mcp_server_id TEXT NOT NULL REFERENCES project_mcp_servers(id) ON DELETE RESTRICT,
    required INTEGER NOT NULL CHECK (required IN (0, 1)),
    enabled_tools_json TEXT NOT NULL,
    PRIMARY KEY(profile_revision_id, mcp_server_id)
);

CREATE TABLE run_tool_policy_snapshots (
    run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    snapshot_json TEXT NOT NULL,
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
    created_at TEXT NOT NULL
);`, true
	case 20:
		return `CREATE TABLE run_usage (
    run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    input_tokens INTEGER CHECK (input_tokens IS NULL OR input_tokens >= 0),
    cached_input_tokens INTEGER CHECK (cached_input_tokens IS NULL OR cached_input_tokens >= 0),
    cache_write_input_tokens INTEGER CHECK (cache_write_input_tokens IS NULL OR cache_write_input_tokens >= 0),
    output_tokens INTEGER CHECK (output_tokens IS NULL OR output_tokens >= 0),
    reasoning_output_tokens INTEGER CHECK (reasoning_output_tokens IS NULL OR reasoning_output_tokens >= 0),
    model_requests INTEGER CHECK (model_requests IS NULL OR model_requests >= 1),
    peak_input_tokens INTEGER CHECK (peak_input_tokens IS NULL OR peak_input_tokens >= 0),
    source TEXT NOT NULL CHECK (source IN ('CODEX_JSONL')),
    captured_at TEXT NOT NULL,
    CHECK (
        input_tokens IS NOT NULL OR cached_input_tokens IS NOT NULL OR
        cache_write_input_tokens IS NOT NULL OR output_tokens IS NOT NULL OR
        reasoning_output_tokens IS NOT NULL OR model_requests IS NOT NULL OR
        peak_input_tokens IS NOT NULL
    ),
    CHECK (input_tokens IS NULL OR cached_input_tokens IS NULL OR cached_input_tokens <= input_tokens),
    CHECK (input_tokens IS NULL OR peak_input_tokens IS NULL OR peak_input_tokens <= input_tokens)
);

CREATE INDEX run_usage_captured_idx ON run_usage(captured_at DESC);`, true
	case 21:
		return `ALTER TABLE task_events RENAME TO task_events_v20;
CREATE TABLE task_events (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    event_type TEXT NOT NULL CHECK (event_type IN (
        'TASK_CREATED', 'TASK_STATUS_CHANGED', 'TASK_TITLE_CHANGED',
        'TASK_TARGET_BRANCH_CHANGED'
    )),
    payload_json TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    UNIQUE(task_id, sequence)
);
INSERT INTO task_events SELECT * FROM task_events_v20;
DROP TABLE task_events_v20;
CREATE INDEX task_events_timeline_idx ON task_events(task_id, sequence);`, true
	case 22:
		return `CREATE TABLE run_workspace_snapshots (
    run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,
    branch_name TEXT NOT NULL,
    target_branch TEXT NOT NULL,
    base_commit_sha TEXT NOT NULL,
    head_before TEXT NOT NULL,
    head_after TEXT NOT NULL,
    dirty_before INTEGER NOT NULL CHECK (dirty_before IN (0, 1)),
    dirty_after INTEGER NOT NULL CHECK (dirty_after IN (0, 1)),
    state_after TEXT NOT NULL CHECK (state_after IN ('READY', 'DIRTY', 'QUARANTINED', 'ERROR')),
    captured_at TEXT NOT NULL
);`, true
	case 23:
		return `CREATE TABLE run_events (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    event_type TEXT NOT NULL CHECK (length(trim(event_type)) BETWEEN 1 AND 100),
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json)),
    occurred_at TEXT NOT NULL,
    UNIQUE(run_id, sequence)
);
CREATE INDEX run_events_timeline_idx ON run_events(run_id, sequence);

INSERT INTO run_events(id, run_id, sequence, event_type, payload_json, occurred_at)
SELECT lower(hex(randomblob(16))), id, 1, 'RUN_IMPORTED',
       json_object('schema_version', 1, 'status', status), updated_at
FROM runs;`, true
	case 24:
		return `ALTER TABLE runs ADD COLUMN cancel_requested_at TEXT;
ALTER TABLE runs ADD COLUMN cancel_reason TEXT NOT NULL DEFAULT ''
    CHECK (length(cancel_reason) <= 1000);`, true
	case 25:
		return `CREATE TABLE agent_sessions (
    id TEXT PRIMARY KEY,
    topic_id TEXT REFERENCES topics(id) ON DELETE RESTRICT,
    task_id TEXT REFERENCES tasks(id) ON DELETE RESTRICT,
    profile_revision_id TEXT NOT NULL REFERENCES agent_profile_revisions(id) ON DELETE RESTRICT,
    model TEXT NOT NULL DEFAULT '',
    adapter TEXT NOT NULL,
    external_session_id TEXT NOT NULL CHECK (length(trim(external_session_id)) BETWEEN 1 AND 255),
    status TEXT NOT NULL CHECK (status IN ('ACTIVE', 'INVALID')),
    last_run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (
        (topic_id IS NOT NULL AND task_id IS NULL) OR
        (topic_id IS NULL AND task_id IS NOT NULL)
    )
);
CREATE UNIQUE INDEX agent_sessions_active_task_idx
ON agent_sessions(task_id, profile_revision_id)
WHERE task_id IS NOT NULL AND status = 'ACTIVE';
CREATE UNIQUE INDEX agent_sessions_active_topic_idx
ON agent_sessions(topic_id, profile_revision_id)
WHERE topic_id IS NOT NULL AND status = 'ACTIVE';
CREATE UNIQUE INDEX agent_sessions_external_idx
ON agent_sessions(adapter, external_session_id);
ALTER TABLE runs ADD COLUMN agent_session_id TEXT REFERENCES agent_sessions(id) ON DELETE RESTRICT;
ALTER TABLE runs ADD COLUMN session_resumed INTEGER NOT NULL DEFAULT 0
    CHECK (session_resumed IN (0, 1));`, true
	case 26:
		return `CREATE TABLE approval_requests (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    external_request_id TEXT NOT NULL,
    item_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL CHECK (kind IN ('COMMAND', 'FILE_CHANGE', 'NETWORK', 'PERMISSIONS')),
    reason TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 4000),
    command_text TEXT NOT NULL DEFAULT '' CHECK (length(command_text) <= 16000),
    cwd TEXT NOT NULL DEFAULT '' CHECK (length(cwd) <= 4000),
    host TEXT NOT NULL DEFAULT '' CHECK (length(host) <= 1000),
    protocol TEXT NOT NULL DEFAULT '' CHECK (length(protocol) <= 100),
    grant_root TEXT NOT NULL DEFAULT '' CHECK (length(grant_root) <= 4000),
    available_decisions_json TEXT NOT NULL CHECK (json_valid(available_decisions_json)),
    status TEXT NOT NULL CHECK (status IN ('OPEN', 'RESOLVED', 'CLEARED')),
    decision TEXT NOT NULL DEFAULT '' CHECK (decision IN ('', 'ACCEPT_ONCE', 'ACCEPT_SESSION', 'DECLINE', 'CANCEL_RUN')),
    version INTEGER NOT NULL CHECK (version >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    resolved_at TEXT,
    UNIQUE(run_id, external_request_id)
);
CREATE UNIQUE INDEX approval_requests_one_open_run_idx
ON approval_requests(run_id) WHERE status = 'OPEN';
CREATE INDEX approval_requests_open_idx
ON approval_requests(status, created_at);`, true
	case 27:
		return `ALTER TABLE runs ADD COLUMN runner_identity TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN lease_heartbeat_at TEXT;
CREATE TABLE run_finalization_intents (
    run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    terminal_status TEXT NOT NULL CHECK (terminal_status IN (
        'NEEDS_INPUT', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'TIMED_OUT', 'LOST'
    )),
    exit_code INTEGER NOT NULL,
    failure_kind TEXT NOT NULL DEFAULT '',
    failure_code TEXT NOT NULL DEFAULT '',
    failure_message TEXT NOT NULL DEFAULT '',
    failure_retryable INTEGER CHECK (failure_retryable IS NULL OR failure_retryable IN (0, 1)),
    clarification_json TEXT CHECK (clarification_json IS NULL OR json_valid(clarification_json)),
    created_at TEXT NOT NULL,
    completed_at TEXT
);`, true
	case 28:
		return `ALTER TABLE topic_events RENAME TO topic_events_v27;
CREATE TABLE topic_events (
    id TEXT PRIMARY KEY,
    topic_id TEXT NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    event_type TEXT NOT NULL CHECK (event_type IN (
        'TOPIC_CREATED', 'TOPIC_STATUS_CHANGED', 'TOPIC_MESSAGE_ADDED', 'TOPIC_PLANNING_REQUESTED'
    )),
    payload_json TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    UNIQUE(topic_id, sequence)
);
INSERT INTO topic_events SELECT * FROM topic_events_v27;
DROP TABLE topic_events_v27;
CREATE INDEX topic_events_timeline_idx ON topic_events(topic_id, sequence);

ALTER TABLE run_finalization_intents ADD COLUMN planning_result_json TEXT
    CHECK (planning_result_json IS NULL OR json_valid(planning_result_json));`, true
	case 29:
		return `CREATE TABLE task_feedback_turns (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    source_message_id TEXT NOT NULL UNIQUE REFERENCES messages(id) ON DELETE CASCADE,
    intent TEXT NOT NULL CHECK (intent IN ('DISCUSS', 'REQUEST_CHANGES')),
    status TEXT NOT NULL CHECK (status IN ('QUEUED', 'RUNNING', 'ANSWERED', 'APPLIED', 'FAILED')),
    run_id TEXT UNIQUE REFERENCES runs(id) ON DELETE RESTRICT,
    response_message_id TEXT UNIQUE REFERENCES messages(id) ON DELETE RESTRICT,
    failure_message TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (
        (intent = 'DISCUSS' AND status IN ('QUEUED', 'RUNNING', 'ANSWERED', 'FAILED')) OR
        (intent = 'REQUEST_CHANGES' AND status = 'APPLIED')
    ),
    CHECK ((status = 'QUEUED' AND run_id IS NULL) OR status != 'QUEUED'),
    CHECK ((status IN ('RUNNING', 'ANSWERED', 'FAILED') AND run_id IS NOT NULL) OR
           status NOT IN ('RUNNING', 'ANSWERED', 'FAILED')),
    CHECK ((status = 'ANSWERED' AND response_message_id IS NOT NULL) OR
           (status != 'ANSWERED' AND response_message_id IS NULL))
);
CREATE INDEX task_feedback_queue_idx ON task_feedback_turns(status, created_at);
CREATE INDEX task_feedback_task_idx ON task_feedback_turns(task_id, created_at DESC);

ALTER TABLE run_finalization_intents ADD COLUMN task_reply TEXT
    CHECK (task_reply IS NULL OR length(trim(task_reply)) BETWEEN 1 AND 20000);`, true
	case 30:
		return `ALTER TABLE task_feedback_turns RENAME TO task_feedback_turns_v29;
CREATE TABLE task_feedback_turns (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    source_message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    retry_of_feedback_id TEXT UNIQUE REFERENCES task_feedback_turns(id) ON DELETE RESTRICT,
    intent TEXT NOT NULL CHECK (intent IN ('DISCUSS', 'REQUEST_CHANGES')),
    status TEXT NOT NULL CHECK (status IN ('QUEUED', 'RUNNING', 'ANSWERED', 'APPLIED', 'FAILED')),
    run_id TEXT UNIQUE REFERENCES runs(id) ON DELETE RESTRICT,
    response_message_id TEXT UNIQUE REFERENCES messages(id) ON DELETE RESTRICT,
    failure_message TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (retry_of_feedback_id IS NULL OR retry_of_feedback_id != id),
    CHECK (
        (intent = 'DISCUSS' AND status IN ('QUEUED', 'RUNNING', 'ANSWERED', 'FAILED')) OR
        (intent = 'REQUEST_CHANGES' AND status = 'APPLIED')
    ),
    CHECK ((status = 'QUEUED' AND run_id IS NULL) OR status != 'QUEUED'),
    CHECK ((status IN ('RUNNING', 'ANSWERED', 'FAILED') AND run_id IS NOT NULL) OR
           status NOT IN ('RUNNING', 'ANSWERED', 'FAILED')),
    CHECK ((status = 'ANSWERED' AND response_message_id IS NOT NULL) OR
           (status != 'ANSWERED' AND response_message_id IS NULL))
);
INSERT INTO task_feedback_turns(
    id, task_id, source_message_id, retry_of_feedback_id, intent, status, run_id,
    response_message_id, failure_message, created_at, updated_at
)
SELECT id, task_id, source_message_id, NULL, intent, status, run_id,
       response_message_id, failure_message, created_at, updated_at
FROM task_feedback_turns_v29;
DROP TABLE task_feedback_turns_v29;
CREATE INDEX task_feedback_queue_idx ON task_feedback_turns(status, created_at);
CREATE INDEX task_feedback_task_idx ON task_feedback_turns(task_id, created_at DESC);

CREATE TABLE task_feedback_events (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    feedback_id TEXT NOT NULL REFERENCES task_feedback_turns(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    status TEXT NOT NULL CHECK (status IN ('QUEUED', 'RUNNING', 'ANSWERED', 'APPLIED', 'FAILED')),
    run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    response_message_id TEXT REFERENCES messages(id) ON DELETE RESTRICT,
    failure_message TEXT NOT NULL DEFAULT '',
    occurred_at TEXT NOT NULL,
    UNIQUE(task_id, sequence)
);
INSERT INTO task_feedback_events(
    id, task_id, feedback_id, sequence, status, run_id,
    response_message_id, failure_message, occurred_at
)
SELECT lower(hex(randomblob(16))), task_id, id,
       ROW_NUMBER() OVER (PARTITION BY task_id ORDER BY created_at, id),
       status, run_id, response_message_id, failure_message, updated_at
FROM task_feedback_turns;
CREATE INDEX task_feedback_events_timeline_idx
ON task_feedback_events(task_id, sequence);`, true
	case 31:
		return `CREATE TABLE task_integration_attempts (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    review_id TEXT NOT NULL REFERENCES task_reviews(id) ON DELETE RESTRICT,
    operation TEXT NOT NULL CHECK (operation IN ('INTEGRATE', 'SYNC')),
    status TEXT NOT NULL CHECK (status IN (
        'RUNNING', 'SUCCEEDED', 'NEEDS_SYNC', 'SYNCED', 'CONFLICT', 'FAILED'
    )),
    target_branch TEXT NOT NULL CHECK (length(trim(target_branch)) BETWEEN 1 AND 255),
    source_commit_sha TEXT NOT NULL CHECK (length(source_commit_sha) >= 7),
    target_before_sha TEXT NOT NULL CHECK (length(target_before_sha) >= 7),
    target_after_sha TEXT NOT NULL DEFAULT '',
    workspace_after_sha TEXT NOT NULL DEFAULT '',
    failure_kind TEXT NOT NULL DEFAULT '' CHECK (length(failure_kind) <= 100),
    failure_message TEXT NOT NULL DEFAULT '' CHECK (length(failure_message) <= 4000),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX task_integration_one_running_idx
ON task_integration_attempts(task_id) WHERE status = 'RUNNING';
CREATE INDEX task_integration_task_history_idx
ON task_integration_attempts(task_id, created_at DESC, id DESC);`, true
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
