package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/domain/artifact"
)

// ErrArtifactNotFound 表示指定 Artifact 不存在。
var ErrArtifactNotFound = errors.New("artifact not found")

// ArtifactStore 管理数据库索引和项目 Artifact 文件。
type ArtifactStore struct {
	database *sql.DB
	root     string
}

// NewArtifactStore 创建项目级 Artifact Store。
func NewArtifactStore(database *sql.DB, root string) *ArtifactStore {
	return &ArtifactStore{database: database, root: root}
}

// CreateImage 原子写入图片的原始版本和优化版本，再记录索引。
func (store *ArtifactStore) CreateImage(ctx context.Context, input artifact.CreateImageInput) (artifact.Artifact, error) {
	if err := input.Validate(); err != nil {
		return artifact.Artifact{}, err
	}
	id, err := newID()
	if err != nil {
		return artifact.Artifact{}, err
	}
	directory := filepath.Join(store.root, "images", id)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return artifact.Artifact{}, fmt.Errorf("create image artifact directory: %w", err)
	}
	originalPath := filepath.Join(directory, "original"+imageExtension(input.OriginalMediaType))
	optimizedPath := filepath.Join(directory, "optimized"+imageExtension(input.OptimizedMediaType))
	if err := writeArtifactFile(originalPath, input.Original); err != nil {
		cleanupImageFiles(directory, originalPath, optimizedPath)
		return artifact.Artifact{}, err
	}
	if err := writeArtifactFile(optimizedPath, input.Optimized); err != nil {
		cleanupImageFiles(directory, originalPath, optimizedPath)
		return artifact.Artifact{}, err
	}

	created := artifact.Artifact{
		ID: id, Kind: artifact.KindImage, OriginalName: filepath.Base(input.OriginalName),
		OriginalMediaType: input.OriginalMediaType, OriginalRelativePath: relativeArtifactPath(store.root, originalPath),
		OriginalSize: int64(len(input.Original)), OriginalSHA256: contentHash(input.Original),
		OptimizedMediaType: input.OptimizedMediaType, OptimizedRelativePath: relativeArtifactPath(store.root, optimizedPath),
		OptimizedSize: int64(len(input.Optimized)), OptimizedSHA256: contentHash(input.Optimized),
		CreatedAt: time.Now().UTC(),
	}
	if err := store.insert(ctx, created); err != nil {
		cleanupImageFiles(directory, originalPath, optimizedPath)
		return artifact.Artifact{}, err
	}
	return created, nil
}

// Get 读取 Artifact 索引。
func (store *ArtifactStore) Get(ctx context.Context, id string) (artifact.Artifact, error) {
	var loaded artifact.Artifact
	var createdAt string
	err := store.database.QueryRowContext(ctx, `
SELECT id, kind, original_name, original_media_type, original_relative_path,
       original_size, original_sha256, optimized_media_type, optimized_relative_path,
       optimized_size, optimized_sha256, created_at
FROM artifacts WHERE id = ?`, id).Scan(
		&loaded.ID, &loaded.Kind, &loaded.OriginalName, &loaded.OriginalMediaType,
		&loaded.OriginalRelativePath, &loaded.OriginalSize, &loaded.OriginalSHA256,
		&loaded.OptimizedMediaType, &loaded.OptimizedRelativePath, &loaded.OptimizedSize,
		&loaded.OptimizedSHA256, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return artifact.Artifact{}, ErrArtifactNotFound
	}
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("get artifact: %w", err)
	}
	loaded.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return artifact.Artifact{}, fmt.Errorf("parse artifact created time: %w", err)
	}
	return loaded, nil
}

// ReadImageVariant 校验受管相对路径后读取指定图片版本。
func (store *ArtifactStore) ReadImageVariant(ctx context.Context, id string, variant artifact.ImageVariant) ([]byte, string, error) {
	loaded, err := store.Get(ctx, id)
	if err != nil {
		return nil, "", err
	}
	relativePath := loaded.OptimizedRelativePath
	mediaType := loaded.OptimizedMediaType
	if variant == artifact.VariantOriginal {
		relativePath = loaded.OriginalRelativePath
		mediaType = loaded.OriginalMediaType
	} else if variant != artifact.VariantOptimized {
		return nil, "", errors.New("unsupported image variant")
	}
	path, err := resolveArtifactPath(store.root, relativePath)
	if err != nil {
		return nil, "", err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read image artifact: %w", err)
	}
	return content, mediaType, nil
}

func (store *ArtifactStore) insert(ctx context.Context, value artifact.Artifact) error {
	_, err := store.database.ExecContext(ctx, `
INSERT INTO artifacts (
    id, kind, original_name, original_media_type, original_relative_path,
    original_size, original_sha256, optimized_media_type, optimized_relative_path,
    optimized_size, optimized_sha256, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.ID, value.Kind, value.OriginalName, value.OriginalMediaType, value.OriginalRelativePath,
		value.OriginalSize, value.OriginalSHA256, value.OptimizedMediaType, value.OptimizedRelativePath,
		value.OptimizedSize, value.OptimizedSHA256, formatTime(value.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert image artifact: %w", err)
	}
	return nil
}

func writeArtifactFile(path string, content []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".artifact-write-*")
	if err != nil {
		return fmt.Errorf("create artifact temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return fmt.Errorf("write artifact: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close artifact: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace artifact: %w", err)
	}
	return nil
}

func resolveArtifactPath(root, relativePath string) (string, error) {
	if filepath.IsAbs(relativePath) || relativePath == "" {
		return "", errors.New("invalid artifact relative path")
	}
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve artifact root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(cleanRoot)
	if err != nil {
		return "", fmt.Errorf("resolve artifact root symlinks: %w", err)
	}
	resolved := filepath.Join(resolvedRoot, filepath.FromSlash(relativePath))
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve artifact path symlinks: %w", err)
	}
	relative, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("artifact path escapes managed root")
	}
	return resolved, nil
}

func relativeArtifactPath(root, path string) string {
	relative, _ := filepath.Rel(root, path)
	return filepath.ToSlash(relative)
}

func imageExtension(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

func contentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func cleanupImageFiles(directory string, paths ...string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
	_ = os.Remove(directory)
}
