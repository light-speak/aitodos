package project

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitializeCreatesProjectLocalState(t *testing.T) {
	repoRoot := initGitRepository(t)
	nested := filepath.Join(repoRoot, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	initialized, created, err := Initialize(context.Background(), nested)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !created {
		t.Fatal("Initialize() created = false, want true")
	}
	if initialized.Root != repoRoot {
		t.Fatalf("Initialize() root = %q, want %q", initialized.Root, repoRoot)
	}
	if initialized.Config.Name != filepath.Base(repoRoot) {
		t.Fatalf("project name = %q, want %q", initialized.Config.Name, filepath.Base(repoRoot))
	}
	if initialized.Local.Agent.MaxWorkers != 2 {
		t.Fatalf("max workers = %d, want 2", initialized.Local.Agent.MaxWorkers)
	}
	if initialized.Local.Proxy.Mode != "inherit" {
		t.Fatalf("proxy mode = %q, want inherit", initialized.Local.Proxy.Mode)
	}
	if initialized.Local.Server.Port != 0 {
		t.Fatalf("server port = %d, want 0", initialized.Local.Server.Port)
	}

	wantPaths := []string{
		initialized.Paths.ProjectConfig,
		initialized.Paths.LocalConfig,
		initialized.Paths.GitIgnore,
		initialized.Paths.Database,
		initialized.Paths.Artifacts,
		initialized.Paths.Runtime,
		initialized.Paths.Worktrees,
	}
	for _, path := range wantPaths {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected path %q: %v", path, err)
		}
	}

	assertDatabaseInitialized(t, initialized.Paths.Database, initialized.InstanceID)

	again, createdAgain, err := Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatalf("second Initialize() error = %v", err)
	}
	if createdAgain {
		t.Fatal("second Initialize() created = true, want false")
	}
	if again.InstanceID != initialized.InstanceID {
		t.Fatalf("instance ID changed from %q to %q", initialized.InstanceID, again.InstanceID)
	}
}

func TestLoadReadsFixedLocalServerPort(t *testing.T) {
	repoRoot := initGitRepository(t)
	initialized, _, err := Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(initialized.Paths.LocalConfig)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(content), "port = 0", "port = 6170", 1)
	if updated == string(content) {
		t.Fatalf("local config does not contain default server port: %s", content)
	}
	if err := os.WriteFile(initialized.Paths.LocalConfig, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Local.Server.Port != 6170 {
		t.Fatalf("server port = %d, want 6170", loaded.Local.Server.Port)
	}
}

func TestWorkerSettingsPersistInLocalConfig(t *testing.T) {
	repoRoot := initGitRepository(t)
	initialized, _, err := Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if settings := initialized.WorkerSettings(); settings.Enabled || settings.MaxWorkers != 2 {
		t.Fatalf("default worker settings = %#v", settings)
	}

	updated, err := initialized.UpdateWorkerSettings(true, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Enabled || updated.MaxWorkers != 3 {
		t.Fatalf("updated worker settings = %#v", updated)
	}

	loaded, err := Load(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if settings := loaded.WorkerSettings(); !settings.Enabled || settings.MaxWorkers != 3 {
		t.Fatalf("reloaded worker settings = %#v", settings)
	}
}

func TestWorkerSettingsRejectInvalidConcurrencyWithoutChangingConfig(t *testing.T) {
	repoRoot := initGitRepository(t)
	initialized, _, err := Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(initialized.Paths.LocalConfig)
	if err != nil {
		t.Fatal(err)
	}

	for _, maxWorkers := range []int{0, 33} {
		if _, err := initialized.UpdateWorkerSettings(true, maxWorkers); err == nil {
			t.Fatalf("UpdateWorkerSettings(true, %d) error = nil", maxWorkers)
		}
	}
	after, err := os.ReadFile(initialized.Paths.LocalConfig)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("invalid update changed local.toml")
	}
}

func TestLoadRejectsInvalidLocalServerPort(t *testing.T) {
	repoRoot := initGitRepository(t)
	initialized, _, err := Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(initialized.Paths.LocalConfig)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(content), "port = 0", "port = 70000", 1)
	if err := os.WriteFile(initialized.Paths.LocalConfig, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = Load(context.Background(), repoRoot)
	if err == nil || !strings.Contains(err.Error(), "server.port") {
		t.Fatalf("Load() error = %v, want invalid server.port", err)
	}
}

func TestInitializeRejectsDirectoryOutsideGitRepository(t *testing.T) {
	_, _, err := Initialize(context.Background(), t.TempDir())
	if err == nil {
		t.Fatal("Initialize() error = nil, want non-nil")
	}
}

func TestInitializePrefersConventionalBranchOverCurrentFeatureBranch(t *testing.T) {
	repoRoot := t.TempDir()
	command := exec.Command("git", "init", "--quiet", "--initial-branch=feature/current", repoRoot)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	for _, args := range [][]string{
		{"config", "user.name", "AiTodos Test"},
		{"config", "user.email", "aitodos@example.invalid"},
		{"commit", "--allow-empty", "--quiet", "-m", "initial"},
		{"branch", "main"},
	} {
		command = exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}

	initialized, _, err := Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if initialized.Config.DefaultBranch != "main" {
		t.Fatalf("default branch = %q, want main", initialized.Config.DefaultBranch)
	}
}

func initGitRepository(t *testing.T) string {
	t.Helper()

	repoRoot := t.TempDir()
	command := exec.Command("git", "init", "--quiet", repoRoot)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	resolved, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func assertDatabaseInitialized(t *testing.T, databasePath string, instanceID string) {
	t.Helper()

	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var storedID string
	if err := database.QueryRow("SELECT instance_id FROM project_metadata WHERE id = 1").Scan(&storedID); err != nil {
		t.Fatalf("read project metadata: %v", err)
	}
	if storedID != instanceID {
		t.Fatalf("stored instance ID = %q, want %q", storedID, instanceID)
	}

	var version int
	if err := database.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != 28 {
		t.Fatalf("schema version = %d, want 28", version)
	}
}
