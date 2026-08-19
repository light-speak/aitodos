package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

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
	if version != 8 {
		t.Fatalf("schema version = %d, want 8", version)
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
	if version != 8 {
		t.Fatalf("schema version = %d, want 8", version)
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
	for _, table := range []string{"message_task_links", "topic_task_links", "task_links"} {
		var name string
		if err := upgraded.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name); err != nil {
			t.Fatalf("find table %s: %v", table, err)
		}
	}
	for _, table := range []string{"workspaces", "releases"} {
		var name string
		if err := upgraded.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name); err != nil {
			t.Fatalf("find table %s: %v", table, err)
		}
	}
}
