package recovery

import (
	"context"
	"database/sql"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestStartMarksDeadRunningRunLostWithoutRetry(t *testing.T) {
	currentProject, database, claim := recoveryRun(t)
	manager := New(currentProject, database)
	manager.matchesProcess = func(context.Context, int, string) bool { return false }
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, err := storage.NewRunStore(database).Get(context.Background(), claim.Run.ID)
	if err != nil || current.Status != run.StatusLost || current.FailureCode != "RUNNER_LOST" {
		t.Fatalf("recovered run = %#v, %v", current, err)
	}
	updated, err := storage.NewTaskStore(database).Get(context.Background(), claim.Run.TaskID)
	if err != nil || updated.Status != task.StatusBlocked {
		t.Fatalf("recovered task = %#v, %v", updated, err)
	}
	runs, err := storage.NewRunStore(database).ListTaskRuns(context.Background(), claim.Run.TaskID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("recovery invoked a duplicate Run: %#v, %v", runs, err)
	}
}

func TestStartLeavesIdentityMatchedRunnerActive(t *testing.T) {
	currentProject, database, claim := recoveryRun(t)
	manager := New(currentProject, database)
	manager.matchesProcess = func(context.Context, int, string) bool { return true }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := storage.NewRunStore(database).Get(context.Background(), claim.Run.ID)
	if err != nil || current.Status != run.StatusRunning {
		t.Fatalf("live run = %#v, %v", current, err)
	}
}

func recoveryRun(t *testing.T) (*project.Project, *sql.DB, run.Claim) {
	t.Helper()
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "--quiet", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	currentProject, _, err := project.Initialize(context.Background(), root)
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
	created, err := storage.NewTaskStore(database).Create(context.Background(), task.CreateInput{Title: "恢复旧 Runner"})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := storage.NewRunStore(database).ClaimNextTask(context.Background(), 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	store := storage.NewRunStore(database)
	if _, err := store.Start(context.Background(), claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkRunning(context.Background(), claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	claim.Run.TaskID = created.ID
	return currentProject, database, claim
}
