package projectstate

import (
	"archive/zip"
	"context"
	"errors"
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

func TestProjectStateHelpersRejectUnsafeAndBrokenInputs(t *testing.T) {
	if err := writeArchiveAtomically("", nil, Manifest{}); err == nil {
		t.Fatal("writeArchiveAtomically() accepted an empty output path")
	}
	root := t.TempDir()
	if _, err := inspectFile(filepath.Join(root, "missing")); err == nil {
		t.Fatal("inspectFile() accepted a missing file")
	}
	if err := writeArchiveAtomically(filepath.Join(root, "backup.zip"), []backupFile{{
		Source: filepath.Join(root, "missing"), ArchivePath: "missing",
	}}, Manifest{}); err == nil {
		t.Fatal("writeArchiveAtomically() accepted a missing source")
	}

	for _, relative := range []string{"", ".", "..", "../escape", "/absolute"} {
		if _, err := managedArtifactPath(root, relative); err == nil {
			t.Fatalf("managedArtifactPath(%q) unexpectedly succeeded", relative)
		}
	}
	wantPath := filepath.Join(root, "nested", "artifact.txt")
	if got, err := managedArtifactPath(root, "nested/artifact.txt"); err != nil || got != wantPath {
		t.Fatalf("managedArtifactPath() = %q, %v", got, err)
	}

	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	runtimeRoot := filepath.Join(root, "runtime")
	inside := filepath.Join(runtimeRoot, "restore-safe")
	if err := os.MkdirAll(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	removeManagedTemp(runtimeRoot, outside)
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside path was removed: %v", err)
	}
	removeManagedTemp(runtimeRoot, inside)
	if _, err := os.Stat(inside); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed temp still exists: %v", err)
	}

	rollback := filepath.Join(root, "rollback")
	target := filepath.Join(root, "state.db")
	if err := os.Mkdir(rollback, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rollback, "state.db"), []byte("restored"), 0o600); err != nil {
		t.Fatal(err)
	}
	rollbackState([]string{target}, rollback)
	if content, err := os.ReadFile(target); err != nil || string(content) != "restored" {
		t.Fatalf("rollback content = %q, %v", content, err)
	}
}

func TestArchiveValidationRejectsUnsupportedManifestSymlinkAndDuplicates(t *testing.T) {
	current := initializeProject(t)
	tests := []struct {
		name    string
		entries func(*zip.Writer) error
	}{
		{name: "invalid manifest", entries: func(writer *zip.Writer) error {
			for _, entry := range []struct{ name, content string }{
				{name: "project.toml", content: "project"},
				{name: "local.toml", content: "local"},
				{name: "state.db", content: "database"},
				{name: manifestName, content: `{}`},
			} {
				created, err := writer.Create(entry.name)
				if err != nil {
					return err
				}
				if _, err := created.Write([]byte(entry.content)); err != nil {
					return err
				}
			}
			return nil
		}},
		{name: "unsupported path", entries: func(writer *zip.Writer) error {
			_, err := writer.Create("README.md")
			return err
		}},
		{name: "symlink", entries: func(writer *zip.Writer) error {
			header := &zip.FileHeader{Name: "artifacts/link"}
			header.SetMode(os.ModeSymlink | 0o777)
			_, err := writer.CreateHeader(header)
			return err
		}},
		{name: "duplicate", entries: func(writer *zip.Writer) error {
			for range 2 {
				entry, err := writer.Create("state.db")
				if err != nil {
					return err
				}
				if _, err := entry.Write([]byte("duplicate")); err != nil {
					return err
				}
			}
			return nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archivePath := filepath.Join(current.Root, ".tmp", strings.ReplaceAll(test.name, " ", "-")+".zip")
			file, err := os.Create(archivePath)
			if err != nil {
				t.Fatal(err)
			}
			writer := zip.NewWriter(file)
			if err := test.entries(writer); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := Restore(t.Context(), current, archivePath); err == nil {
				t.Fatal("Restore() accepted an invalid archive")
			}
		})
	}
}

