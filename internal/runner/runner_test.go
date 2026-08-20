package runner

import (
	"bytes"
	"context"
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
	"github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
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

func TestInvocationArgsExpandsManagedResultFile(t *testing.T) {
	revision := agentprofile.Revision{Args: []string{"--output-last-message", "{result_file}", "-"}}
	args, usesPromptFile := invocationArgs(revision, "/runtime/run-1", "run-1", "/artifacts/prompt.md")
	if usesPromptFile || len(args) != 3 || args[1] != "/runtime/run-1/.ats-run-result.json" {
		t.Fatalf("invocation args = %#v, prompt file = %v", args, usesPromptFile)
	}
}

func TestInvocationArgsOmitsEmptyOptionalModel(t *testing.T) {
	revision := agentprofile.Revision{Args: []string{"exec", "--model", "{model}", "-"}}
	args, _ := invocationArgs(revision, "/runtime/run-1", "run-1", "/artifacts/prompt.md")
	if strings.Join(args, " ") != "exec -" {
		t.Fatalf("invocation args = %#v", args)
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
	if os.Getenv("ATS_RUN_PURPOSE") == "TRIAGE" {
		result := `{"triage":{"suggested_title":"实现搜索组合筛选","scores":{"technical_complexity":2,"requirement_uncertainty":1,"change_scope":2,"validation_burden":2,"human_dependency":1,"risk_and_reversibility":1},"confidence":0.8,"rationale":"涉及查询参数、持久化和前端筛选状态","assumptions":["现有搜索接口可以扩展"],"split_recommended":false,"split_rationale":""}}`
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
