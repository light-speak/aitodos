package runner

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/discussion"
	"github.com/light-speak/aitodos/internal/domain/plan"
	"github.com/light-speak/aitodos/internal/domain/quality"
	"github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/topic"
	"github.com/light-speak/aitodos/internal/gitworkflow"
	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestFakeAgentWorkflowFromTopicToIntegratedTask(t *testing.T) {
	ctx := context.Background()
	repoRoot := initializeRunnerRepository(t)
	currentProject, _, err := project.Initialize(ctx, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	commitWorkflowProjectConfig(t, repoRoot)
	database, err := storage.OpenExisting(ctx, currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	configureWorkflowFakeAgent(t, database, agentprofile.RolePlanner)
	configureWorkflowFakeAgent(t, database, agentprofile.RoleImplementer)

	createdTopic, err := storage.NewTopicStore(database).Create(ctx, topic.CreateInput{
		Title: "实现最小社区", Description: "用户可以发布文本帖子",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.NewDiscussionStore(database).AppendTopicMessage(ctx, createdTopic.ID, discussion.CreateMessageInput{
		Content: "第一版只需要文本帖子",
	}); err != nil {
		t.Fatal(err)
	}
	planningClaim := claimWorkflowRun(t, database, run.PurposePlanning)
	closeWorkflowDatabase(t, database)
	if err := Execute(ctx, currentProject, planningClaim.Run.ID, planningClaim.ClaimToken, planningClaim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}

	database = openWorkflowDatabase(t, currentProject)
	planView, err := storage.NewPlanStore(database).GetByTopic(ctx, createdTopic.ID)
	if err != nil {
		t.Fatal(err)
	}
	currentTopic, err := storage.NewTopicStore(database).Get(ctx, createdTopic.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, createdTasks, err := storage.NewPlanStore(database).Approve(ctx, planView.Plan.ID, plan.ReviewInput{
		ExpectedTopicVersion: currentTopic.Version,
		RevisionID:           planView.Revision.ID,
		Comment:              "批准最小范围",
	})
	if err != nil || len(createdTasks) != 1 {
		t.Fatalf("Approve() tasks = %#v, error = %v", createdTasks, err)
	}
	implementationClaim := claimWorkflowRun(t, database, run.PurposeImplementation)
	closeWorkflowDatabase(t, database)
	if err := Execute(ctx, currentProject, implementationClaim.Run.ID, implementationClaim.ClaimToken, implementationClaim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}

	database = openWorkflowDatabase(t, currentProject)
	t.Cleanup(func() { _ = database.Close() })
	createdTask := createdTasks[0]
	qualityView, err := storage.NewQualityStore(database).GetTaskQuality(ctx, createdTask.ID)
	if err != nil || len(qualityView.TestCases) < 1 {
		t.Fatalf("GetTaskQuality() = %#v, %v", qualityView, err)
	}
	for _, testCase := range qualityView.TestCases {
		if _, err := storage.NewQualityStore(database).AddTestResult(ctx, createdTask.ID, testCase.ID, quality.TestResultInput{
			Outcome: quality.OutcomePassed, EvidenceKind: quality.EvidenceHuman, Summary: "Fake E2E 人工确认",
		}); err != nil {
			t.Fatal(err)
		}
	}
	currentTask, err := storage.NewTaskStore(database).Get(ctx, createdTask.ID)
	if err != nil || currentTask.Status != task.StatusReview {
		t.Fatalf("implemented task = %#v, %v", currentTask, err)
	}
	manager := gitworkflow.New(currentProject, database)
	accepted, review, err := manager.ReviewTask(ctx, currentTask.ID, currentTask.Version, task.ReviewInput{
		Decision: task.ReviewAccepted, Comment: "Fake E2E 验收通过",
	})
	if err != nil || accepted.Status != task.StatusAccepted || review.CommitSHA == "" {
		t.Fatalf("ReviewTask() = %#v, %#v, %v", accepted, review, err)
	}
	integrated, err := manager.IntegrateTask(ctx, accepted.ID)
	if err != nil || integrated.TargetAfterSHA != review.CommitSHA {
		t.Fatalf("IntegrateTask() = %#v, %v", integrated, err)
	}
	content, err := os.ReadFile(filepath.Join(currentProject.Root, "runner-output.txt"))
	if err != nil || string(content) != "agent wrote this\n" {
		t.Fatalf("integrated output = %q, %v", content, err)
	}
	taskRuns, err := storage.NewRunStore(database).ListTaskRuns(ctx, accepted.ID)
	if err != nil || len(taskRuns) != 1 || taskRuns[0].Status != run.StatusSucceeded {
		t.Fatalf("task runs = %#v, %v", taskRuns, err)
	}
}

func commitWorkflowProjectConfig(t *testing.T, repoRoot string) {
	t.Helper()
	for _, arguments := range [][]string{
		{"add", ".ats/.gitignore", ".ats/project.toml"},
		{"commit", "--quiet", "-m", "configure aitodos"},
	} {
		command := exec.Command("git", arguments...)
		command.Dir = repoRoot
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", arguments, err, output)
		}
	}
}

func configureWorkflowFakeAgent(t *testing.T, database *sql.DB, role agentprofile.Role) {
	t.Helper()
	profiles := storage.NewAgentProfileStore(database)
	profile, err := profiles.GetByRole(t.Context(), role)
	if err != nil {
		t.Fatal(err)
	}
	revision := profile.CurrentRevision
	if _, err := profiles.CreateRevision(t.Context(), profile.ID, agentprofile.RevisionInput{
		Instructions: revision.Instructions, Adapter: "generic", Command: os.Args[0],
		Args: []string{"-test.run=TestRunnerFakeAgentProcess"}, MaxInputTokens: revision.MaxInputTokens,
		ReservedOutputTokens: revision.ReservedOutputTokens, RecentMessageLimit: revision.RecentMessageLimit,
		RetrievalLimit: revision.RetrievalLimit, TimeoutSeconds: 60,
	}); err != nil {
		t.Fatal(err)
	}
}

func claimWorkflowRun(t *testing.T, database *sql.DB, purpose run.Purpose) run.Claim {
	t.Helper()
	claim, err := storage.NewRunStore(database).ClaimNextTask(t.Context(), 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Run.Purpose != purpose {
		t.Fatalf("claimed purpose = %q, want %q", claim.Run.Purpose, purpose)
	}
	return claim
}

func openWorkflowDatabase(t *testing.T, currentProject *project.Project) *sql.DB {
	t.Helper()
	database, err := storage.OpenExisting(t.Context(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	return database
}

func closeWorkflowDatabase(t *testing.T, database *sql.DB) {
	t.Helper()
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
}
