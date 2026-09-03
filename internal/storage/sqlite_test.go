package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/topic"
)

func TestOpenEnablesRequiredPragmasAndMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	metadata := ProjectMetadata{
		InstanceID:   "instance-1",
		Name:         "example",
		RepoRoot:     "/tmp/example",
		GitCommonDir: "/tmp/example/.git",
	}

	database, err := Open(context.Background(), path, metadata)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()

	var journalMode string
	if err := database.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal mode = %q, want wal", journalMode)
	}

	var foreignKeys int
	if err := database.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}

	loaded, err := ReadProjectMetadata(context.Background(), database)
	if err != nil {
		t.Fatalf("ReadProjectMetadata() error = %v", err)
	}
	if loaded != metadata {
		t.Fatalf("metadata = %#v, want %#v", loaded, metadata)
	}

	version, err := schemaVersion(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, currentSchemaVersion)
	}
}

func TestOpenExistingWaitsForConcurrentWALConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	database, err := Open(context.Background(), path, ProjectMetadata{
		InstanceID: "concurrent", Name: "concurrent", RepoRoot: "/repo", GitCommonDir: "/repo/.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	const readers = 8
	start := make(chan struct{})
	errorsFound := make(chan error, readers)
	var group sync.WaitGroup
	for range readers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			opened, openErr := OpenExisting(context.Background(), path)
			if openErr == nil {
				openErr = opened.Close()
			}
			errorsFound <- openErr
		}()
	}
	close(start)
	group.Wait()
	close(errorsFound)
	for openErr := range errorsFound {
		if openErr != nil {
			t.Fatalf("OpenExisting() concurrent error = %v", openErr)
		}
	}
}

func TestMigrationsSixteenAndSeventeenPreserveRunAndQualityForeignKeys(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := configure(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL
)`); err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 15; version++ {
		if err := applyMigration(ctx, database, version); err != nil {
			t.Fatal(err)
		}
	}
	if err := upsertProjectMetadata(ctx, database, ProjectMetadata{
		InstanceID: "migration-16", Name: "example", RepoRoot: "/repo", GitCommonDir: "/repo/.git",
	}); err != nil {
		t.Fatal(err)
	}
	now := "2026-08-20T00:00:00Z"
	if _, err := database.ExecContext(ctx, `
INSERT INTO tasks(
    id, task_key, title, description, acceptance_criteria, status, priority,
    target_branch, base_commit_sha, current_workspace_id, latest_run_id,
    created_at, updated_at, version
) VALUES ('task-migration', 'ATS-MIGRATE', '保留迁移数据', '', '', 'REVIEW', 2,
          '', '', '', 'run-migration', ?, ?, 2);
INSERT INTO task_events(id, task_id, sequence, event_type, payload_json, occurred_at)
VALUES ('event-migration', 'task-migration', 1, 'TASK_CREATED', '{}', ?);
INSERT INTO runs(
    id, purpose, topic_id, task_id, status, profile_revision_id, retry_of_run_id,
    claim_token_hash, lease_generation, lease_expires_at, run_nonce,
    queued_at, claimed_at, started_at, finished_at, exit_code,
    failure_kind, failure_code, failure_message, created_at, updated_at
) VALUES ('run-migration', 'IMPLEMENTATION', NULL, 'task-migration', 'SUCCEEDED',
          'profile-implementer-r1', NULL, 'hash', 1, ?, 'nonce', ?, ?, ?, ?, 0,
          '', '', '', ?, ?);
INSERT INTO run_artifacts(id, run_id, kind, relative_path, sha256, size, truncated, created_at)
VALUES ('artifact-migration', 'run-migration', 'PROMPT', 'runs/migration/prompt.md', 'abc', 3, 0, ?);
INSERT INTO task_estimates(
    id, task_id, revision, points, remaining_points, confidence, rationale,
    source, source_run_id, created_at
) VALUES ('estimate-migration', 'task-migration', 1, 3, 3, 0.7, '迁移测试',
          'AI', 'run-migration', ?)`,
		now, now, now, now, now, now, now, now, now, now, now, now,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := OpenExisting(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	if _, err := NewRunStore(upgraded).Get(ctx, "run-migration"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAgentProfileStore(upgraded).GetByRole(ctx, agentprofile.RoleTriager); err != nil {
		t.Fatal(err)
	}
	rows, err := upgraded.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("migration 16 left a foreign key violation")
	}
}

func TestOpenExistingMigratesVersionTwoDatabaseToTopics(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := configure(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
)`); err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 2; version++ {
		if err := applyMigration(ctx, database, version); err != nil {
			t.Fatal(err)
		}
	}
	metadata := ProjectMetadata{
		InstanceID: "instance-v2", Name: "example", RepoRoot: "/repo", GitCommonDir: "/repo/.git",
	}
	if err := upsertProjectMetadata(ctx, database, metadata); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := OpenExisting(ctx, path)
	if err != nil {
		t.Fatalf("OpenExisting() error = %v", err)
	}
	defer upgraded.Close()
	version, err := schemaVersion(ctx, upgraded)
	if err != nil {
		t.Fatal(err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, currentSchemaVersion)
	}
	if _, err := NewTopicStore(upgraded).Create(ctx, topic.CreateInput{Title: "迁移后的 Topic"}); err != nil {
		t.Fatalf("create Topic after migration: %v", err)
	}
}

