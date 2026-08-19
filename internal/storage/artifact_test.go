package storage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/artifact"
)

func TestArtifactStoreCreatesAndReadsImageVariants(t *testing.T) {
	database := openTaskTestDatabase(t)
	root := filepath.Join(t.TempDir(), "artifacts")
	store := NewArtifactStore(database, root)
	original := []byte("original image")
	optimized := []byte("optimized image")

	created, err := store.CreateImage(context.Background(), artifact.CreateImageInput{
		OriginalName:       "screen.png",
		OriginalMediaType:  "image/png",
		Original:           original,
		OptimizedMediaType: "image/jpeg",
		Optimized:          optimized,
	})
	if err != nil {
		t.Fatalf("CreateImage() error = %v", err)
	}
	if created.Kind != artifact.KindImage || created.OriginalName != "screen.png" {
		t.Fatalf("created artifact = %#v", created)
	}

	loaded, err := store.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if loaded.OriginalSHA256 == "" || loaded.OptimizedSHA256 == "" {
		t.Fatalf("artifact hashes are empty: %#v", loaded)
	}

	originalContent, originalType, err := store.ReadImageVariant(context.Background(), created.ID, artifact.VariantOriginal)
	if err != nil {
		t.Fatalf("ReadImageVariant(original) error = %v", err)
	}
	if originalType != "image/png" || !bytes.Equal(originalContent, original) {
		t.Fatalf("original = %q, %q", originalContent, originalType)
	}
	optimizedContent, optimizedType, err := store.ReadImageVariant(context.Background(), created.ID, artifact.VariantOptimized)
	if err != nil {
		t.Fatalf("ReadImageVariant(optimized) error = %v", err)
	}
	if optimizedType != "image/jpeg" || !bytes.Equal(optimizedContent, optimized) {
		t.Fatalf("optimized = %q, %q", optimizedContent, optimizedType)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(created.OriginalRelativePath))); err != nil {
		t.Fatalf("original file: %v", err)
	}
}

func TestArtifactStoreRejectsEmptyImageContent(t *testing.T) {
	store := NewArtifactStore(openTaskTestDatabase(t), filepath.Join(t.TempDir(), "artifacts"))
	_, err := store.CreateImage(context.Background(), artifact.CreateImageInput{
		OriginalName: "empty.png", OriginalMediaType: "image/png", OptimizedMediaType: "image/png",
	})
	if err == nil {
		t.Fatal("CreateImage() error = nil, want non-nil")
	}
}

func TestArtifactStoreRejectsSymlinkPathEscape(t *testing.T) {
	database := openTaskTestDatabase(t)
	root := filepath.Join(t.TempDir(), "artifacts")
	store := NewArtifactStore(database, root)
	created, err := store.CreateImage(context.Background(), artifact.CreateImageInput{
		OriginalName: "screen.png", OriginalMediaType: "image/png", Original: []byte("original"),
		OptimizedMediaType: "image/png", Optimized: []byte("optimized"),
	})
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.png"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE artifacts SET optimized_relative_path = ? WHERE id = ?", "escape/secret.png", created.ID); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.ReadImageVariant(context.Background(), created.ID, artifact.VariantOptimized); err == nil {
		t.Fatal("ReadImageVariant() error = nil, want path escape rejection")
	}
}
