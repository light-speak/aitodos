// Package projectstate 负责项目本地 .ats 事实数据的校验、备份和恢复。
package projectstate

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/storage"
)

const (
	manifestName    = "manifest.json"
	maxArchiveSize  = int64(10 << 30)
	maxArchiveFiles = 100000
)

// Manifest 描述可校验的项目备份内容。
type Manifest struct {
	FormatVersion     int            `json:"format_version"`
	ProjectInstanceID string         `json:"project_instance_id"`
	CreatedAt         time.Time      `json:"created_at"`
	Files             []ManifestFile `json:"files"`
}

// ManifestFile 保存备份文件的相对路径、大小和内容哈希。
type ManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// IntegrityReport 是适合 CLI 和 UI 展示的完整性结果。
type IntegrityReport struct {
	OK       bool     `json:"ok"`
	Problems []string `json:"problems"`
}

// Backup 在数据库无活动写入时创建不含 runtime 和 worktree 的原子 ZIP 备份。
func Backup(ctx context.Context, current *project.Project, outputPath string) (Manifest, error) {
	report, err := CheckIntegrity(ctx, current)
	if err != nil {
		return Manifest{}, err
	}
	if !report.OK {
		return Manifest{}, fmt.Errorf("项目完整性检查失败: %s", strings.Join(report.Problems, "; "))
	}
	if err := checkpointDatabase(ctx, current.Paths.Database); err != nil {
		return Manifest{}, err
	}
	files, err := collectBackupFiles(current)
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{FormatVersion: 1, ProjectInstanceID: current.InstanceID, CreatedAt: time.Now().UTC()}
	for _, file := range files {
		metadata, hashErr := inspectFile(file.Source)
		if hashErr != nil {
			return Manifest{}, hashErr
		}
		manifest.Files = append(manifest.Files, ManifestFile{Path: file.ArchivePath, Size: metadata.Size, SHA256: metadata.SHA256})
	}
	if err := writeArchiveAtomically(outputPath, files, manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Restore 校验完整备份后原子替换当前项目事实数据；调用方必须先确认 Daemon 已停止。
func Restore(ctx context.Context, current *project.Project, archivePath string) (Manifest, error) {
	staging, err := os.MkdirTemp(current.Paths.Runtime, "restore-")
	if err != nil {
		return Manifest{}, fmt.Errorf("创建恢复暂存目录: %w", err)
	}
	defer removeManagedTemp(current.Paths.Runtime, staging)
	manifest, err := extractAndVerifyArchive(archivePath, staging)
	if err != nil {
		return Manifest{}, err
	}
	if manifest.ProjectInstanceID != current.InstanceID {
		return Manifest{}, errors.New("备份不属于当前项目实例")
	}
	if err := verifyRestoredDatabase(ctx, filepath.Join(staging, "state.db"), current); err != nil {
		return Manifest{}, err
	}
	if err := replaceProjectState(current, staging); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// CheckIntegrity 检查 SQLite、外键以及数据库引用的受管 Artifact。
func CheckIntegrity(ctx context.Context, current *project.Project) (IntegrityReport, error) {
	database, err := storage.OpenExisting(ctx, current.Paths.Database)
	if err != nil {
		return IntegrityReport{}, err
	}
	defer database.Close()
	problems, err := databaseProblems(ctx, database)
	if err != nil {
		return IntegrityReport{}, err
	}
	artifactProblems, err := managedArtifactProblems(ctx, database, current.Paths.Artifacts)
	if err != nil {
		return IntegrityReport{}, err
	}
	problems = append(problems, artifactProblems...)
	return IntegrityReport{OK: len(problems) == 0, Problems: problems}, nil
}

type backupFile struct {
	Source      string
	ArchivePath string
}

type fileMetadata struct {
	Size   int64
	SHA256 string
}

func collectBackupFiles(current *project.Project) ([]backupFile, error) {
	files := []backupFile{
		{Source: current.Paths.ProjectConfig, ArchivePath: "project.toml"},
		{Source: current.Paths.LocalConfig, ArchivePath: "local.toml"},
		{Source: current.Paths.Database, ArchivePath: "state.db"},
	}
	err := filepath.WalkDir(current.Paths.Artifacts, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("Artifact 不是普通文件: %s", path)
		}
		relative, err := filepath.Rel(current.Paths.Artifacts, path)
		if err != nil {
			return err
		}
		files = append(files, backupFile{Source: path, ArchivePath: filepath.ToSlash(filepath.Join("artifacts", relative))})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("扫描 Artifact: %w", err)
	}
	sort.Slice(files, func(first, second int) bool { return files[first].ArchivePath < files[second].ArchivePath })
	return files, nil
}

func checkpointDatabase(ctx context.Context, path string) error {
	database, err := storage.OpenExisting(ctx, path)
	if err != nil {
		return err
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("checkpoint SQLite WAL: %w", err)
	}
	return nil
}

func inspectFile(path string) (fileMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return fileMetadata{}, fmt.Errorf("打开备份文件 %q: %w", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return fileMetadata{}, fmt.Errorf("计算备份文件哈希 %q: %w", path, err)
	}
	return fileMetadata{Size: size, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func writeArchiveAtomically(outputPath string, files []backupFile, manifest Manifest) error {
	if strings.TrimSpace(outputPath) == "" {
		return errors.New("备份输出路径不能为空")
	}
	outputPath, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return fmt.Errorf("创建备份目录: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(outputPath), ".ats-backup-*.zip")
	if err != nil {
		return fmt.Errorf("创建临时备份: %w", err)
	}
	temporaryPath := temporary.Name()
	succeeded := false
	defer func() {
		_ = temporary.Close()
		if !succeeded {
			_ = os.Remove(temporaryPath)
		}
	}()
	writer := zip.NewWriter(temporary)
	for _, file := range files {
		if err := addArchiveFile(writer, file); err != nil {
			_ = writer.Close()
			return err
		}
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	header := &zip.FileHeader{Name: manifestName, Method: zip.Deflate}
	header.SetMode(0o600)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	if _, err := entry.Write(encoded); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("完成备份压缩: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("原子保存备份: %w", err)
	}
	succeeded = true
	return nil
}

func addArchiveFile(writer *zip.Writer, input backupFile) error {
	file, err := os.Open(input.Source)
	if err != nil {
		return err
	}
	defer file.Close()
	header := &zip.FileHeader{Name: input.ArchivePath, Method: zip.Deflate}
	header.SetMode(0o600)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, file)
	return err
}

func extractAndVerifyArchive(archivePath, staging string) (Manifest, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return Manifest{}, fmt.Errorf("打开备份: %w", err)
	}
	defer reader.Close()
	if len(reader.File) > maxArchiveFiles {
		return Manifest{}, errors.New("备份文件数量超过限制")
	}
	var total uint64
	for _, entry := range reader.File {
		total += entry.UncompressedSize64
		if total > uint64(maxArchiveSize) {
			return Manifest{}, errors.New("备份解压大小超过 10 GiB")
		}
		if !safeArchivePath(entry.Name) || entry.FileInfo().Mode()&os.ModeSymlink != 0 {
			return Manifest{}, fmt.Errorf("备份包含不安全路径 %q", entry.Name)
		}
		if err := extractArchiveFile(entry, staging); err != nil {
			return Manifest{}, err
		}
	}
	if err := os.MkdirAll(filepath.Join(staging, "artifacts"), 0o700); err != nil {
		return Manifest{}, err
	}
	manifestContent, err := os.ReadFile(filepath.Join(staging, manifestName))
	if err != nil {
		return Manifest{}, errors.New("备份缺少 manifest.json")
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestContent, &manifest); err != nil || manifest.FormatVersion != 1 {
		return Manifest{}, errors.New("备份 manifest 无效或版本不受支持")
	}
	if err := verifyManifest(staging, manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func safeArchivePath(name string) bool {
	cleaned := filepath.ToSlash(filepath.Clean(name))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || filepath.IsAbs(name) {
		return false
	}
	return cleaned == manifestName || cleaned == "project.toml" || cleaned == "local.toml" || cleaned == "state.db" || strings.HasPrefix(cleaned, "artifacts/")
}

func extractArchiveFile(entry *zip.File, staging string) error {
	if entry.FileInfo().IsDir() {
		return nil
	}
	target := filepath.Join(staging, filepath.FromSlash(entry.Name))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	input, err := entry.Open()
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, io.LimitReader(input, int64(entry.UncompressedSize64)+1))
	closeErr := output.Close()
	return errors.Join(copyErr, closeErr)
}

func verifyManifest(staging string, manifest Manifest) error {
	required := map[string]bool{"project.toml": false, "local.toml": false, "state.db": false}
	seen := make(map[string]struct{}, len(manifest.Files))
	for _, expected := range manifest.Files {
		if !safeArchivePath(expected.Path) || expected.Path == manifestName {
			return fmt.Errorf("manifest 包含不安全路径 %q", expected.Path)
		}
		if _, exists := seen[expected.Path]; exists {
			return fmt.Errorf("manifest 路径重复 %q", expected.Path)
		}
		seen[expected.Path] = struct{}{}
		metadata, err := inspectFile(filepath.Join(staging, filepath.FromSlash(expected.Path)))
		if err != nil || metadata.Size != expected.Size || metadata.SHA256 != expected.SHA256 {
			return fmt.Errorf("备份文件校验失败 %q", expected.Path)
		}
		if _, exists := required[expected.Path]; exists {
			required[expected.Path] = true
		}
	}
	for path, found := range required {
		if !found {
			return fmt.Errorf("manifest 缺少 %s", path)
		}
	}
	return nil
}

func verifyRestoredDatabase(ctx context.Context, path string, current *project.Project) error {
	database, err := storage.OpenExisting(ctx, path)
	if err != nil {
		return fmt.Errorf("打开恢复数据库: %w", err)
	}
	defer database.Close()
	metadata, err := storage.ReadProjectMetadata(ctx, database)
	if err != nil {
		return err
	}
	if metadata.InstanceID != current.InstanceID || metadata.GitCommonDir != current.GitCommonDir {
		return errors.New("恢复数据库的项目或 Git 身份不匹配")
	}
	problems, err := databaseProblems(ctx, database)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("恢复数据库完整性失败: %s", strings.Join(problems, "; "))
	}
	return nil
}

func replaceProjectState(current *project.Project, staging string) error {
	rollback, err := os.MkdirTemp(current.Paths.Runtime, "restore-rollback-")
	if err != nil {
		return err
	}
	cleanupRollback := true
	defer func() {
		if cleanupRollback {
			removeManagedTemp(current.Paths.Runtime, rollback)
		}
	}()
	targets := []string{current.Paths.ProjectConfig, current.Paths.LocalConfig, current.Paths.Database,
		current.Paths.Database + "-wal", current.Paths.Database + "-shm", current.Paths.Artifacts}
	moved := make([]string, 0, len(targets))
	for _, target := range targets {
		if _, statErr := os.Lstat(target); errors.Is(statErr, os.ErrNotExist) {
			continue
		} else if statErr != nil {
			return statErr
		}
		if err := os.Rename(target, filepath.Join(rollback, filepath.Base(target))); err != nil {
			rollbackState(moved, rollback)
			return fmt.Errorf("暂存当前项目状态: %w", err)
		}
		moved = append(moved, target)
	}
	replacements := []struct{ source, target string }{
		{filepath.Join(staging, "project.toml"), current.Paths.ProjectConfig},
		{filepath.Join(staging, "local.toml"), current.Paths.LocalConfig},
		{filepath.Join(staging, "state.db"), current.Paths.Database},
		{filepath.Join(staging, "artifacts"), current.Paths.Artifacts},
	}
	installed := make([]string, 0, len(replacements))
	for _, replacement := range replacements {
		if err := os.Rename(replacement.source, replacement.target); err != nil {
			for _, target := range installed {
				_ = os.Rename(target, filepath.Join(staging, filepath.Base(target)))
			}
			rollbackState(moved, rollback)
			return fmt.Errorf("安装恢复状态: %w", err)
		}
		installed = append(installed, replacement.target)
	}
	return nil
}

func rollbackState(targets []string, rollback string) {
	for _, target := range targets {
		_ = os.Rename(filepath.Join(rollback, filepath.Base(target)), target)
	}
}

func removeManagedTemp(runtimeRoot, path string) {
	relative, err := filepath.Rel(runtimeRoot, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return
	}
	_ = os.RemoveAll(path)
}

func databaseProblems(ctx context.Context, database *sql.DB) ([]string, error) {
	var integrity string
	if err := database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return nil, fmt.Errorf("运行 SQLite integrity_check: %w", err)
	}
	problems := make([]string, 0)
	if integrity != "ok" {
		problems = append(problems, "SQLite: "+integrity)
	}
	rows, err := database.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var table, parent string
		var rowID sql.NullInt64
		var constraint int
		if err := rows.Scan(&table, &rowID, &parent, &constraint); err != nil {
			return nil, err
		}
		problems = append(problems, fmt.Sprintf("外键: %s row %v -> %s", table, rowID, parent))
	}
	return problems, rows.Err()
}

func managedArtifactProblems(ctx context.Context, database *sql.DB, root string) ([]string, error) {
	queries := []string{
		"SELECT original_relative_path, original_size, original_sha256 FROM artifacts UNION ALL SELECT optimized_relative_path, optimized_size, optimized_sha256 FROM artifacts",
		"SELECT relative_path, size, sha256 FROM run_artifacts",
	}
	problems := make([]string, 0)
	for _, query := range queries {
		rows, err := database.QueryContext(ctx, query)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var relative, expectedHash string
			var expectedSize int64
			if err := rows.Scan(&relative, &expectedSize, &expectedHash); err != nil {
				rows.Close()
				return nil, err
			}
			path, err := managedArtifactPath(root, relative)
			if err != nil {
				problems = append(problems, err.Error())
				continue
			}
			metadata, err := inspectFile(path)
			if err != nil {
				problems = append(problems, "Artifact 缺失: "+relative)
				continue
			}
			if metadata.Size != expectedSize || metadata.SHA256 != expectedHash {
				problems = append(problems, "Artifact 校验失败: "+relative)
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return problems, nil
}

func managedArtifactPath(root, relative string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(relative))
	if cleaned == "." || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("Artifact 路径不安全: %s", relative)
	}
	return filepath.Join(root, cleaned), nil
}