func TestOpenExistingMigratesVersionThreeDatabaseToDiscussionTables(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := configure(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
)`); err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 3; version++ {
		if err := applyMigration(ctx, database, version); err != nil {
			t.Fatal(err)
		}
	}
	metadata := ProjectMetadata{
		InstanceID: "instance-v3", Name: "example", RepoRoot: "/repo", GitCommonDir: "/repo/.git",
	}
	if err := upsertProjectMetadata(ctx, database, metadata); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := OpenExisting(ctx, path)
	if err != nil {
		t.Fatalf("OpenExisting() error = %v", err)
	}
	defer upgraded.Close()
	for _, table := range []string{"threads", "messages"} {
		var name string
		if err := upgraded.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name); err != nil {
			t.Fatalf("find table %s: %v", table, err)
		}
	}
	var artifactTable string
	if err := upgraded.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'artifacts'").Scan(&artifactTable); err != nil {
		t.Fatalf("find artifacts table: %v", err)
	}
	for _, table := range []string{"message_task_links", "topic_task_links", "task_relations"} {
		var name string
		if err := upgraded.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name); err != nil {
			t.Fatalf("find table %s: %v", table, err)
		}
	}
	for _, table := range []string{"workspaces", "releases", "task_reviews"} {
		var name string
		if err := upgraded.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name); err != nil {
			t.Fatalf("find table %s: %v", table, err)
		}
	}
}

func TestEveryHistoricalSchemaVersionUpgradesToCurrent(t *testing.T) {
	for startingVersion := 1; startingVersion < currentSchemaVersion; startingVersion++ {
		t.Run(fmt.Sprintf("v%d", startingVersion), func(t *testing.T) {
			ctx := t.Context()
			path := filepath.Join(t.TempDir(), "state.db")
			database, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			if err := configure(ctx, database); err != nil {
				database.Close()
				t.Fatal(err)
			}
			if _, err := database.ExecContext(ctx, `CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL
)`); err != nil {
				database.Close()
				t.Fatal(err)
			}
			for version := 1; version <= startingVersion; version++ {
				if err := applyMigration(ctx, database, version); err != nil {
					database.Close()
					t.Fatalf("prepare schema v%d: %v", version, err)
				}
			}
			metadata := ProjectMetadata{
				InstanceID:   fmt.Sprintf("upgrade-v%d", startingVersion),
				Name:         "upgrade-test",
				RepoRoot:     "/repo",
				GitCommonDir: "/repo/.git",
			}
			if err := upsertProjectMetadata(ctx, database, metadata); err != nil {
				database.Close()
				t.Fatal(err)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}

			upgraded, err := OpenExisting(ctx, path)
			if err != nil {
				t.Fatalf("upgrade schema v%d: %v", startingVersion, err)
			}
			defer upgraded.Close()
			version, err := schemaVersion(ctx, upgraded)
			if err != nil || version != currentSchemaVersion {
				t.Fatalf("schema version = %d, %v; want %d", version, err, currentSchemaVersion)
			}
			loaded, err := ReadProjectMetadata(ctx, upgraded)
			if err != nil || loaded != metadata {
				t.Fatalf("metadata = %#v, %v; want %#v", loaded, err, metadata)
			}
			rows, err := upgraded.QueryContext(ctx, "PRAGMA foreign_key_check")
			if err != nil {
				t.Fatal(err)
			}
			if rows.Next() {
				rows.Close()
				t.Fatal("upgrade left a foreign key violation")
			}
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
