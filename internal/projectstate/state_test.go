package projectstate

import (
	"archive/zip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestBackupAndRestoreRoundTrip(t *testing.T) {
	current := initializeProject(t)
	database, err := storage.OpenExisting(t.Context(), current.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	created, err := storage.NewTaskStore(database).Create(t.Context(), task.CreateInput{Title: "备份前任务"})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(current.Paths.Artifacts, "sample.txt")
	if err := os.WriteFile(artifactPath, []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(current.Root, ".tmp", "backup.zip")
	manifest, err := Backup(t.Context(), current, archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ProjectInstanceID != current.InstanceID || len(manifest.Files) < 4 {
		t.Fatalf("manifest = %#v", manifest)
	}

	database, err = storage.OpenExisting(t.Context(), current.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.NewTaskStore(database).Create(t.Context(), task.CreateInput{Title: "备份后任务"}); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(t.Context(), current, archivePath); err != nil {
		t.Fatal(err)
	}

	database, err = storage.OpenExisting(t.Context(), current.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	items, err := storage.NewTaskStore(database).List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("restored tasks = %#v", items)
	}
	content, err := os.ReadFile(artifactPath)
	if err != nil || string(content) != "artifact" {
		t.Fatalf("artifact = %q, %v", content, err)
	}
}

func TestRestoreRejectsArchiveTraversal(t *testing.T) {
	current := initializeProject(t)
	archivePath := filepath.Join(current.Root, ".tmp", "unsafe.zip")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("../escape")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("unsafe"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), current, archivePath); err == nil || !strings.Contains(err.Error(), "不安全") {
		t.Fatalf("Restore() error = %v", err)
	}
}

func TestRestoreRejectsMissingManifestAndDifferentProject(t *testing.T) {
	current := initializeProject(t)
	missingManifest := filepath.Join(current.Root, ".tmp", "missing-manifest.zip")
	file, err := os.Create(missingManifest)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("state.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("not a database")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(t.Context(), current, missingManifest); err == nil || !strings.Contains(err.Error(), "缺少 manifest") {
		t.Fatalf("missing manifest Restore() error = %v", err)
	}

	other := initializeProject(t)
	otherBackup := filepath.Join(other.Root, ".tmp", "other-project.zip")
	if _, err := Backup(t.Context(), other, otherBackup); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(t.Context(), current, otherBackup); err == nil || !strings.Contains(err.Error(), "不属于当前项目") {
		t.Fatalf("different project Restore() error = %v", err)
	}
	if _, err := Restore(t.Context(), current, filepath.Join(current.Root, ".tmp", "missing.zip")); err == nil {
		t.Fatal("Restore() accepted missing archive")
	}
}

func TestCheckIntegrityReportsMissingManagedArtifact(t *testing.T) {
	current := initializeProject(t)
	database, err := storage.OpenExisting(t.Context(), current.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(t.Context(), `INSERT INTO artifacts(
id, kind, original_name, original_media_type, original_relative_path, original_size, original_sha256,
optimized_media_type, optimized_relative_path, optimized_size, optimized_sha256, created_at
) VALUES ('a', 'IMAGE', 'a.png', 'image/png', 'images/a.png', 1, ?, 'image/png', 'images/b.png', 1, ?, '2026-01-01T00:00:00Z')`,
		strings.Repeat("a", 64), strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	report, err := CheckIntegrity(t.Context(), current)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || len(report.Problems) != 2 {
		t.Fatalf("report = %#v", report)
	}
	if _, err := Backup(t.Context(), current, filepath.Join(current.Root, ".tmp", "invalid.zip")); err == nil || !strings.Contains(err.Error(), "完整性检查失败") {
		t.Fatalf("Backup() error = %v", err)
	}
}

func initializeProject(t *testing.T) *project.Project {
	t.Helper()
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "--quiet", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	current, _, err := project.Initialize(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	return current
}
