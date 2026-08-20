package storage

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/capability"
	"github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
)

func TestRunStoreClaimsRevisionThenPriorityAndHonorsCapacity(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureRunnableProfiles(t, database)
	tasks := NewTaskStore(database)
	revisionTask, err := tasks.Create(ctx, task.CreateInput{Title: "修订旧任务", Priority: 3})
	if err != nil {
		t.Fatal(err)
	}
	reviewTask, err := tasks.ApplyCommand(ctx, revisionTask.ID, revisionTask.Version, task.CommandSubmitReview)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tasks.ApplyReview(ctx, reviewTask.ID, reviewTask.Version, task.ReviewInput{
		Decision: task.ReviewRejected, Comment: "测试失败",
	}, ""); err != nil {
		t.Fatal(err)
	}
	p0, err := tasks.Create(ctx, task.CreateInput{Title: "紧急任务", Priority: 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.Create(ctx, task.CreateInput{Title: "普通任务", Priority: 1}); err != nil {
		t.Fatal(err)
	}

	runs := NewRunStore(database)
	first, err := runs.ClaimNextTask(ctx, 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if first.Run.TaskID != revisionTask.ID || first.Run.Purpose != run.PurposeRevision || first.ClaimToken == "" {
		t.Fatalf("first claim = %#v", first)
	}
	second, err := runs.ClaimNextTask(ctx, 2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.Run.TaskID != p0.ID || second.Run.Purpose != run.PurposeImplementation {
		t.Fatalf("second claim = %#v", second)
	}
	if _, err := runs.ClaimNextTask(ctx, 2, time.Minute); !errors.Is(err, ErrRunCapacityReached) {
		t.Fatalf("third claim error = %v", err)
	}
}

func TestRunStoreClaimsTriageWithoutChangingTaskStatus(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureProfile(t, database, agentprofile.RoleTriager)
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{
		Description: "需要重新生成一个清晰标题并评估复杂度",
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := NewRunStore(database).ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Run.Purpose != run.PurposeTriage || claim.Run.SubjectVersion != created.AssessmentInputVersion {
		t.Fatalf("claim = %#v", claim)
	}
	loaded, err := NewTaskStore(database).Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != task.StatusReady || loaded.Version != created.Version {
		t.Fatalf("triage changed task = %#v", loaded)
	}
}

func TestRunStoreSnapshotsResolvedToolPolicyAtClaim(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	capabilities := NewCapabilityStore(database)
	skill, err := capabilities.CreateSkill(ctx, capability.SkillInput{
		Name: "任务评估", SourcePath: ".agents/skills/triage",
	}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	server, err := capabilities.CreateMCPServer(ctx, capability.MCPServerInput{
		Name: "代码搜索", ConfigName: "code-search",
	})
	if err != nil {
		t.Fatal(err)
	}
	profiles := NewAgentProfileStore(database)
	profile, err := profiles.GetByRole(ctx, agentprofile.RoleTriager)
	if err != nil {
		t.Fatal(err)
	}
	revision := profile.CurrentRevision
	if _, err := profiles.CreateRevision(ctx, profile.ID, agentprofile.RevisionInput{
		Instructions: revision.Instructions, Adapter: "codex", Command: "codex", Args: []string{"exec", "--json"},
		MaxInputTokens: revision.MaxInputTokens, ReservedOutputTokens: revision.ReservedOutputTokens,
		RecentMessageLimit: revision.RecentMessageLimit, RetrievalLimit: revision.RetrievalLimit,
		TimeoutSeconds: revision.TimeoutSeconds,
		ToolPolicy: capability.ToolPolicyInput{
			Skills:     []capability.SkillBindingInput{{SkillID: skill.ID, Required: true}},
			MCPServers: []capability.MCPBindingInput{{ServerID: server.ID, EnabledTools: []string{"search"}}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewTaskStore(database).Create(ctx, task.CreateInput{Description: "评估任务"}); err != nil {
		t.Fatal(err)
	}
	runs := NewRunStore(database)
	claim, err := runs.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := runs.GetToolPolicySnapshot(ctx, claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ProfileRevisionID != claim.Run.ProfileRevisionID || len(snapshot.Skills) != 1 ||
		len(snapshot.MCPServers) != 1 || snapshot.MCPServers[0].ConfigName != "code-search" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestRunStoreConcurrentClaimCreatesOnlyOneRun(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureRunnableProfiles(t, database)
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "只能领取一次", Priority: 0})
	if err != nil {
		t.Fatal(err)
	}
	store := NewRunStore(database)

	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, claimErr := store.ClaimNextTask(ctx, 2, time.Minute)
			results <- claimErr
		}()
	}
	close(start)
	group.Wait()
	close(results)
	succeeded := 0
	for claimErr := range results {
		if claimErr == nil {
			succeeded++
		} else if !errors.Is(claimErr, ErrNoRunnableTask) {
			t.Fatalf("claim error = %v", claimErr)
		}
	}
	if succeeded != 1 {
		t.Fatalf("successful claims = %d, want 1", succeeded)
	}
	listed, err := store.ListTaskRuns(ctx, created.ID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListTaskRuns() = %#v, %v", listed, err)
	}
}

func TestRunStoreAuthorizesLeaseAndFinalizesTask(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureRunnableProfiles(t, database)
	tasks := NewTaskStore(database)
	created, err := tasks.Create(ctx, task.CreateInput{Title: "执行生命周期"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewRunStore(database)
	claim, err := store.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(ctx, claim.Run.ID, "wrong", claim.Run.LeaseGeneration, 123, time.Hour); !errors.Is(err, ErrRunClaimMismatch) {
		t.Fatalf("Start(wrong token) error = %v", err)
	}
	started, err := store.Start(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if started.Status != run.StatusStarting || started.StartedAt.IsZero() {
		t.Fatalf("started run = %#v", started)
	}
	running, err := store.MarkRunning(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration)
	if err != nil || running.Status != run.StatusRunning {
		t.Fatalf("MarkRunning() = %#v, %v", running, err)
	}
	artifact, err := store.RecordArtifact(ctx, run.Artifact{
		RunID: claim.Run.ID, Kind: "PROMPT", RelativePath: "runs/example/prompt.md",
		SHA256: "abc", Size: 123,
	})
	if err != nil || artifact.ID == "" {
		t.Fatalf("RecordArtifact() = %#v, %v", artifact, err)
	}
	finished, err := store.Finish(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, RunFinish{
		Status: run.StatusSucceeded, ExitCode: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != run.StatusSucceeded || finished.FinishedAt.IsZero() {
		t.Fatalf("finished run = %#v", finished)
	}
	loaded, err := tasks.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != task.StatusReview || loaded.LatestRunID != claim.Run.ID {
		t.Fatalf("task after finish = %#v", loaded)
	}
}

func TestRunStoreRecordsUsageAndBuildsPurposeSummary(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureRunnableProfiles(t, database)
	tasks := NewTaskStore(database)
	if _, err := tasks.Create(ctx, task.CreateInput{Title: "统计真实用量"}); err != nil {
		t.Fatal(err)
	}
	store := NewRunStore(database)
	claim, err := store.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	usage, err := store.RecordUsage(ctx, run.Usage{
		RunID: claim.Run.ID, Source: run.UsageSourceCodexJSONL,
		InputTokens: tokenCount(71420), CachedInputTokens: tokenCount(56320),
		OutputTokens: tokenCount(916), ReasoningOutputTokens: tokenCount(276),
	})
	if err != nil {
		t.Fatal(err)
	}
	if usage.CapturedAt.IsZero() {
		t.Fatal("CapturedAt was not set")
	}
	loaded, err := store.GetUsage(ctx, claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.InputTokens == nil || *loaded.InputTokens != 71420 || loaded.CachedInputTokens == nil || *loaded.CachedInputTokens != 56320 {
		t.Fatalf("usage = %#v", loaded)
	}
	summary, err := store.UsageSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.RunsWithUsage != 1 || len(summary.ByPurpose) != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	implementation := summary.ByPurpose[0]
	if implementation.Purpose != run.PurposeImplementation || implementation.InputTokens == nil || *implementation.InputTokens != 71420 ||
		implementation.UncachedInputTokens == nil || *implementation.UncachedInputTokens != 15100 {
		t.Fatalf("implementation usage = %#v", implementation)
	}
}

func tokenCount(value int64) *int64 {
	return &value
}

func configureRunnableProfiles(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, role := range []agentprofile.Role{agentprofile.RoleImplementer, agentprofile.RoleRevision} {
		configureProfile(t, database, role)
	}
}

func configureProfile(t *testing.T, database *sql.DB, role agentprofile.Role) {
	t.Helper()
	store := NewAgentProfileStore(database)
	profile, err := store.GetByRole(context.Background(), role)
	if err != nil {
		t.Fatal(err)
	}
	revision := profile.CurrentRevision
	_, err = store.CreateRevision(context.Background(), profile.ID, agentprofile.RevisionInput{
		Instructions: revision.Instructions, Adapter: "generic", Command: "fake-agent",
		Args: []string{}, Model: "", MaxInputTokens: revision.MaxInputTokens,
		ReservedOutputTokens: revision.ReservedOutputTokens,
		RecentMessageLimit:   revision.RecentMessageLimit, RetrievalLimit: revision.RetrievalLimit,
		TimeoutSeconds: revision.TimeoutSeconds,
	})
	if err != nil {
		t.Fatal(err)
	}
}
