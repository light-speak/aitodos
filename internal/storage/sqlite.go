package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

const (
	currentSchemaEpoch   = 1
	currentSchemaVersion = 1
)

var (
	// ErrLegacySchema 表示数据库来自 1.0 前的开发迁移链，必须显式重建。
	ErrLegacySchema = errors.New("检测到 1.0 前开发数据库，请备份后重新执行 ats init")
	// ErrUnsupportedSchema 表示数据库基线与当前二进制不兼容。
	ErrUnsupportedSchema = errors.New("数据库 Schema 与当前二进制不兼容")
)

// ProjectMetadata 保存当前项目实例的本地身份。
type ProjectMetadata struct {
	InstanceID   string
	Name         string
	RepoRoot     string
	GitCommonDir string
}

// Open 打开数据库、创建完整基线并初始化项目元数据。
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

// OpenExisting 打开已经使用当前基线初始化的项目数据库。
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
	lock, err := acquireSchemaLock(ctx, path+".init.lock")
	if err != nil {
		return nil, err
	}
	defer releaseSchemaLock(lock)
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	database.SetMaxOpenConns(1)
	if err := configure(ctx, database); err != nil {
		database.Close()
		return nil, err
	}
	if err := ensureSchema(ctx, database); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func acquireSchemaLock(ctx context.Context, path string) (*os.File, error) {
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open schema initialization lock: %w", err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			lock.Close()
			return nil, fmt.Errorf("lock schema initialization: %w", err)
		}
		select {
		case <-ctx.Done():
			lock.Close()
			return nil, fmt.Errorf("lock schema initialization: %w", ctx.Err())
		case <-deadline.C:
			lock.Close()
			return nil, errors.New("等待数据库初始化锁超时")
		case <-ticker.C:
		}
	}
}

func releaseSchemaLock(lock *os.File) {
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = lock.Close()
}

func configure(ctx context.Context, database *sql.DB) error {
	for _, statement := range []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure sqlite with %q: %w", statement, err)
		}
	}
	return nil
}

func ensureSchema(ctx context.Context, database *sql.DB) error {
	empty, err := databaseIsEmpty(ctx, database)
	if err != nil {
		return err
	}
	if empty {
		return createBaseline(ctx, database)
	}
	epoch, version, err := schemaIdentity(ctx, database)
	if err != nil {
		legacy, checkErr := tableExists(ctx, database, "schema_migrations")
		if checkErr != nil {
			return checkErr
		}
		if legacy {
			return ErrLegacySchema
		}
		return fmt.Errorf("%w: %v", ErrUnsupportedSchema, err)
	}
	if epoch != currentSchemaEpoch || version != currentSchemaVersion {
		return fmt.Errorf("%w: epoch=%d version=%d, want epoch=%d version=%d",
			ErrUnsupportedSchema, epoch, version, currentSchemaEpoch, currentSchemaVersion)
	}
	return nil
}

func createBaseline(ctx context.Context, database *sql.DB) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema baseline: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, baselineSchema); err != nil {
		return fmt.Errorf("apply schema baseline: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, baselineSeed); err != nil {
		return fmt.Errorf("seed schema baseline: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO schema_metadata(id, epoch, version, applied_at) VALUES (1, ?, ?, ?)`,
		currentSchemaEpoch, currentSchemaVersion, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("record schema baseline: %w", err)
	}
	if err := checkForeignKeys(ctx, transaction); err != nil {
		return fmt.Errorf("validate schema baseline: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit schema baseline: %w", err)
	}
	return nil
}

func databaseIsEmpty(ctx context.Context, database *sql.DB) (bool, error) {
	var count int
	if err := database.QueryRowContext(ctx, `
SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect sqlite schema: %w", err)
	}
	return count == 0, nil
}

func tableExists(ctx context.Context, database *sql.DB, name string) (bool, error) {
	var count int
	if err := database.QueryRowContext(ctx, `
SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = ?`, name).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect sqlite table %q: %w", name, err)
	}
	return count == 1, nil
}

func schemaIdentity(ctx context.Context, database *sql.DB) (int, int, error) {
	var epoch, version int
	if err := database.QueryRowContext(ctx, "SELECT epoch, version FROM schema_metadata WHERE id = 1").Scan(&epoch, &version); err != nil {
		return 0, 0, fmt.Errorf("read schema identity: %w", err)
	}
	return epoch, version, nil
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
