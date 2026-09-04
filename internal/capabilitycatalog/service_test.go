package capabilitycatalog

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/capability"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestServiceAddsHashedProjectSkill(t *testing.T) {
	root := t.TempDir()
	skillDirectory := filepath.Join(root, ".agents", "skills", "release")
	if err := os.MkdirAll(skillDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDirectory, "SKILL.md"), []byte("# Release\n\n检查版本。\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	database := openCatalogDatabase(t, root)
	service := New(root, "codex", storage.NewCapabilityStore(database))
	created, err := service.AddSkill(context.Background(), capability.SkillInput{
		Name: "发布", SourcePath: ".agents/skills/release",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.ContentSHA256) != 64 {
		t.Fatalf("content sha = %q", created.ContentSHA256)
	}
}

func TestServiceRefreshesChangedSkillContent(t *testing.T) {
	root := t.TempDir()
	skillDirectory := filepath.Join(root, ".agents", "skills", "release")
	if err := os.MkdirAll(skillDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	skillFile := filepath.Join(skillDirectory, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("# Release\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New(root, "codex", storage.NewCapabilityStore(openCatalogDatabase(t, root)))
	created, err := service.AddSkill(context.Background(), capability.SkillInput{
		Name: "发布", SourcePath: ".agents/skills/release",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillFile, []byte("# Release\n\n检查 Tag。\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	refreshed, err := service.RefreshSkill(context.Background(), created.ID, created.Version)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Version != 2 || refreshed.ContentSHA256 == created.ContentSHA256 {
		t.Fatalf("refreshed = %#v", refreshed)
	}
}

func TestServiceRejectsRelativeSkillSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions differ on Windows")
	}
	root := t.TempDir()
	external := t.TempDir()
	if err := os.WriteFile(filepath.Join(external, "SKILL.md"), []byte("# External"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".agents", "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, ".agents", "skills", "external")); err != nil {
		t.Fatal(err)
	}
	service := New(root, "codex", storage.NewCapabilityStore(openCatalogDatabase(t, root)))
	_, err := service.AddSkill(context.Background(), capability.SkillInput{
		Name: "外部", SourcePath: ".agents/skills/external",
	})
	if err == nil {
		t.Fatal("AddSkill() should reject a relative symlink escape")
	}
}

func TestServiceRequiresRegisteredCodexMCP(t *testing.T) {
	root := t.TempDir()
	fake := filepath.Join(root, "fake-codex")
	script := "#!/bin/sh\n[ \"$1\" = mcp ] && [ \"$2\" = get ] && [ \"$3\" = playwright ] && exit 0\nexit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	service := New(root, fake, storage.NewCapabilityStore(openCatalogDatabase(t, root)))
	if _, err := service.AddMCPServer(context.Background(), capability.MCPServerInput{
		Name: "浏览器", ConfigName: "playwright",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddMCPServer(context.Background(), capability.MCPServerInput{
		Name: "缺失", ConfigName: "missing",
	}); err == nil {
		t.Fatal("AddMCPServer() should reject an unregistered MCP")
	}
}

func TestServiceListsCatalogAndReadsAbsoluteSkill(t *testing.T) {
	root := t.TempDir()
	skillDirectory := filepath.Join(root, "skill")
	if err := os.MkdirAll(skillDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("# Review\n\n只读检查。\n")
	if err := os.WriteFile(filepath.Join(skillDirectory, "SKILL.md"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	service := New(root, "codex", storage.NewCapabilityStore(openCatalogDatabase(t, root)))
	created, err := service.AddSkill(context.Background(), capability.SkillInput{Name: "审查", SourcePath: skillDirectory})
	if err != nil {
		t.Fatal(err)
	}
	read, hash, err := ReadSkillContent(root, skillDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if string(read) != string(content) || hash != created.ContentSHA256 {
		t.Fatalf("read = %q, hash = %q", read, hash)
	}
	catalog, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Skills) != 1 || catalog.Skills[0].ID != created.ID {
		t.Fatalf("catalog = %#v", catalog)
	}
}

func TestServiceRejectsInvalidSkillFilesAndMissingCodexCommand(t *testing.T) {
	root := t.TempDir()
	store := storage.NewCapabilityStore(openCatalogDatabase(t, root))
	service := New(root, "", store)
	if _, err := service.AddMCPServer(context.Background(), capability.MCPServerInput{
		Name: "浏览器", ConfigName: "playwright",
	}); err == nil {
		t.Fatal("AddMCPServer() should require a Codex command")
	}

	regularFile := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(regularFile, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, sourcePath := range []string{"missing", regularFile} {
		if _, err := service.AddSkill(context.Background(), capability.SkillInput{Name: "无效", SourcePath: sourcePath}); err == nil {
			t.Fatalf("AddSkill(%q) should fail", sourcePath)
		}
	}

	tooLarge := filepath.Join(root, "too-large")
	if err := os.Mkdir(tooLarge, 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(tooLarge, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxSkillBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddSkill(context.Background(), capability.SkillInput{Name: "过大", SourcePath: tooLarge}); err == nil {
		t.Fatal("AddSkill() should reject an oversized SKILL.md")
	}
}

func openCatalogDatabase(t *testing.T, root string) *sql.DB {
	t.Helper()
	database, err := storage.Open(context.Background(), filepath.Join(root, ".ats", "state.db"), storage.ProjectMetadata{
		InstanceID: "project-test", Name: "test", RepoRoot: root, GitCommonDir: filepath.Join(root, ".git"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}
