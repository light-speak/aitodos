package runner

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/capability"
	"github.com/light-speak/aitodos/internal/domain/clarification"
	"github.com/light-speak/aitodos/internal/domain/discussion"
	"github.com/light-speak/aitodos/internal/domain/quality"
	"github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/topic"
	"github.com/light-speak/aitodos/internal/domain/workspace"
	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestProcessResultKeepsLogTruncationSeparate(t *testing.T) {
	result := processResult{
		Stdout: []byte("out"), Stderr: []byte("err"),
		StdoutTruncated: true, StderrTruncated: false,
	}
	if !result.StdoutTruncated || result.StderrTruncated || bytes.Equal(result.Stdout, result.Stderr) {
		t.Fatalf("process result = %#v", result)
	}
}

func TestExecutePersistsClarificationAndRebuildsContinuationContext(t *testing.T) {
	repoRoot := initializeRunnerRepository(t)
	currentProject, _, err := project.Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	database, err := storage.OpenExisting(context.Background(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	profileStore := storage.NewAgentProfileStore(database)
	profile, err := profileStore.GetByRole(context.Background(), agentprofile.RoleImplementer)
	if err != nil {
		t.Fatal(err)
	}
	revision := profile.CurrentRevision
	if _, err := profileStore.CreateRevision(context.Background(), profile.ID, agentprofile.RevisionInput{
		Instructions: revision.Instructions, Adapter: "generic", Command: os.Args[0],
		Args: []string{"-test.run=TestRunnerFakeAgentProcess"}, MaxInputTokens: revision.MaxInputTokens,
		ReservedOutputTokens: revision.ReservedOutputTokens, RecentMessageLimit: revision.RecentMessageLimit,
		RetrievalLimit: revision.RetrievalLimit, TimeoutSeconds: 60,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := storage.NewTaskStore(database).Create(context.Background(), task.CreateInput{
		Title: "需要人工决定迁移策略", AcceptanceCriteria: "人工选择后继续",
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := storage.NewRunStore(database).ClaimNextTask(context.Background(), 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), currentProject, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}

	database, err = storage.OpenExisting(context.Background(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	questions, err := storage.NewClarificationStore(database).ListOpen(context.Background())
	if err != nil || len(questions) != 1 {
		t.Fatalf("open clarifications = %#v, %v", questions, err)
	}
	blocked, err := storage.NewTaskStore(database).Get(context.Background(), created.ID)
	if err != nil || blocked.Status != task.StatusBlocked {
		t.Fatalf("blocked task = %#v, %v", blocked, err)
	}
	if _, _, err := storage.NewClarificationStore(database).Answer(context.Background(), questions[0].ID, clarification.AnswerInput{
		SelectedOptionID: "compatible", ExpectedVersion: questions[0].Version,
	}); err != nil {
		t.Fatal(err)
	}
	continuation, err := storage.NewRunStore(database).ClaimNextTask(context.Background(), 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), currentProject, continuation.Run.ID, continuation.ClaimToken, continuation.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}

	database, err = storage.OpenExisting(context.Background(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	completed, err := storage.NewTaskStore(database).Get(context.Background(), created.ID)
	if err != nil || completed.Status != task.StatusReview {
		t.Fatalf("continued task = %#v, %v", completed, err)
	}
	prompt, err := os.ReadFile(filepath.Join(currentProject.Paths.Artifacts, "runs", continuation.Run.ID, "prompt.md"))
	if err != nil || !strings.Contains(string(prompt), "数据库迁移是否兼容旧版本") ||
		!strings.Contains(string(prompt), "compatible") || !strings.Contains(string(prompt), "Current Workspace") {
		t.Fatalf("continuation prompt = %q, %v", prompt, err)
	}
}

func TestValidateAgentResultRejectsAllDataBeforePersistence(t *testing.T) {
	result := agentResult{
		Estimate:     &agentEstimate{Points: 5, RemainingPoints: 2, Confidence: 0.8, Rationale: "有效估算"},
		NewTestCases: []agentTestCase{{Title: "无效测试结果", Required: true, Outcome: "UNKNOWN", Summary: "无效"}},
	}
	if err := validateAgentResult("run-1", result); err == nil {
		t.Fatal("validateAgentResult() error = nil")
	}
}

func TestCommandEvidenceParsesCodexAndAppServerEvents(t *testing.T) {
	stdout := []byte(strings.Join([]string{
		`{"type":"item.completed","item":{"type":"command_execution","command":"/bin/zsh -lc 'go test ./...'","exit_code":0,"status":"completed"}}`,
		`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","completedAtMs":1,"item":{"id":"item-1","type":"commandExecution","command":"pnpm test","commandActions":[],"cwd":"/workspace","status":"completed","aggregatedOutput":"failed","exitCode":1}}}`,
		`{"type":"item.completed","item":{"type":"command_execution","command":"go vet ./...","exit_code":null,"status":"in_progress"}}`,
	}, "\n"))

	executions := parseCommandExecutions(stdout)
	passed, ok := executions.match("go test ./...", quality.OutcomePassed)
	if !ok || passed.Command != "/bin/zsh -lc 'go test ./...'" || passed.ExitCode != 0 {
		t.Fatalf("passed command evidence = %#v, %v", passed, ok)
	}
	failed, ok := executions.match("pnpm test", quality.OutcomeFailed)
	if !ok || failed.Command != "pnpm test" || failed.ExitCode != 1 {
		t.Fatalf("failed command evidence = %#v, %v", failed, ok)
	}
	if _, ok := executions.match("go vet ./...", quality.OutcomePassed); ok {
		t.Fatal("incomplete command must not become evidence")
	}
	if len(observedCommandExecutions("generic", stdout)) != 0 {
		t.Fatal("generic adapter output must not be trusted as structured command evidence")
	}
}

func TestAgentTestResultUsesCommandOnlyWhenOutcomeMatchesObservedExit(t *testing.T) {
	executions := commandExecutions{
		"go test ./...": {{Command: "/bin/zsh -lc 'go test ./...'", ExitCode: 0}},
	}
	verified := agentTestResultInput("run-1", agentTestResult{
		Outcome: quality.OutcomePassed, Summary: "测试通过", Command: "go test ./...",
	}, executions)
	if verified.EvidenceKind != quality.EvidenceCommand || verified.Command == "" || verified.ArtifactRef != "runs/run-1/stdout.log" {
		t.Fatalf("verified input = %#v", verified)
	}
	unmatched := agentTestResultInput("run-1", agentTestResult{
		Outcome: quality.OutcomeFailed, Summary: "声称失败", Command: "go test ./...",
	}, executions)
	if unmatched.EvidenceKind != quality.EvidenceAgentReport || unmatched.Command != "" {
		t.Fatalf("unmatched input = %#v", unmatched)
	}
}

func TestInvocationArgsExpandsManagedResultFile(t *testing.T) {
	revision := agentprofile.Revision{Args: []string{"--output-last-message", "{result_file}", "-"}}
	args, usesPromptFile := invocationArgs(revision, run.PurposeImplementation, "/runtime/run-1", "run-1", "/artifacts/prompt.md", "")
	if usesPromptFile || len(args) != 3 || args[1] != "/runtime/run-1/.ats-run-result.json" {
		t.Fatalf("invocation args = %#v, prompt file = %v", args, usesPromptFile)
	}
}

func TestInvocationArgsOmitsEmptyOptionalModel(t *testing.T) {
	revision := agentprofile.Revision{Args: []string{"exec", "--model", "{model}", "-"}}
	args, _ := invocationArgs(revision, run.PurposeImplementation, "/runtime/run-1", "run-1", "/artifacts/prompt.md", "")
	if strings.Join(args, " ") != "exec -" {
		t.Fatalf("invocation args = %#v", args)
	}
}

func TestInvocationArgsInjectsCodexResumeBeforePrompt(t *testing.T) {
	revision := agentprofile.Revision{
		Adapter: "codex", Model: "gpt-5.3-codex",
		Args: []string{"exec", "--json", "--model", "{model}", "--sandbox", "workspace-write", "-"},
	}
	args, usesPromptFile := invocationArgs(
		revision, run.PurposeImplementation, "/runtime/run-2", "run-2", "/artifacts/prompt.md",
		"019c8b9f-c6d5-7020-a5ed-e3a92c861e5d",
	)
	if usesPromptFile || strings.Join(args, " ") != "exec --json --model gpt-5.3-codex --sandbox workspace-write resume 019c8b9f-c6d5-7020-a5ed-e3a92c861e5d -" {
		t.Fatalf("invocation args = %#v", args)
	}
}

func TestInvocationArgsAddsManagedPlanningResultForLegacyCodexProfile(t *testing.T) {
	revision := agentprofile.Revision{
		Adapter: "codex", Args: []string{"exec", "--json", "--sandbox", "read-only", "-"},
	}
	args, _ := invocationArgs(revision, run.PurposePlanning, "/runtime/run-3", "run-3", "/artifacts/prompt.md", "")
	want := "exec --json --sandbox read-only --output-last-message /runtime/run-3/.ats-run-result.json -"
	if strings.Join(args, " ") != want {
		t.Fatalf("invocation args = %#v, want %q", args, want)
	}
}

func TestInvocationArgsAddsManagedReviewResultForLegacyCodexProfile(t *testing.T) {
	revision := agentprofile.Revision{
		Adapter: "codex", Args: []string{"exec", "--json", "--sandbox", "read-only", "-"},
	}
	args, _ := invocationArgs(revision, run.PurposeReview, "/runtime/run-review", "run-review", "/artifacts/prompt.md", "")
	want := "exec --json --sandbox read-only --output-last-message /runtime/run-review/.ats-run-result.json -"
	if strings.Join(args, " ") != want {
		t.Fatalf("invocation args = %#v, want %q", args, want)
	}
}

func TestPersistAppServerReviewRejectsUnstructuredFinalMessage(t *testing.T) {
	err := persistAppServerFinalResult(run.PurposeReview, t.TempDir(), "普通文本回答")
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("persist review result error = %v", err)
	}
}

func TestParseCodexSessionIDUsesThreadStartedEvent(t *testing.T) {
	stdout := []byte("{\"type\":\"thread.started\",\"thread_id\":\"019c8b9f-c6d5-7020-a5ed-e3a92c861e5d\"}\n" +
		"{\"type\":\"future.event\",\"thread_id\":\"must-not-replace\"}\n")
	if sessionID := parseCodexSessionID(stdout); sessionID != "019c8b9f-c6d5-7020-a5ed-e3a92c861e5d" {
		t.Fatalf("session id = %q", sessionID)
	}
}

func TestParseCodexMCPListAndBuildsDenyByDefaultArgs(t *testing.T) {
	configured, err := parseCodexMCPList([]byte(`[
		{"name":"filesystem","enabled":true},
		{"name":"playwright","enabled":true}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	policy := []capability.MCPServerSnapshot{{
		ConfigName: "playwright", Enabled: true, Required: true, EnabledTools: []string{"navigate", "close"},
	}}
	args, err := codexToolPolicyArgs(configured, policy)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, expected := range []string{
		`mcp_servers.filesystem.enabled=false`,
		`mcp_servers.playwright.enabled=true`,
		`mcp_servers.playwright.required=true`,
		`mcp_servers.playwright.enabled_tools=["navigate","close"]`,
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("policy args = %q, missing %q", joined, expected)
		}
	}
}

func TestParseCodexUsageUsesLastCompletedTurn(t *testing.T) {
	stdout := []byte(`{"type":"thread.started","thread_id":"thread-1"}
{"type":"turn.completed","usage":{"input_tokens":16533,"cached_input_tokens":11008,"cache_write_input_tokens":0,"output_tokens":285,"reasoning_output_tokens":83}}
`)
	usage := parseCodexUsage(stdout)
	if usage == nil || usage.InputTokens == nil || *usage.InputTokens != 16533 ||
		usage.CachedInputTokens == nil || *usage.CachedInputTokens != 11008 ||
		usage.OutputTokens == nil || *usage.OutputTokens != 285 ||
		usage.ReasoningOutputTokens == nil || *usage.ReasoningOutputTokens != 83 {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestParseCodexUsageIgnoresUnknownAndInvalidEvents(t *testing.T) {
	stdout := []byte(`{"type":"future.event","usage":{"input_tokens":99}}
not-json
{"type":"turn.completed","usage":{"input_tokens":-1}}
`)
	if usage := parseCodexUsage(stdout); usage != nil {
		t.Fatalf("usage = %#v, want nil", usage)
	}
}

func TestCodexToolPolicyRejectsMissingRequiredServer(t *testing.T) {
	_, err := codexToolPolicyArgs(nil, []capability.MCPServerSnapshot{{
		ConfigName: "playwright", Enabled: true, Required: true,
	}})
	if err == nil {
		t.Fatal("codexToolPolicyArgs() should reject a missing required server")
	}
}

func TestLoadRunSkillsRejectsChangedRequiredSkill(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, ".agents", "skills", "review")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("# Review"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadRunSkillChunks(root, []capability.SkillSnapshot{{
		Name: "审查", SourcePath: ".agents/skills/review", ContentSHA256: strings.Repeat("0", 64),
		Enabled: true, Required: true,
	}})
	if err == nil {
		t.Fatal("loadRunSkillChunks() should reject a changed required skill")
	}
}

func TestExecuteRunsAgentInTaskWorkspaceAndFinalizesReview(t *testing.T) {
	repoRoot := initializeRunnerRepository(t)
	currentProject, _, err := project.Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	database, err := storage.OpenExisting(context.Background(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	profileStore := storage.NewAgentProfileStore(database)
	profile, err := profileStore.GetByRole(context.Background(), agentprofile.RoleImplementer)
	if err != nil {
		t.Fatal(err)
	}
	revision := profile.CurrentRevision
	if _, err := profileStore.CreateRevision(context.Background(), profile.ID, agentprofile.RevisionInput{
		Instructions: revision.Instructions, Adapter: "generic", Command: os.Args[0],
		Args: []string{"-test.run=TestRunnerFakeAgentProcess"}, MaxInputTokens: revision.MaxInputTokens,
		ReservedOutputTokens: revision.ReservedOutputTokens, RecentMessageLimit: revision.RecentMessageLimit,
		RetrievalLimit: revision.RetrievalLimit, TimeoutSeconds: 60,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := storage.NewTaskStore(database).Create(context.Background(), task.CreateInput{
		Title: "Runner 流程", Description: "由 fake agent 修改文件", AcceptanceCriteria: "生成 runner-output.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := storage.NewRunStore(database).ClaimNextTask(context.Background(), 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	if err := Execute(context.Background(), currentProject, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}

	database, err = storage.OpenExisting(context.Background(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	loaded, err := storage.NewTaskStore(database).Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != task.StatusReview {
		t.Fatalf("task status = %q, want REVIEW", loaded.Status)
	}
	workspace, err := storage.NewWorkspaceStore(database).GetByTask(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !workspace.Dirty || workspace.LastVerifiedAt == nil {
		t.Fatalf("workspace was not finalized after agent exit: %#v", workspace)
	}
	if content, err := os.ReadFile(filepath.Join(workspace.Path, "runner-output.txt")); err != nil || string(content) != "agent wrote this\n" {
		t.Fatalf("workspace output = %q, %v", content, err)
	}
	qualityData, err := storage.NewQualityStore(database).GetTaskQuality(context.Background(), created.ID)
	if err != nil || qualityData.Estimate == nil || qualityData.Estimate.SourceRunID != claim.Run.ID {
		t.Fatalf("quality = %#v, %v", qualityData, err)
	}
	prompt, err := os.ReadFile(filepath.Join(currentProject.Paths.Artifacts, "runs", claim.Run.ID, "prompt.md"))
	if err != nil || !strings.Contains(string(prompt), "生成 runner-output.txt") {
		t.Fatalf("prompt = %q, %v", prompt, err)
	}
	runs, err := storage.NewRunStore(database).ListTaskRuns(context.Background(), created.ID)
	if err != nil || len(runs) != 1 || runs[0].Status != run.StatusSucceeded {
		t.Fatalf("runs = %#v, %v", runs, err)
	}
	snapshot, err := storage.NewRunStore(database).GetWorkspaceSnapshot(context.Background(), claim.Run.ID)
	if err != nil || snapshot.HeadBefore == "" || snapshot.HeadAfter != workspace.HeadSHA || !snapshot.DirtyAfter {
		t.Fatalf("workspace snapshot = %#v, %v", snapshot, err)
	}
}

func TestExecutePlanningUsesTopicDiscussionAndCreatesReviewDraft(t *testing.T) {
	repoRoot := initializeRunnerRepository(t)
	currentProject, _, err := project.Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	database, err := storage.OpenExisting(context.Background(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	profiles := storage.NewAgentProfileStore(database)
	profile, err := profiles.GetByRole(context.Background(), agentprofile.RolePlanner)
	if err != nil {
		t.Fatal(err)
	}
	revision := profile.CurrentRevision
	if _, err := profiles.CreateRevision(context.Background(), profile.ID, agentprofile.RevisionInput{
		Instructions: revision.Instructions, Adapter: "generic", Command: os.Args[0],
		Args: []string{"-test.run=TestRunnerFakeAgentProcess"}, MaxInputTokens: revision.MaxInputTokens,
		ReservedOutputTokens: revision.ReservedOutputTokens, RecentMessageLimit: revision.RecentMessageLimit,
		RetrievalLimit: revision.RetrievalLimit, TimeoutSeconds: 60,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := storage.NewTopicStore(database).Create(context.Background(), topic.CreateInput{
		Title: "设计简单社区", Description: "用户可以发布帖子",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.NewDiscussionStore(database).AppendTopicMessage(context.Background(), created.ID, discussion.CreateMessageInput{
		Content: "第一版只需要文本帖子，不需要关注和点赞",
	}); err != nil {
		t.Fatal(err)
	}
	claim, err := storage.NewRunStore(database).ClaimNextTask(context.Background(), 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Run.Purpose != run.PurposePlanning {
		t.Fatalf("purpose = %q", claim.Run.Purpose)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), currentProject, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}

	database, err = storage.OpenExisting(context.Background(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	view, err := storage.NewPlanStore(database).GetByTopic(context.Background(), created.ID)
	if err != nil || view.Revision.SourceRunID != claim.Run.ID || len(view.Revision.Drafts) != 1 {
		t.Fatalf("plan = %#v, %v", view, err)
	}
	messages, err := storage.NewDiscussionStore(database).ListTopicMessages(context.Background(), created.ID)
	if err != nil || len(messages) != 2 || messages[1].AuthorKind != discussion.AuthorAgent {
		t.Fatalf("messages = %#v, %v", messages, err)
	}
	prompt, err := os.ReadFile(filepath.Join(currentProject.Paths.Artifacts, "runs", claim.Run.ID, "prompt.md"))
	if err != nil || !strings.Contains(string(prompt), "Current Topic") ||
		!strings.Contains(string(prompt), "第一版只需要文本帖子") {
		t.Fatalf("prompt = %q, %v", prompt, err)
	}
	if _, err := storage.NewWorkspaceStore(database).GetByTask(context.Background(), ""); !errors.Is(err, storage.ErrWorkspaceNotFound) {
		t.Fatalf("planning workspace error = %v", err)
	}
}

func TestExecuteReviewAnswersTaskDiscussionWithoutWorkspaceOrStateChange(t *testing.T) {
	repoRoot := initializeRunnerRepository(t)
	currentProject, _, err := project.Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	database, err := storage.OpenExisting(context.Background(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	profiles := storage.NewAgentProfileStore(database)
	profile, err := profiles.GetByRole(context.Background(), agentprofile.RoleReviewer)
	if err != nil {
		t.Fatal(err)
	}
	revision := profile.CurrentRevision
	if _, err := profiles.CreateRevision(context.Background(), profile.ID, agentprofile.RevisionInput{
		Instructions: revision.Instructions, Adapter: "generic", Command: os.Args[0],
		Args: []string{"-test.run=TestRunnerFakeAgentProcess"}, MaxInputTokens: revision.MaxInputTokens,
		ReservedOutputTokens: revision.ReservedOutputTokens, RecentMessageLimit: revision.RecentMessageLimit,
		RetrievalLimit: revision.RetrievalLimit, TimeoutSeconds: 60,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := storage.NewTaskStore(database).Create(context.Background(), task.CreateInput{Title: "讨论当前实现"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := storage.NewTaskFeedbackStore(database).Discuss(context.Background(), created.ID, discussion.CreateMessageInput{
		Content: "这个实现目前还有什么缺陷？",
	}); err != nil {
		t.Fatal(err)
	}
	claim, err := storage.NewRunStore(database).ClaimNextTask(context.Background(), 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Run.Purpose != run.PurposeReview {
		t.Fatalf("purpose = %q", claim.Run.Purpose)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Execute(context.Background(), currentProject, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	database, err = storage.OpenExisting(context.Background(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	loaded, err := storage.NewTaskStore(database).Get(context.Background(), created.ID)
	if err != nil || loaded.Status != task.StatusReady {
		t.Fatalf("task after review = %#v, %v", loaded, err)
	}
	messages, err := storage.NewDiscussionStore(database).ListTaskMessages(context.Background(), created.ID)
	if err != nil || len(messages) != 2 || messages[1].AuthorKind != discussion.AuthorAgent {
		t.Fatalf("messages = %#v, %v", messages, err)
	}
	if _, err := storage.NewWorkspaceStore(database).GetByTask(context.Background(), created.ID); !errors.Is(err, storage.ErrWorkspaceNotFound) {
		t.Fatalf("review workspace error = %v", err)
	}
	prompt, err := os.ReadFile(filepath.Join(currentProject.Paths.Artifacts, "runs", claim.Run.ID, "prompt.md"))
	if err != nil || !strings.Contains(string(prompt), "Current Task Question") || !strings.Contains(string(prompt), "这个实现目前还有什么缺陷？") {
		t.Fatalf("review prompt = %q, %v", prompt, err)
	}
}

func TestExecuteFinalizesWorkspaceWhenAgentResultIsInvalid(t *testing.T) {
	repoRoot := initializeRunnerRepository(t)
	currentProject, _, err := project.Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	database, err := storage.OpenExisting(context.Background(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	profiles := storage.NewAgentProfileStore(database)
	profile, err := profiles.GetByRole(context.Background(), agentprofile.RoleImplementer)
	if err != nil {
		t.Fatal(err)
	}
	revision := profile.CurrentRevision
	if _, err := profiles.CreateRevision(context.Background(), profile.ID, agentprofile.RevisionInput{
		Instructions: revision.Instructions, Adapter: "generic", Command: os.Args[0],
		Args: []string{"-test.run=TestRunnerFakeAgentProcess"}, MaxInputTokens: revision.MaxInputTokens,
		ReservedOutputTokens: revision.ReservedOutputTokens, RecentMessageLimit: revision.RecentMessageLimit,
		RetrievalLimit: revision.RetrievalLimit, TimeoutSeconds: 60,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := storage.NewTaskStore(database).Create(context.Background(), task.CreateInput{
		Title: "无效结果仍需收尾", AcceptanceCriteria: "保留 Agent 产生的修改并记录失败",
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := storage.NewRunStore(database).ClaimNextTask(context.Background(), 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	if err := Execute(context.Background(), currentProject, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err == nil {
		t.Fatal("Execute() error = nil, want invalid result error")
	}
	database, err = storage.OpenExisting(context.Background(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	currentRun, err := storage.NewRunStore(database).Get(context.Background(), claim.Run.ID)
	if err != nil || currentRun.Status != run.StatusFailed || currentRun.FailureCode != "RESULT" {
		t.Fatalf("run = %#v, %v", currentRun, err)
	}
	workspace, err := storage.NewWorkspaceStore(database).GetByTask(context.Background(), created.ID)
	if err != nil || !workspace.Dirty || workspace.LastVerifiedAt == nil {
		t.Fatalf("workspace = %#v, %v", workspace, err)
	}
	snapshot, err := storage.NewRunStore(database).GetWorkspaceSnapshot(context.Background(), claim.Run.ID)
	if err != nil || !snapshot.DirtyAfter || snapshot.HeadAfter != workspace.HeadSHA {
		t.Fatalf("workspace snapshot = %#v, %v", snapshot, err)
	}
}

func TestExecuteStopsAgentAfterCancellationRequestAndPreservesWorkspace(t *testing.T) {
	repoRoot := initializeRunnerRepository(t)
	currentProject, _, err := project.Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	database, err := storage.OpenExisting(context.Background(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	profiles := storage.NewAgentProfileStore(database)
	profile, err := profiles.GetByRole(context.Background(), agentprofile.RoleImplementer)
	if err != nil {
		t.Fatal(err)
	}
	revision := profile.CurrentRevision
	if _, err := profiles.CreateRevision(context.Background(), profile.ID, agentprofile.RevisionInput{
		Instructions: revision.Instructions, Adapter: "generic", Command: os.Args[0],
		Args: []string{"-test.run=TestRunnerFakeAgentProcess"}, MaxInputTokens: revision.MaxInputTokens,
		ReservedOutputTokens: revision.ReservedOutputTokens, RecentMessageLimit: revision.RecentMessageLimit,
		RetrievalLimit: revision.RetrievalLimit, TimeoutSeconds: 60,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := storage.NewTaskStore(database).Create(context.Background(), task.CreateInput{
		Title: "等待取消", AcceptanceCriteria: "停止 Agent 并保留 Workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	store := storage.NewRunStore(database)
	claim, err := store.ClaimNextTask(context.Background(), 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- Execute(context.Background(), currentProject, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration)
	}()

	workspace := waitForAgentMarker(t, database, created.ID)
	if _, err := store.RequestCancel(context.Background(), claim.Run.ID, "人工停止当前方向"); err != nil {
		t.Fatal(err)
	}
	select {
	case executeErr := <-done:
		if executeErr != nil {
			t.Fatal(executeErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Runner did not stop after cancellation request")
	}
	currentRun, err := store.Get(context.Background(), claim.Run.ID)
	if err != nil || currentRun.Status != run.StatusCancelled || currentRun.CancelReason != "人工停止当前方向" {
		t.Fatalf("cancelled run = %#v, %v", currentRun, err)
	}
	updated, err := storage.NewTaskStore(database).Get(context.Background(), created.ID)
	if err != nil || updated.Status != task.StatusBlocked {
		t.Fatalf("task after cancel = %#v, %v", updated, err)
	}
	snapshot, err := store.GetWorkspaceSnapshot(context.Background(), claim.Run.ID)
	if err != nil || !snapshot.DirtyAfter || snapshot.WorkspaceID != workspace.ID {
		t.Fatalf("workspace snapshot = %#v, %v", snapshot, err)
	}
}

func waitForAgentMarker(t *testing.T, database *sql.DB, taskID string) workspace.Workspace {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case <-ticker.C:
			current, err := storage.NewWorkspaceStore(database).GetByTask(context.Background(), taskID)
			if err == nil {
				if _, statErr := os.Stat(filepath.Join(current.Path, "agent-started")); statErr == nil {
					return current
				}
			}
		case <-timeout.C:
			t.Fatal("Agent process did not create its start marker")
		}
	}
}

func TestExecuteTriageUsesRuntimeDirectoryAndKeepsTaskReady(t *testing.T) {
	repoRoot := initializeRunnerRepository(t)
	currentProject, _, err := project.Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	database, err := storage.OpenExisting(context.Background(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	profiles := storage.NewAgentProfileStore(database)
	profile, err := profiles.GetByRole(context.Background(), agentprofile.RoleTriager)
	if err != nil {
		t.Fatal(err)
	}
	revision := profile.CurrentRevision
	if _, err := profiles.CreateRevision(context.Background(), profile.ID, agentprofile.RevisionInput{
		Instructions: revision.Instructions, Adapter: "generic", Command: os.Args[0],
		Args: []string{"-test.run=TestRunnerFakeAgentProcess"}, MaxInputTokens: revision.MaxInputTokens,
		ReservedOutputTokens: revision.ReservedOutputTokens, RecentMessageLimit: revision.RecentMessageLimit,
		RetrievalLimit: revision.RetrievalLimit, TimeoutSeconds: 60,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := storage.NewTaskStore(database).Create(context.Background(), task.CreateInput{
		Description: "希望搜索页面能够组合筛选状态和更新时间",
	})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := storage.NewRunStore(database).ClaimNextTask(context.Background(), 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Run.Purpose != run.PurposeTriage {
		t.Fatalf("purpose = %q", claim.Run.Purpose)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	if err := Execute(context.Background(), currentProject, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	database, err = storage.OpenExisting(context.Background(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	updated, err := storage.NewTaskStore(database).Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != task.StatusReady || updated.Title != "实现搜索组合筛选" {
		t.Fatalf("updated task = %#v", updated)
	}
	current, err := storage.NewAssessmentStore(database).GetCurrent(context.Background(), created.ID)
	if err != nil || current.SourceRunID != claim.Run.ID {
		t.Fatalf("assessment = %#v, %v", current, err)
	}
	if _, err := storage.NewWorkspaceStore(database).GetByTask(context.Background(), created.ID); !errors.Is(err, storage.ErrWorkspaceNotFound) {
		t.Fatalf("triage workspace error = %v", err)
	}
}

func TestRunnerFakeAgentProcess(t *testing.T) {
	if os.Getenv("ATS_RUN_ID") == "" {
		return
	}
	if os.Getenv("ATS_CLAIM_TOKEN") != "" {
		t.Fatal("claim token leaked to agent")
	}
	if os.Getenv("ATS_RUN_PURPOSE") == "PLANNING" {
		result := `{"reply":"需求已经足够明确，我整理了一版可审核方案。","plan":{"summary":"实现最小文本社区","rationale":"先完成发布和列表闭环","risks":"暂不包含账户体系","drafts":[{"title":"实现帖子发布与列表","description":"支持发布并查看文本帖子","acceptance_criteria":"发布后帖子出现在列表中","priority":1,"test_cases":[{"title":"发布文本帖子","description":"提交后列表展示内容","required":true}]}]}}`
		if err := os.WriteFile(".ats-run-result.json", []byte(result), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	if os.Getenv("ATS_RUN_PURPOSE") == "TRIAGE" {
		result := `{"triage":{"suggested_title":"实现搜索组合筛选","scores":{"technical_complexity":2,"requirement_uncertainty":1,"change_scope":2,"validation_burden":2,"human_dependency":1,"risk_and_reversibility":1},"confidence":0.8,"rationale":"涉及查询参数、持久化和前端筛选状态","assumptions":["现有搜索接口可以扩展"],"split_recommended":false,"split_rationale":""}}`
		if err := os.WriteFile(".ats-run-result.json", []byte(result), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	if os.Getenv("ATS_RUN_PURPOSE") == "REVIEW" {
		result := `{"reply":"当前实现仍需补充错误路径和并发边界测试。"}`
		if err := os.WriteFile(".ats-run-result.json", []byte(result), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	prompt, _ := io.ReadAll(os.Stdin)
	if strings.Contains(string(prompt), "需要人工决定迁移策略") &&
		!strings.Contains(string(prompt), "Continuation Clarification") {
		result := `{"clarification":{"category":"DECISION","question":"数据库迁移是否兼容旧版本？","options":[{"id":"compatible","label":"兼容升级","description":"保留旧数据"},{"id":"fresh","label":"仅新项目","description":"不迁移旧数据"}],"recommended_option_id":"compatible","allow_custom_answer":true}}`
		if err := os.WriteFile(".ats-run-result.json", []byte(result), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	if strings.Contains(string(prompt), "无效结果仍需收尾") {
		if err := os.WriteFile("runner-output.txt", []byte("must be preserved\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(".ats-run-result.json", []byte(`{"estimate":`), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	if strings.Contains(string(prompt), "等待取消") {
		if err := os.WriteFile("agent-started", []byte("started\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	if err := os.WriteFile("runner-output.txt", []byte("agent wrote this\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := `{"estimate":{"points":5,"remaining_points":1,"confidence":0.8,"rationale":"实现已完成，剩余人工验收"},"new_test_cases":[{"title":"Runner 输出文件","description":"文件内容正确","required":true,"outcome":"PASSED","summary":"Agent 检查通过"}]}`
	if err := os.WriteFile(".ats-run-result.json", []byte(result), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _ = os.Stdout.WriteString("fake agent completed\n")
}

func initializeRunnerRepository(t *testing.T) string {
	t.Helper()
	repoRoot := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = repoRoot
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	runGit("init", "--quiet")
	runGit("config", "user.name", "runner-test")
	runGit("config", "user.email", "runner@example.com")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("runner test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "README.md")
	runGit("commit", "--quiet", "-m", "initial")
	return repoRoot
}