func TestProjectStateRejectsMismatchedDatabaseAndArtifactSymlink(t *testing.T) {
	current := initializeProject(t)
	other := initializeProject(t)
	if err := verifyRestoredDatabase(t.Context(), other.Paths.Database, current); err == nil || !strings.Contains(err.Error(), "身份不匹配") {
		t.Fatalf("mismatched database error = %v", err)
	}
	if err := verifyRestoredDatabase(t.Context(), filepath.Join(current.Root, ".tmp", "missing.db"), current); err == nil {
		t.Fatal("verifyRestoredDatabase() accepted a missing database")
	}

	target := filepath.Join(current.Root, ".tmp", "outside-artifact")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(current.Paths.Artifacts, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := collectBackupFiles(current); err == nil || !strings.Contains(err.Error(), "不是普通文件") {
		t.Fatalf("artifact symlink error = %v", err)
	}
}

func TestArchiveHelpersHandleAllowedPathsAndDirectories(t *testing.T) {
	for _, name := range []string{manifestName, "project.toml", "local.toml", "state.db", "artifacts/nested/file"} {
		if !safeArchivePath(name) {
			t.Fatalf("safeArchivePath(%q) = false", name)
		}
	}
	root := t.TempDir()
	archivePath := filepath.Join(root, "source.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	header := &zip.FileHeader{Name: "artifacts/", Method: zip.Store}
	header.SetMode(os.ModeDir | 0o700)
	if _, err := writer.CreateHeader(header); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := extractArchiveFile(reader.File[0], filepath.Join(root, "staging")); err != nil {
		t.Fatal(err)
	}
	reader.Close()

}

func TestVerifyManifestRejectsUnsafeDuplicateMissingAndChangedFiles(t *testing.T) {
	staging := t.TempDir()
	entries := make([]ManifestFile, 0, 3)
	for _, name := range []string{"project.toml", "local.toml", "state.db"} {
		path := filepath.Join(staging, name)
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		metadata, err := inspectFile(path)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, ManifestFile{Path: name, Size: metadata.Size, SHA256: metadata.SHA256})
	}
	if err := verifyManifest(staging, Manifest{Files: entries}); err != nil {
		t.Fatalf("valid manifest failed: %v", err)
	}
	tests := []struct {
		name  string
		files []ManifestFile
	}{
		{name: "unsafe", files: []ManifestFile{{Path: "../escape"}}},
		{name: "manifest self", files: []ManifestFile{{Path: manifestName}}},
		{name: "duplicate", files: append(append([]ManifestFile{}, entries...), entries[0])},
		{name: "missing required", files: entries[:2]},
		{name: "changed size", files: []ManifestFile{{Path: entries[0].Path, Size: entries[0].Size + 1, SHA256: entries[0].SHA256}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := verifyManifest(staging, Manifest{Files: test.files}); err == nil {
				t.Fatal("verifyManifest() unexpectedly succeeded")
			}
		})
	}
}

func TestReplaceProjectStateRollsBackWhenStagingIsIncomplete(t *testing.T) {
	current := initializeProject(t)
	staging := filepath.Join(current.Paths.Runtime, "incomplete-staging")
	if err := os.MkdirAll(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "project.toml"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceProjectState(current, staging); err == nil {
		t.Fatal("replaceProjectState() accepted incomplete staging")
	}
	for _, path := range []string{current.Paths.ProjectConfig, current.Paths.LocalConfig, current.Paths.Database, current.Paths.Artifacts} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("original state %q was not rolled back: %v", path, err)
		}
	}
}

func TestManagedArtifactProblemsReportsUnsafeAndChangedFiles(t *testing.T) {
	current := initializeProject(t)
	database, err := storage.OpenExisting(t.Context(), current.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	artifactPath := filepath.Join(current.Paths.Artifacts, "changed.txt")
	if err := os.WriteFile(artifactPath, []byte("actual"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `INSERT INTO artifacts(
id, kind, original_name, original_media_type, original_relative_path, original_size, original_sha256,
optimized_media_type, optimized_relative_path, optimized_size, optimized_sha256, created_at
) VALUES ('unsafe', 'IMAGE', 'a.png', 'image/png', '../escape', 1, ?, 'image/png', 'changed.txt', 1, ?, '2026-01-01T00:00:00Z')`,
		strings.Repeat("a", 64), strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	problems, err := managedArtifactProblems(t.Context(), database, current.Paths.Artifacts)
	if err != nil || len(problems) != 2 {
		t.Fatalf("managedArtifactProblems() = %#v, %v", problems, err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := databaseProblems(t.Context(), database); err == nil {
		t.Fatal("databaseProblems() accepted a closed database")
	}
	if _, err := managedArtifactProblems(t.Context(), database, current.Paths.Artifacts); err == nil {
		t.Fatal("managedArtifactProblems() accepted a closed database")
	}
}

func TestProjectStateReportsFilesystemAndForeignKeyFailures(t *testing.T) {
	current := initializeProject(t)
	broken := projectWithPaths(current, current.Paths)
	broken.Paths.Database = filepath.Join(current.Root, ".tmp", "missing", "state.db")
	if _, err := CheckIntegrity(t.Context(), broken); err == nil {
		t.Fatal("CheckIntegrity() accepted a missing database")
	}
	if err := checkpointDatabase(t.Context(), broken.Paths.Database); err == nil {
		t.Fatal("checkpointDatabase() accepted a missing database")
	}
	broken = projectWithPaths(current, current.Paths)
	broken.Paths.Artifacts = filepath.Join(current.Root, ".tmp", "missing-artifacts")
	if _, err := collectBackupFiles(broken); err == nil {
		t.Fatal("collectBackupFiles() accepted a missing artifact directory")
	}
	blockingFile := filepath.Join(current.Root, ".tmp", "blocking-file")
	if err := os.WriteFile(blockingFile, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeArchiveAtomically(filepath.Join(blockingFile, "backup.zip"), nil, Manifest{}); err == nil {
		t.Fatal("writeArchiveAtomically() accepted a file as output directory")
	}
	broken = projectWithPaths(current, current.Paths)
	broken.Paths.Runtime = filepath.Join(blockingFile, "runtime")
	if _, err := Restore(t.Context(), broken, filepath.Join(current.Root, ".tmp", "missing.zip")); err == nil {
		t.Fatal("Restore() accepted an unusable runtime directory")
	}

	database, err := storage.OpenExisting(t.Context(), current.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if _, err := database.ExecContext(t.Context(), "PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `INSERT INTO task_labels(task_id, label_id, created_at) VALUES ('missing-task', 'missing-label', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	problems, err := databaseProblems(t.Context(), database)
	if err != nil || len(problems) == 0 || !strings.Contains(problems[0], "外键") {
		t.Fatalf("databaseProblems() = %#v, %v", problems, err)
	}
}

func projectWithPaths(current *project.Project, paths project.Paths) *project.Project {
	return &project.Project{
		Root:         current.Root,
		GitCommonDir: current.GitCommonDir,
		InstanceID:   current.InstanceID,
		Paths:        paths,
		Config:       current.Config,
		Local:        current.Local,
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
