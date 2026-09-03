package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/approvalrequest"
	"github.com/light-speak/aitodos/internal/domain/capability"
	"github.com/light-speak/aitodos/internal/domain/clarification"
	"github.com/light-speak/aitodos/internal/domain/discussion"
	"github.com/light-speak/aitodos/internal/domain/plan"
	"github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/topic"
	"github.com/light-speak/aitodos/internal/domain/workspace"
)

func TestRunStoreClaimsTopicPlanningOncePerVersionAndFinalizesDraft(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureProfile(t, database, agentprofile.RolePlanner)
	created, err := NewTopicStore(database).Create(ctx, topic.CreateInput{
		Title: "设计社区发帖", Description: "用户可以发布帖子",
	})
	if err != nil {
		t.Fatal(err)
	}
	store := NewRunStore(database)
	claim, err := store.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Run.Purpose != run.PurposePlanning || claim.Run.TopicID != created.ID ||
		claim.Run.TaskID != "" || claim.Run.SubjectVersion != created.Version {
		t.Fatalf("planning claim = %#v", claim)
	}
	if _, err := store.Start(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkRunning(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	planning := plan.PlanningResult{
		Reply: "需求已经足够明确，我整理了一版可审核方案。",
		Plan: &plan.RevisionInput{
			Summary: "实现最小社区发帖闭环",
			Drafts: []plan.TaskDraftInput{{
				Title: "实现帖子发布", Description: "提供创建和展示帖子能力",
				AcceptanceCriteria: "用户发布后可以看到帖子", Priority: 1,
				TestCases: []plan.TestCaseInput{{Title: "发布帖子", Required: true}},
			}},
		},
	}
	if _, err := store.BeginFinalization(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, FinalizationIntent{
		Finish: RunFinish{Status: run.StatusSucceeded, ExitCode: 0}, Planning: &planning,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteFinalization(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	summary, err := NewKnowledgeStore(database).GetRunSummary(ctx, claim.Run.ID)
	if err != nil || summary.Status != string(run.StatusSucceeded) || summary.Summary == "" {
		t.Fatalf("run summary = %#v, %v", summary, err)
	}
	view, err := NewPlanStore(database).GetByTopic(ctx, created.ID)
	if err != nil || view.Revision.SourceRunID != claim.Run.ID || view.Revision.Summary != planning.Plan.Summary {
		t.Fatalf("generated plan = %#v, %v", view, err)
	}
	messages, err := NewDiscussionStore(database).ListTopicMessages(ctx, created.ID)
	if err != nil || len(messages) != 1 || messages[0].AuthorKind != discussion.AuthorAgent || messages[0].Content != planning.Reply {
		t.Fatalf("agent messages = %#v, %v", messages, err)
	}
	if _, err := store.ClaimNextTask(ctx, 1, time.Minute); !errors.Is(err, ErrNoRunnableTask) {
		t.Fatalf("second planning claim error = %v", err)
	}
}

func TestRunStoreQueuesLatestTopicVersionAfterActivePlanningFinishes(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureProfile(t, database, agentprofile.RolePlanner)
	created, err := NewTopicStore(database).Create(ctx, topic.CreateInput{Title: "连续讨论"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewRunStore(database)
	first, err := store.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(ctx, first.Run.ID, first.ClaimToken, first.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkRunning(ctx, first.Run.ID, first.ClaimToken, first.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDiscussionStore(database).AppendTopicMessage(ctx, created.ID, discussion.CreateMessageInput{Content: "运行期间补充的新约束"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginFinalization(ctx, first.Run.ID, first.ClaimToken, first.Run.LeaseGeneration, FinalizationIntent{
		Finish:   RunFinish{Status: run.StatusSucceeded, ExitCode: 0},
		Planning: &plan.PlanningResult{Reply: "收到，我会结合新约束继续分析。"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteFinalization(ctx, first.Run.ID, first.ClaimToken, first.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	second, err := store.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.Run.TopicID != created.ID || second.Run.SubjectVersion != created.Version+1 ||
		second.Run.ContinuationOfRunID != first.Run.ID {
		t.Fatalf("continuation claim = %#v", second)
	}
}

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
	if _, err := store.Start(ctx, claim.Run.ID, "wrong", claim.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Hour); !errors.Is(err, ErrRunClaimMismatch) {
		t.Fatalf("Start(wrong token) error = %v", err)
	}
	started, err := store.Start(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Hour)
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
	finalizing, err := store.MarkFinalizing(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration)
	if err != nil || finalizing.Status != run.StatusFinalizing {
		t.Fatalf("MarkFinalizing() = %#v, %v", finalizing, err)
	}
	now := formatTime(time.Now().UTC())
	if _, err := database.ExecContext(ctx, `
INSERT INTO workspaces(
    id, task_id, path, branch_name, target_branch, base_commit_sha, head_sha,
    state, dirty, created_at, updated_at
) VALUES ('workspace-1', ?, '/managed/workspace-1', 'aitodos/task-1', 'main', 'base-sha', 'head-one', 'READY', 0, ?, ?)`, created.ID, now, now); err != nil {
		t.Fatal(err)
	}
	firstSnapshot, err := store.RecordWorkspaceSnapshot(ctx, run.WorkspaceSnapshot{
		RunID: claim.Run.ID, WorkspaceID: "workspace-1", BranchName: "aitodos/task-1",
		TargetBranch: "main", BaseCommitSHA: "base-sha", HeadBefore: "head-one",
		HeadAfter: "head-two", DirtyAfter: true, StateAfter: workspace.StateDirty,
	})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := store.RecordWorkspaceSnapshot(ctx, run.WorkspaceSnapshot{
		RunID: claim.Run.ID, WorkspaceID: "workspace-1", BranchName: "aitodos/task-1",
		TargetBranch: "main", BaseCommitSHA: "base-sha", HeadBefore: "wrong-head",
		HeadAfter: "wrong-after", StateAfter: workspace.StateReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.HeadBefore != firstSnapshot.HeadBefore || replayed.HeadAfter != firstSnapshot.HeadAfter || !replayed.DirtyAfter {
		t.Fatalf("replayed snapshot overwrote audit fact: %#v", replayed)
	}
	artifact, err := store.RecordArtifact(ctx, run.Artifact{
		RunID: claim.Run.ID, Kind: "PROMPT", RelativePath: "runs/example/prompt.md",
		SHA256: "abc", Size: 123,
	})
	if err != nil || artifact.ID == "" {
		t.Fatalf("RecordArtifact() = %#v, %v", artifact, err)
	}
	artifacts, err := store.ListArtifacts(ctx, claim.Run.ID)
	if err != nil || len(artifacts) != 1 || artifacts[0].ID != artifact.ID {
		t.Fatalf("ListArtifacts() = %#v, %v", artifacts, err)
	}
	loadedArtifact, err := store.GetArtifact(ctx, claim.Run.ID, "PROMPT")
	if err != nil || loadedArtifact.ID != artifact.ID {
		t.Fatalf("GetArtifact() = %#v, %v", loadedArtifact, err)
	}
	if _, err := store.GetArtifact(ctx, claim.Run.ID, "STDOUT"); err != ErrRunArtifactNotFound {
		t.Fatalf("missing GetArtifact() error = %v", err)
	}
	finished, err := store.Finish(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, RunFinish{
		Status: run.StatusFailed, ExitCode: 7, FailureKind: "AGENT_PROCESS",
		FailureCode: "NON_ZERO_EXIT", FailureMessage: "agent exited with status 7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != run.StatusFailed || finished.FinishedAt.IsZero() || finished.ExitCode == nil || *finished.ExitCode != 7 || finished.FailureCode != "NON_ZERO_EXIT" {
		t.Fatalf("finished run = %#v", finished)
	}
	events, err := store.ListEvents(ctx, claim.Run.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 || events[0].Type != run.EventClaimed || events[4].Type != run.EventStatusChanged {
		t.Fatalf("run events = %#v", events)
	}
	for index, event := range events {
		if event.Sequence != int64(index+1) {
			t.Fatalf("event sequence = %d at index %d", event.Sequence, index)
		}
	}
	resumed, err := store.ListEvents(ctx, claim.Run.ID, 3, 100)
	if err != nil || len(resumed) != 2 || resumed[0].Sequence != 4 {
		t.Fatalf("resumed events = %#v, %v", resumed, err)
	}
	loaded, err := tasks.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != task.StatusBlocked || loaded.LatestRunID != claim.Run.ID {
		t.Fatalf("task after finish = %#v", loaded)
	}
}

func TestRunStoreRenewsLeaseAndRecoversFrozenFinalization(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureRunnableProfiles(t, database)
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "恢复 Finalization"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewRunStore(database)
	claim, err := store.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Second); err != nil {
		t.Fatal(err)
	}
	before, err := store.Get(ctx, claim.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RenewLease(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, time.Minute); err != nil {
		t.Fatal(err)
	}
	after, err := store.Get(ctx, claim.Run.ID)
	if err != nil || !after.LeaseExpiresAt.After(before.LeaseExpiresAt) {
		t.Fatalf("renewed run = %#v, %v", after, err)
	}
	if _, err := store.MarkRunning(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginFinalization(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, FinalizationIntent{
		Finish: RunFinish{Status: run.StatusSucceeded, ExitCode: 0},
	}); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.RecoverFinalization(ctx, claim.Run.ID, claim.Run.LeaseGeneration)
	if err != nil || recovered.Status != run.StatusSucceeded {
		t.Fatalf("RecoverFinalization() = %#v, %v", recovered, err)
	}
	replayed, err := store.RecoverFinalization(ctx, claim.Run.ID, claim.Run.LeaseGeneration)
	if err != nil || replayed.Status != run.StatusSucceeded {
		t.Fatalf("replayed finalization = %#v, %v", replayed, err)
	}
	updated, err := NewTaskStore(database).Get(ctx, created.ID)
	if err != nil || updated.Status != task.StatusReview {
		t.Fatalf("task = %#v, %v", updated, err)
	}
}

func TestRunStoreTracksAgentProcessForCrashRecovery(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureProfile(t, database, agentprofile.RolePlanner)
	if _, err := NewTopicStore(database).Create(ctx, topic.CreateInput{Title: "Agent 进程恢复"}); err != nil {
		t.Fatal(err)
	}
	store := NewRunStore(database)
	claim, err := store.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	identity := strings.Repeat("b", 64)
	if _, err := store.Start(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.AttachAgentProcess(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 456, identity); err != nil {
		t.Fatal(err)
	}
	recoveryItems, err := store.ListRecoveryRuns(ctx)
	if err != nil || len(recoveryItems) != 1 || recoveryItems[0].AgentPID != 456 || recoveryItems[0].AgentIdentity != identity {
		t.Fatalf("recovery items = %#v, %v", recoveryItems, err)
	}
	if err := store.ReleaseAgentProcess(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	recoveryItems, err = store.ListRecoveryRuns(ctx)
	if err != nil || recoveryItems[0].AgentPID != 0 || recoveryItems[0].AgentIdentity != "" {
		t.Fatalf("released recovery items = %#v, %v", recoveryItems, err)
	}
}

func TestRunStoreRecoversNeedsInputFinalizationWithClarification(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureRunnableProfiles(t, database)
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "恢复澄清"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewRunStore(database)
	claim, err := store.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkRunning(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	request := clarification.Request{
		Category: clarification.CategoryDecision, Question: "选择兼容策略？",
		Options:             []clarification.Option{{ID: "safe", Label: "兼容", Description: "保留旧数据"}},
		RecommendedOptionID: "safe", AllowCustomAnswer: true,
	}
	if _, err := store.BeginFinalization(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, FinalizationIntent{
		Finish: RunFinish{Status: run.StatusNeedsInput, ExitCode: 0}, Clarification: &request,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecoverFinalization(ctx, claim.Run.ID, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	open, err := NewClarificationStore(database).ListOpen(ctx)
	if err != nil || len(open) != 1 || open[0].TaskID != created.ID {
		t.Fatalf("clarifications = %#v, %v", open, err)
	}
	updated, err := NewTaskStore(database).Get(ctx, created.ID)
	if err != nil || updated.Status != task.StatusBlocked {
		t.Fatalf("task = %#v, %v", updated, err)
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

func TestRunStoreRequestsCancellationAndBlocksTaskAfterRunnerFinalizes(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureRunnableProfiles(t, database)
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "取消当前执行"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewRunStore(database)
	claim, err := store.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkRunning(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	cancelled, err := store.RequestCancel(ctx, claim.Run.ID, "停止错误方向")
	if err != nil || cancelled.CancelRequestedAt == nil || cancelled.CancelReason != "停止错误方向" {
		t.Fatalf("RequestCancel() = %#v, %v", cancelled, err)
	}
	if _, err := store.RequestCancel(ctx, claim.Run.ID, "不得覆盖第一次原因"); err != nil {
		t.Fatal(err)
	}
	requested, err := store.CancellationRequested(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration)
	if err != nil || !requested {
		t.Fatalf("CancellationRequested() = %v, %v", requested, err)
	}
	if _, err := store.MarkFinalizing(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Finish(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, RunFinish{
		Status: run.StatusCancelled, ExitCode: -1,
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := NewTaskStore(database).Get(ctx, created.ID)
	if err != nil || updated.Status != task.StatusBlocked {
		t.Fatalf("task after cancellation = %#v, %v", updated, err)
	}
	retried, err := NewTaskStore(database).RetryBlocked(ctx, created.ID, updated.Version)
	if err != nil || retried.Status != task.StatusReady {
		t.Fatalf("retried task = %#v, %v", retried, err)
	}
	events, err := store.ListEvents(ctx, claim.Run.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	cancelEvents := 0
	for _, event := range events {
		if event.Type == run.EventCancelRequested {
			cancelEvents++
		}
	}
	if cancelEvents != 1 {
		t.Fatalf("cancel events = %d, events = %#v", cancelEvents, events)
	}
}

func TestRunStoreConcurrentCancellationDoesNotHitBusySnapshot(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureRunnableProfiles(t, database)
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "并发取消当前执行"})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := NewRunStore(database).ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRunStore(database).Start(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRunStore(database).MarkRunning(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	var sequence int
	var databaseName, path string
	if err := database.QueryRowContext(ctx, "PRAGMA database_list").Scan(&sequence, &databaseName, &path); err != nil {
		t.Fatal(err)
	}
	second, err := OpenExisting(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	stores := []*RunStore{NewRunStore(database), NewRunStore(second)}
	start := make(chan struct{})
	errorsFound := make(chan error, 8)
	var group sync.WaitGroup
	for index := range 8 {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			_, requestErr := stores[index%len(stores)].RequestCancel(ctx, claim.Run.ID, "并发停止")
			errorsFound <- requestErr
		}(index)
	}
	close(start)
	group.Wait()
	close(errorsFound)
	for requestErr := range errorsFound {
		if requestErr != nil {
			t.Fatalf("concurrent RequestCancel() error = %v", requestErr)
		}
	}
	current, err := NewRunStore(database).Get(ctx, claim.Run.ID)
	if err != nil || current.CancelRequestedAt == nil || current.CancelReason != "并发停止" || current.TaskID != created.ID {
		t.Fatalf("cancelled run = %#v, %v", current, err)
	}
}

func TestRunStorePersistsCodexSessionAndBindsCompatibleRetry(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureCodexImplementer(t, database)
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "复用 Agent Session"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewRunStore(database)
	first, err := store.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.RecordAgentSession(ctx, first.Run.ID, "019c8b9f-c6d5-7020-a5ed-e3a92c861e5d")
	if err != nil {
		t.Fatal(err)
	}
	if session.ExternalSessionID == "" || session.LastRunID != first.Run.ID {
		t.Fatalf("session = %#v", session)
	}
	if _, err := store.Finish(ctx, first.Run.ID, first.ClaimToken, first.Run.LeaseGeneration, RunFinish{
		Status: run.StatusCancelled, ExitCode: -1,
	}); err != nil {
		t.Fatal(err)
	}
	blocked, err := NewTaskStore(database).Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewTaskStore(database).RetryBlocked(ctx, created.ID, blocked.Version); err != nil {
		t.Fatal(err)
	}
	second, err := store.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.Run.AgentSessionID != session.ID || !second.Run.SessionResumed {
		t.Fatalf("retry run = %#v, want session %q", second.Run, session.ID)
	}
	loaded, err := store.GetAgentSessionForRun(ctx, second.Run.ID)
	if err != nil || loaded.ExternalSessionID != session.ExternalSessionID {
		t.Fatalf("GetAgentSessionForRun() = %#v, %v", loaded, err)
	}
	if err := store.InvalidateAgentSessionForRun(ctx, second.Run.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.GetAgentSessionForRun(ctx, second.Run.ID)
	if err != nil || loaded.Status != "INVALID" {
		t.Fatalf("invalidated session = %#v, %v", loaded, err)
	}
}

func TestRunStoreBridgesStructuredApprovalDecision(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureRunnableProfiles(t, database)
	if _, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "等待命令权限"}); err != nil {
		t.Fatal(err)
	}
	store := NewRunStore(database)
	claim, err := store.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkRunning(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateApprovalRequest(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, approvalrequest.CreateInput{
		ExternalRequestID: "rpc-17", ItemID: "item-1", Kind: approvalrequest.KindCommand,
		Reason: "需要运行项目测试", Command: "go test ./...", CWD: "/workspace",
		Available: []approvalrequest.Decision{
			approvalrequest.DecisionAcceptOnce, approvalrequest.DecisionAcceptSession,
			approvalrequest.DecisionDecline, approvalrequest.DecisionCancelRun,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	open, err := store.ListOpenApprovalRequests(ctx)
	if err != nil || len(open) != 1 || open[0].ID != created.ID {
		t.Fatalf("open approvals = %#v, %v", open, err)
	}
	runApprovals, err := store.ListRunApprovalRequests(ctx, claim.Run.ID)
	if err != nil || len(runApprovals) != 1 || runApprovals[0].ID != created.ID {
		t.Fatalf("run approvals = %#v, %v", runApprovals, err)
	}
	loadedApproval, err := store.GetApprovalRequest(ctx, created.ID)
	if err != nil || loadedApproval.ID != created.ID {
		t.Fatalf("GetApprovalRequest() = %#v, %v", loadedApproval, err)
	}
	resolved, err := store.ResolveApprovalRequest(ctx, created.ID, created.Version, approvalrequest.DecisionAcceptSession)
	if err != nil || resolved.Status != approvalrequest.StatusResolved || resolved.Decision != approvalrequest.DecisionAcceptSession {
		t.Fatalf("resolved approval = %#v, %v", resolved, err)
	}
	if _, err := store.ResolveApprovalRequest(ctx, created.ID, created.Version, approvalrequest.DecisionDecline); !errors.Is(err, ErrApprovalRequestConflict) {
		t.Fatalf("second decision error = %v", err)
	}
}

func TestRunStoreClearsApprovalAndQueriesRuns(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureRunnableProfiles(t, database)
	createdTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "查询执行记录"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewRunStore(database)
	claim, err := store.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkRunning(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	approval, err := store.CreateApprovalRequest(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, approvalrequest.CreateInput{
		ExternalRequestID: "rpc-clear", Kind: approvalrequest.KindCommand, Command: "go test ./...",
		Available: []approvalrequest.Decision{approvalrequest.DecisionAcceptOnce, approvalrequest.DecisionDecline},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ClearApprovalRequest(ctx, approval.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	cleared, err := store.GetApprovalRequest(ctx, approval.ID)
	if err != nil || cleared.Status != approvalrequest.StatusCleared {
		t.Fatalf("cleared approval = %#v, %v", cleared, err)
	}
	page, err := store.Query(ctx, RunQuery{TaskID: createdTask.ID, ActiveOnly: true, Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != claim.Run.ID {
		t.Fatalf("Query() = %#v, %v", page, err)
	}
	if _, err := store.Query(ctx, RunQuery{Limit: 0}); err == nil {
		t.Fatal("Query() accepted zero limit")
	}
}

func TestRunStoreRecoversLostRunAndBlocksTask(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureRunnableProfiles(t, database)
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "恢复丢失执行"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewRunStore(database)
	claim, err := store.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkRunning(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.RecoverLost(ctx, claim.Run.ID, claim.Run.LeaseGeneration, "RUNNER_LOST", "runner disappeared")
	if err != nil || recovered.Status != run.StatusLost || recovered.FailureCode != "RUNNER_LOST" {
		t.Fatalf("RecoverLost() = %#v, %v", recovered, err)
	}
	updated, err := NewTaskStore(database).Get(ctx, created.ID)
	if err != nil || updated.Status != task.StatusBlocked {
		t.Fatalf("task = %#v, %v", updated, err)
	}
	replayed, err := store.RecoverLost(ctx, claim.Run.ID, claim.Run.LeaseGeneration, "RUNNER_LOST", "runner disappeared")
	if err != nil || replayed.Status != run.StatusLost {
		t.Fatalf("replayed RecoverLost() = %#v, %v", replayed, err)
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

func configureCodexImplementer(t *testing.T, database *sql.DB) {
	t.Helper()
	store := NewAgentProfileStore(database)
	profile, err := store.GetByRole(context.Background(), agentprofile.RoleImplementer)
	if err != nil {
		t.Fatal(err)
	}
	revision := profile.CurrentRevision
	_, err = store.CreateRevision(context.Background(), profile.ID, agentprofile.RevisionInput{
		Instructions: revision.Instructions, Adapter: "codex", Command: "codex",
		Args: []string{"exec", "--json", "--sandbox", "workspace-write", "-"}, Model: "gpt-5.3-codex",
		MaxInputTokens: revision.MaxInputTokens, ReservedOutputTokens: revision.ReservedOutputTokens,
		RecentMessageLimit: revision.RecentMessageLimit, RetrievalLimit: revision.RetrievalLimit,
		TimeoutSeconds: revision.TimeoutSeconds,
	})
	if err != nil {
		t.Fatal(err)
	}
}
