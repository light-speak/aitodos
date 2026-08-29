package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/approvalrequest"
	"github.com/light-speak/aitodos/internal/domain/quality"
	"github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestExecuteCodexAppServerBridgesApprovalAndPersistsSession(t *testing.T) {
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
		Instructions: revision.Instructions, Adapter: "codex-app-server", Command: fakeCodexAppServerCommand(t),
		MaxInputTokens: revision.MaxInputTokens, ReservedOutputTokens: revision.ReservedOutputTokens,
		RecentMessageLimit: revision.RecentMessageLimit, RetrievalLimit: revision.RetrievalLimit,
		TimeoutSeconds: 60,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := storage.NewTaskStore(database).Create(context.Background(), task.CreateInput{
		Title: "执行需要批准的命令", AcceptanceCriteria: "批准后完成并保存 Session",
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
	request := waitForApprovalRequest(t, store)
	if request.Kind != approvalrequest.KindCommand || request.Command != "git status --short" {
		t.Fatalf("approval request = %#v", request)
	}
	if _, err := store.ResolveApprovalRequest(context.Background(), request.ID, request.Version, approvalrequest.DecisionAcceptOnce); err != nil {
		t.Fatal(err)
	}
	select {
	case executeErr := <-done:
		if executeErr != nil {
			t.Fatal(executeErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Codex App Server Run did not finish")
	}
	currentRun, err := store.Get(context.Background(), claim.Run.ID)
	if err != nil || currentRun.Status != run.StatusSucceeded || currentRun.AgentSessionID == "" {
		t.Fatalf("run = %#v, %v", currentRun, err)
	}
	session, err := store.GetAgentSessionForRun(context.Background(), currentRun.ID)
	if err != nil || session.ExternalSessionID != "thread-aitodos-1" {
		t.Fatalf("session = %#v, %v", session, err)
	}
	updated, err := storage.NewTaskStore(database).Get(context.Background(), created.ID)
	if err != nil || updated.Status != task.StatusReview {
		t.Fatalf("task = %#v, %v", updated, err)
	}
	qualityData, err := storage.NewQualityStore(database).GetTaskQuality(context.Background(), created.ID)
	if err != nil || len(qualityData.TestCases) != 1 || qualityData.TestCases[0].LatestResult == nil ||
		qualityData.TestCases[0].LatestResult.EvidenceKind != quality.EvidenceCommand ||
		qualityData.TestCases[0].LatestResult.Command != "git status --short" {
		t.Fatalf("command evidence = %#v, %v", qualityData, err)
	}
}

func TestMapCodexApprovalRequestPreservesPermissionGrant(t *testing.T) {
	params := json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","cwd":"/work","reason":"需要联网","permissions":{"network":{"enabled":true}},"startedAtMs":1}`)
	mapped, grant, err := mapCodexApprovalRequest("item/permissions/requestApproval", json.RawMessage(`7`), params)
	if err != nil {
		t.Fatal(err)
	}
	if mapped.Kind != approvalrequest.KindPermissions || mapped.CWD != "/work" || string(grant) != `{"network":{"enabled":true}}` {
		t.Fatalf("mapped = %#v, grant = %s", mapped, grant)
	}
	response, interrupt, err := codexApprovalResponse("item/permissions/requestApproval", approvalrequest.DecisionAcceptSession, grant)
	if err != nil || interrupt || !strings.Contains(string(response), `"scope":"session"`) ||
		!strings.Contains(string(response), `"network":{"enabled":true}`) {
		t.Fatalf("response = %s, interrupt = %v, err = %v", response, interrupt, err)
	}
}

func waitForApprovalRequest(t *testing.T, store *storage.RunStore) approvalrequest.Request {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		items, err := store.ListOpenApprovalRequests(context.Background())
		if err == nil && len(items) == 1 {
			return items[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("approval request was not created")
	return approvalrequest.Request{}
}

func fakeCodexAppServerCommand(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-codex")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"mcp\" ]; then printf '[]\\n'; exit 0; fi\nexec %q -test.run=TestRunnerFakeCodexAppServer\n", os.Args[0])
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunnerFakeCodexAppServer(t *testing.T) {
	if os.Getenv("ATS_RUN_ID") == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			t.Fatal(err)
		}
		switch message.Method {
		case "initialize":
			writeFakeRPC(t, `{"id":1,"result":{"userAgent":"fake","platformFamily":"unix","platformOs":"test","codexHome":"/fake"}}`)
		case "thread/start", "thread/resume":
			writeFakeRPC(t, `{"id":2,"result":{"thread":{"id":"thread-aitodos-1"}}}`)
		case "turn/start":
			writeFakeRPC(t, `{"id":3,"result":{"turn":{"id":"turn-aitodos-1","status":"inProgress","items":[]}}}`)
			writeFakeRPC(t, `{"id":"approval-1","method":"item/commandExecution/requestApproval","params":{"threadId":"thread-aitodos-1","turnId":"turn-aitodos-1","itemId":"item-1","command":"git status --short","cwd":"/workspace","reason":"检查工作区","startedAtMs":1}}`)
		case "":
			if string(message.ID) != `"approval-1"` || !strings.Contains(string(message.Result), `"decision":"accept"`) {
				t.Fatalf("approval response = %s", scanner.Bytes())
			}
			result := `{"estimate":{"points":1,"remaining_points":0,"confidence":1,"rationale":"完成"},"new_test_cases":[{"title":"工作区状态检查","description":"命令可以执行","required":true,"outcome":"PASSED","summary":"命令退出码为零","command":"git status --short"}]}`
			if err := os.WriteFile(".ats-run-result.json", []byte(result), 0o600); err != nil {
				t.Fatal(err)
			}
			writeFakeRPC(t, `{"method":"item/completed","params":{"threadId":"thread-aitodos-1","turnId":"turn-aitodos-1","completedAtMs":2,"item":{"id":"item-1","type":"commandExecution","command":"git status --short","commandActions":[],"cwd":"/workspace","status":"completed","aggregatedOutput":"","exitCode":0}}}`)
			writeFakeRPC(t, `{"method":"turn/completed","params":{"threadId":"thread-aitodos-1","turn":{"id":"turn-aitodos-1","status":"completed","items":[]}}}`)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}

func writeFakeRPC(t *testing.T, value string) {
	t.Helper()
	if _, err := fmt.Fprintln(os.Stdout, value); err != nil {
		t.Fatal(err)
	}
}
