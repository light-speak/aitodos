package scheduler

import (
	"context"
	"database/sql"
	"os/exec"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestDispatchOnceRespectsProjectWorkerToggle(t *testing.T) {
	currentProject, database := initializeSchedulerProject(t)
	created, err := storage.NewTaskStore(database).Create(context.Background(), task.CreateInput{Title: "等待 Worker"})
	if err != nil {
		t.Fatal(err)
	}
	launched := make(chan string, 1)
	scheduler := New(currentProject, database)
	scheduler.launch = func(_ context.Context, claim launchClaim) error {
		launched <- claim.RunID
		return nil
	}

	if err := scheduler.DispatchOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-launched:
		t.Fatal("disabled Workers launched a Run")
	default:
	}
	if _, err := currentProject.UpdateWorkerSettings(true, 1); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.DispatchOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case runID := <-launched:
		if runID == "" {
			t.Fatal("launched empty Run ID")
		}
	case <-time.After(time.Second):
		t.Fatal("enabled Workers did not launch a Run")
	}
	loaded, err := storage.NewTaskStore(database).Get(context.Background(), created.ID)
	if err != nil || loaded.Status != task.StatusRunning {
		t.Fatalf("task = %#v, %v", loaded, err)
	}
}

func initializeSchedulerProject(t *testing.T) (*project.Project, *sql.DB) {
	t.Helper()
	repoRoot := t.TempDir()
	if output, err := exec.Command("git", "init", "--quiet", repoRoot).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	currentProject, _, err := project.Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	database, err := storage.OpenExisting(context.Background(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	profiles := storage.NewAgentProfileStore(database)
	profile, err := profiles.GetByRole(context.Background(), agentprofile.RoleImplementer)
	if err != nil {
		t.Fatal(err)
	}
	revision := profile.CurrentRevision
	if _, err := profiles.CreateRevision(context.Background(), profile.ID, agentprofile.RevisionInput{
		Instructions: revision.Instructions, Adapter: "generic", Command: "fake-agent",
		MaxInputTokens: revision.MaxInputTokens, ReservedOutputTokens: revision.ReservedOutputTokens,
		RecentMessageLimit: revision.RecentMessageLimit, RetrievalLimit: revision.RetrievalLimit,
		TimeoutSeconds: revision.TimeoutSeconds,
	}); err != nil {
		t.Fatal(err)
	}
	return currentProject, database
}
