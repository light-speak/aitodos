package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestOpenEnablesRequiredPragmasAndCreatesBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	metadata := ProjectMetadata{
		InstanceID: "instance-1", Name: "example",
		RepoRoot: "/repo/example", GitCommonDir: "/repo/example/.git",
	}
	database, err := Open(t.Context(), path, metadata)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var journalMode string
	if err := database.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil || journalMode != "wal" {
		t.Fatalf("journal mode = %q, %v; want wal", journalMode, err)
	}
	var foreignKeys int
	if err := database.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, %v; want 1", foreignKeys, err)
	}
	loaded, err := ReadProjectMetadata(t.Context(), database)
	if err != nil || loaded != metadata {
		t.Fatalf("metadata = %#v, %v; want %#v", loaded, err, metadata)
	}
	epoch, version, err := schemaIdentity(t.Context(), database)
	if err != nil || epoch != currentSchemaEpoch || version != currentSchemaVersion {
		t.Fatalf("schema = %d/%d, %v; want %d/%d", epoch, version, err, currentSchemaEpoch, currentSchemaVersion)
	}
	for _, table := range []string{
		"topics", "tasks", "runs", "search_documents", "objectives",
		"objective_revisions", "objective_checkpoints", "objective_events",
	} {
		var found string
		if err := database.QueryRow(
			"SELECT name FROM sqlite_schema WHERE type = 'table' AND name = ?", table,
		).Scan(&found); err != nil {
			t.Fatalf("find baseline table %s: %v", table, err)
		}
	}
	var profiles, defaults int
	if err := database.QueryRow("SELECT COUNT(*) FROM agent_profiles").Scan(&profiles); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT COUNT(*) FROM project_agent_defaults").Scan(&defaults); err != nil {
		t.Fatal(err)
	}
	if profiles != 5 || defaults != 5 {
		t.Fatalf("default agent configuration = %d profiles, %d defaults; want 5 and 5", profiles, defaults)
	}
	rows, err := database.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("baseline contains a foreign key violation")
	}
}

func TestOpenExistingWaitsForConcurrentWALConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	database, err := Open(t.Context(), path, ProjectMetadata{
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

func TestOpenCreatesOneBaselineUnderConcurrentInitialization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	metadata := ProjectMetadata{
		InstanceID: "concurrent-init", Name: "concurrent", RepoRoot: "/repo", GitCommonDir: "/repo/.git",
	}
	const writers = 4
	start := make(chan struct{})
	errorsFound := make(chan error, writers)
	var group sync.WaitGroup
	for range writers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			opened, openErr := Open(context.Background(), path, metadata)
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
			t.Fatalf("Open() concurrent initialization error = %v", openErr)
		}
	}
	database, err := OpenExisting(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var rows int
	if err := database.QueryRow("SELECT COUNT(*) FROM schema_metadata WHERE id = 1").Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("schema metadata rows = %d, %v", rows, err)
	}
}

func TestSchemaInitializationLockHonorsContextCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db.init.lock")
	first, err := acquireSchemaLock(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseSchemaLock(first)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	second, err := acquireSchemaLock(cancelled, path)
	if second != nil {
		releaseSchemaLock(second)
		t.Fatal("cancelled lock acquisition unexpectedly succeeded")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lock error = %v, want context.Canceled", err)
	}
}

func TestOpenExistingRejectsPreReleaseMigrationDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
INSERT INTO schema_migrations(version, applied_at) VALUES (45, '2026-09-05T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenExisting(t.Context(), path); !errors.Is(err, ErrLegacySchema) {
		t.Fatalf("OpenExisting() error = %v, want ErrLegacySchema", err)
	}
}

func TestOpenExistingRejectsDifferentSchemaIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	database, err := Open(t.Context(), path, ProjectMetadata{
		InstanceID: "future", Name: "future", RepoRoot: "/repo", GitCommonDir: "/repo/.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE schema_metadata SET version = 2 WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenExisting(t.Context(), path); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("OpenExisting() error = %v, want ErrUnsupportedSchema", err)
	}
}
