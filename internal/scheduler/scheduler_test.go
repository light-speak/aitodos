package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	domainrun "github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestRunPublishesDispatchFailureAndRecovers(t *testing.T) {
	currentProject, database := initializeSchedulerProject(t)
	scheduler := New(currentProject, database)
	scheduler.pollInterval = time.Millisecond
	scheduler.maxErrorBackoff = 2 * time.Millisecond
	secondAttempt := make(chan struct{})
	thirdAttempt := make(chan struct{})
	releaseSecond := make(chan struct{})
	call := 0
	scheduler.dispatch = func(context.Context) error {
		call++
		if call == 1 {
			return errors.New("database unavailable")
		}
		if call == 2 {
			close(secondAttempt)
			<-releaseSecond
		} else if call == 3 {
			close(thirdAttempt)
		}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		scheduler.Run(ctx)
		close(done)
	}()

	<-secondAttempt
	failed := scheduler.Health()
	if failed.ConsecutiveFailures != 1 || !strings.Contains(failed.LastError, "database unavailable") {
		t.Fatalf("failed health = %#v", failed)
	}
	close(releaseSecond)
	<-thirdAttempt
	recovered := scheduler.Health()
	if recovered.ConsecutiveFailures != 0 || recovered.LastError != "" {
		t.Fatalf("recovered health = %#v", recovered)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
}

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

func TestDispatchOnceMarksRunLostWhenRunnerCannotLaunch(t *testing.T) {
	currentProject, database := initializeSchedulerProject(t)
	created, err := storage.NewTaskStore(database).Create(context.Background(), task.CreateInput{Title: "Runner 启动失败"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := currentProject.UpdateWorkerSettings(true, 1); err != nil {
		t.Fatal(err)
	}
	scheduler := New(currentProject, database)
	scheduler.launch = func(context.Context, launchClaim) error { return errors.New("executable missing") }

	if err := scheduler.DispatchOnce(context.Background()); err == nil || !strings.Contains(err.Error(), "executable missing") {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	runs, err := scheduler.runs.ListTaskRuns(context.Background(), created.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("ListTaskRuns() = %#v, %v", runs, err)
	}
	if runs[0].Status != domainrun.StatusLost || runs[0].FailureCode != "RUNNER_SPAWN_FAILED" {
		t.Fatalf("run = %#v", runs[0])
	}
}

func TestWaitAndReconcileMarksActiveRunLostAndReleasesResources(t *testing.T) {
	currentProject, database := initializeSchedulerProject(t)
	if _, err := storage.NewTaskStore(database).Create(context.Background(), task.CreateInput{Title: "Runner 异常退出"}); err != nil {
		t.Fatal(err)
	}
	scheduler := New(currentProject, database)
	claim, err := scheduler.runs.ClaimNextTask(context.Background(), 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claimData := launchClaim{RunID: claim.Run.ID, ClaimToken: claim.ClaimToken, LeaseGeneration: claim.Run.LeaseGeneration, RunNonce: claim.Run.RunNonce}
	audit := storage.NewMCPAuditStore(database)
	if _, err := audit.OpenResourceLease(context.Background(), claim.Run.ID, "BROWSER_SESSION", "playwright"); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^$")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}

	scheduler.waitAndReconcile(command, claimData)
	loaded, err := scheduler.runs.Get(context.Background(), claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != domainrun.StatusLost || loaded.FailureCode != "RUNNER_LOST" {
		t.Fatalf("run = %#v", loaded)
	}
	leases, err := audit.ListRunResourceLeases(context.Background(), claim.Run.ID)
	if err != nil || len(leases) != 1 || leases[0].State != "RELEASED" {
		t.Fatalf("leases = %#v, %v", leases, err)
	}
}

func TestLaunchRunnerReportsExecutableFailures(t *testing.T) {
	currentProject, database := initializeSchedulerProject(t)
	scheduler := New(currentProject, database)
	claim := launchClaim{RunID: "run-1", ClaimToken: "secret", LeaseGeneration: 2, RunNonce: "nonce"}

	scheduler.execError = errors.New("cannot resolve executable")
	if err := scheduler.launchRunner(context.Background(), claim); err == nil || !strings.Contains(err.Error(), "cannot resolve") {
		t.Fatalf("launchRunner() error = %v", err)
	}
	scheduler.execError = nil
	scheduler.executable = filepath.Join(t.TempDir(), "missing-ats")
	if err := scheduler.launchRunner(context.Background(), claim); err == nil || !strings.Contains(err.Error(), "start runner process") {
		t.Fatalf("launchRunner() error = %v", err)
	}
}

func TestRunnerEnvironmentRemovesClaimSecrets(t *testing.T) {
	t.Setenv("ATS_CLAIM_TOKEN", "raw-token")
	t.Setenv("ATS_CLAIM_FD", "99")
	t.Setenv("ATS_LEASE_GENERATION", "99")
	t.Setenv("ATS_SCHEDULER_TEST_VALUE", "preserved")
	environment := runnerEnvironment(7)
	joined := strings.Join(environment, "\n")
	if strings.Contains(joined, "raw-token") || strings.Contains(joined, "ATS_CLAIM_FD=99") || strings.Contains(joined, "ATS_LEASE_GENERATION=99") {
		t.Fatalf("runner environment leaked stale claim state: %q", joined)
	}
	for _, expected := range []string{"ATS_CLAIM_FD=3", "ATS_LEASE_GENERATION=7", "ATS_SCHEDULER_TEST_VALUE=preserved"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("runner environment missing %q: %q", expected, joined)
		}
	}
}

func TestActiveRunStatus(t *testing.T) {
	active := []domainrun.Status{domainrun.StatusClaimed, domainrun.StatusStarting, domainrun.StatusRunning, domainrun.StatusFinalizing}
	for _, status := range active {
		if !activeRunStatus(status) {
			t.Fatalf("activeRunStatus(%q) = false", status)
		}
	}
	for _, status := range []domainrun.Status{domainrun.StatusSucceeded, domainrun.StatusFailed, domainrun.StatusLost, domainrun.StatusCancelled} {
		if activeRunStatus(status) {
			t.Fatalf("activeRunStatus(%q) = true", status)
		}
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
