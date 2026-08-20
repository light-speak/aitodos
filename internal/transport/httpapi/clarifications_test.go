package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/clarification"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestClarificationRoutesListAndAnswer(t *testing.T) {
	database := openHTTPTestDatabase(t)
	profileStore := storage.NewAgentProfileStore(database)
	profile, err := profileStore.GetByRole(t.Context(), agentprofile.RoleImplementer)
	if err != nil {
		t.Fatal(err)
	}
	revision := profile.CurrentRevision
	if _, err := profileStore.CreateRevision(t.Context(), profile.ID, agentprofile.RevisionInput{
		Instructions: revision.Instructions, Adapter: "generic", Command: "fake-agent",
		MaxInputTokens: revision.MaxInputTokens, ReservedOutputTokens: revision.ReservedOutputTokens,
		RecentMessageLimit: revision.RecentMessageLimit, RetrievalLimit: revision.RetrievalLimit,
		TimeoutSeconds: revision.TimeoutSeconds,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := storage.NewTaskStore(database).Create(t.Context(), task.CreateInput{Title: "等待选择迁移策略"})
	if err != nil {
		t.Fatal(err)
	}
	runs := storage.NewRunStore(database)
	claim, err := runs.ClaimNextTask(t.Context(), 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runs.Start(t.Context(), claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := runs.MarkRunning(t.Context(), claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	_, question, err := runs.FinishNeedsInput(t.Context(), claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, clarification.Request{
		Category: clarification.CategoryDecision, Question: "是否兼容旧数据库？",
		Options:             []clarification.Option{{ID: "yes", Label: "兼容", Description: "迁移旧数据"}, {ID: "no", Label: "不兼容", Description: "只支持新项目"}},
		RecommendedOptionID: "yes", AllowCustomAnswer: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	RegisterClarificationRoutes(mux, storage.NewClarificationStore(database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	open := requestClarifications(t, server.Client(), server.URL+"/api/clarifications", http.StatusOK)
	if len(open) != 1 || open[0].TaskID != created.ID {
		t.Fatalf("open clarifications = %#v", open)
	}
	history := requestClarifications(t, server.Client(), server.URL+"/api/tasks/"+created.ID+"/clarifications", http.StatusOK)
	if len(history) != 1 || history[0].ID != question.ID {
		t.Fatalf("task clarifications = %#v", history)
	}

	body, err := json.Marshal(clarification.AnswerInput{SelectedOptionID: "yes", ExpectedVersion: question.Version})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/clarifications/"+question.ID+"/answer", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("answer status = %d", response.StatusCode)
	}
	var answered clarificationAnswerResponse
	if err := json.NewDecoder(response.Body).Decode(&answered); err != nil {
		t.Fatal(err)
	}
	if answered.Clarification.Status != clarification.StatusAnswered || answered.Task.Status != task.StatusReady {
		t.Fatalf("answer = %#v", answered)
	}
	if open = requestClarifications(t, server.Client(), server.URL+"/api/clarifications", http.StatusOK); len(open) != 0 {
		t.Fatalf("remaining open = %#v", open)
	}
}

func requestClarifications(t *testing.T, client *http.Client, url string, wantStatus int) []clarification.Clarification {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		t.Fatalf("GET %s status = %d, want %d", url, response.StatusCode, wantStatus)
	}
	var result []clarification.Clarification
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
