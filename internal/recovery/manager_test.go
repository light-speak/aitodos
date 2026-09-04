package recovery

import (
	"context"
	"database/sql"
	"errors"
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

func TestStartTerminatesTrackedAgentAndReleasesBrowserResources(t *testing.T) {
	currentProject, database, claim := recoveryRun(t)
	store := storage.NewRunStore(database)
	identity := strings.Repeat("b", 64)
	if err := store.AttachAgentProcess(context.Background(), claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 456, identity); err != nil {
		t.Fatal(err)
	}
	audit := storage.NewMCPAuditStore(database)
	if _, err := audit.OpenResourceLease(context.Background(), claim.Run.ID, "BROWSER_SESSION", "playwright"); err != nil {
		t.Fatal(err)
	}
	manager := New(currentProject, database)
	manager.matchesProcess = func(context.Context, int, string) bool { return false }
	terminated := false
	manager.terminateAgent = func(_ context.Context, pid int, expected string) error {
		terminated = pid == 456 && expected == identity
		return nil
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	leases, err := audit.ListRunResourceLeases(context.Background(), claim.Run.ID)
	if err != nil || !terminated || len(leases) != 1 || leases[0].State != "RELEASED" {
		t.Fatalf("terminated=%v leases=%#v err=%v", terminated, leases, err)
	}
}

func TestReconcileTrackedMarksRunnerLostAfterItExits(t *testing.T) {
	currentProject, database, claim := recoveryRun(t)
	alive := true
	manager := New(currentProject, database)
	manager.matchesProcess = func(context.Context, int, string) bool { return alive }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if len(manager.tracked) != 1 {
		t.Fatalf("tracked = %#v", manager.tracked)
	}
	alive = false
	manager.reconcileTracked(context.Background())
	if len(manager.tracked) != 0 {
		t.Fatalf("tracked after exit = %#v", manager.tracked)
	}
	loaded, err := storage.NewRunStore(database).Get(context.Background(), claim.Run.ID)
	if err != nil || loaded.Status != run.StatusLost {
		t.Fatalf("run = %#v, %v", loaded, err)
	}
}

func TestStartMarksFinalizationRecoveryFailureLost(t *testing.T) {
	currentProject, database, claim := recoveryRun(t)
	store := storage.NewRunStore(database)
	if _, err := store.MarkFinalizing(context.Background(), claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	manager := New(currentProject, database)
	manager.matchesProcess = func(context.Context, int, string) bool { return false }
	manager.recoverFinalization = func(context.Context, *project.Project, *sql.DB, storage.RecoveryRun) error {
		return errors.New("artifact unavailable")
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Get(context.Background(), claim.Run.ID)
	if err != nil || loaded.Status != run.StatusLost || loaded.FailureCode != "FINALIZATION_RECOVERY_FAILED" {
		t.Fatalf("run = %#v, %v", loaded, err)
	}
}

func TestAgentCleanupFailureAbandonsBrowserResource(t *testing.T) {
	currentProject, database, claim := recoveryRun(t)
	store := storage.NewRunStore(database)
	identity := strings.Repeat("b", 64)
	if err := store.AttachAgentProcess(context.Background(), claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 456, identity); err != nil {
		t.Fatal(err)
	}
	audit := storage.NewMCPAuditStore(database)
	if _, err := audit.OpenResourceLease(context.Background(), claim.Run.ID, "BROWSER_SESSION", "playwright"); err != nil {
		t.Fatal(err)
	}
	manager := New(currentProject, database)
	manager.matchesProcess = func(context.Context, int, string) bool { return false }
	manager.terminateAgent = func(context.Context, int, string) error { return errors.New("identity mismatch") }
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	leases, err := audit.ListRunResourceLeases(context.Background(), claim.Run.ID)
	if err != nil || len(leases) != 1 || leases[0].State != "ABANDONED" {
		t.Fatalf("leases = %#v, %v", leases, err)
	}
	loaded, err := store.Get(context.Background(), claim.Run.ID)
	if err != nil || !strings.Contains(loaded.FailureMessage, "Agent 资源未能确认关闭") {
		t.Fatalf("run = %#v, %v", loaded, err)
	}
}

func TestIsActive(t *testing.T) {
	for _, status := range []run.Status{run.StatusClaimed, run.StatusStarting, run.StatusRunning, run.StatusFinalizing} {
		if !isActive(status) {
			t.Fatalf("isActive(%q) = false", status)
		}
	}
	for _, status := range []run.Status{run.StatusSucceeded, run.StatusFailed, run.StatusLost, run.StatusCancelled} {
		if isActive(status) {
			t.Fatalf("isActive(%q) = true", status)
		}
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
