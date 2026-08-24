package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/approvalrequest"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestApprovalRoutesListAndResolveHumanDecision(t *testing.T) {
	database := openHTTPTestDatabase(t)
	profiles := storage.NewAgentProfileStore(database)
	profile, err := profiles.GetByRole(t.Context(), agentprofile.RoleImplementer)
	if err != nil {
		t.Fatal(err)
	}
	revision := profile.CurrentRevision
	if _, err := profiles.CreateRevision(t.Context(), profile.ID, agentprofile.RevisionInput{
		Instructions: revision.Instructions, Adapter: "generic", Command: "fake-agent",
		MaxInputTokens: revision.MaxInputTokens, ReservedOutputTokens: revision.ReservedOutputTokens,
		RecentMessageLimit: revision.RecentMessageLimit, RetrievalLimit: revision.RetrievalLimit,
		TimeoutSeconds: revision.TimeoutSeconds,
	}); err != nil {
		t.Fatal(err)
	}
	createdTask, err := storage.NewTaskStore(database).Create(t.Context(), task.CreateInput{Title: "需要网络权限"})
	if err != nil {
		t.Fatal(err)
	}
	runs := storage.NewRunStore(database)
	claim, err := runs.ClaimNextTask(t.Context(), 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runs.Start(t.Context(), claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := runs.MarkRunning(t.Context(), claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	created, err := runs.CreateApprovalRequest(t.Context(), claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, approvalrequest.CreateInput{
		ExternalRequestID: "rpc-1", Kind: approvalrequest.KindNetwork,
		Reason: "下载测试 fixture", Host: "example.com", Protocol: "https",
		Available: []approvalrequest.Decision{approvalrequest.DecisionAcceptOnce, approvalrequest.DecisionDecline},
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	RegisterApprovalRoutes(mux, runs)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	response, err := server.Client().Get(server.URL + "/api/approvals")
	if err != nil {
		t.Fatal(err)
	}
	var open []approvalrequest.Request
	if err := json.NewDecoder(response.Body).Decode(&open); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(open) != 1 || open[0].TaskID != createdTask.ID || open[0].Host != "example.com" {
		t.Fatalf("open approvals = %#v", open)
	}

	body, err := json.Marshal(approvalDecisionInput{
		Decision: approvalrequest.DecisionAcceptOnce, ExpectedVersion: created.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err = server.Client().Post(
		server.URL+"/api/approvals/"+created.ID+"/decision", "application/json", bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("decision status = %d", response.StatusCode)
	}
	var resolved approvalrequest.Request
	if err := json.NewDecoder(response.Body).Decode(&resolved); err != nil {
		t.Fatal(err)
	}
	if resolved.Decision != approvalrequest.DecisionAcceptOnce || resolved.Status != approvalrequest.StatusResolved {
		t.Fatalf("resolved approval = %#v", resolved)
	}
}
