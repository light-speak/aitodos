package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/experience"
	"github.com/light-speak/aitodos/internal/domain/knowledge"
	domainrun "github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/topic"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestKnowledgeRoutesManageTaskDecisionsLabelsAndCI(t *testing.T) {
	database := openHTTPTestDatabase(t)
	createdTask, err := storage.NewTaskStore(database).Create(t.Context(), task.CreateInput{Title: "发布任务"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterKnowledgeRoutes(mux, storage.NewKnowledgeStore(database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	decisionResponse := requestKnowledge(t, server, http.MethodPost, "/api/tasks/"+createdTask.ID+"/decisions",
		`{"title":"使用 PR","content":"公共仓库只通过 PR 合并"}`, http.StatusCreated)
	var decision knowledge.Decision
	decodeKnowledge(t, decisionResponse, &decision)
	if decision.TaskID != createdTask.ID || decision.Key == "" {
		t.Fatalf("decision = %#v", decision)
	}
	listResponse := requestKnowledge(t, server, http.MethodGet, "/api/tasks/"+createdTask.ID+"/decisions", "", http.StatusOK)
	var decisions []knowledge.Decision
	decodeKnowledge(t, listResponse, &decisions)
	if len(decisions) != 1 || decisions[0].ID != decision.ID {
		t.Fatalf("decisions = %#v", decisions)
	}

	labelResponse := requestKnowledge(t, server, http.MethodPost, "/api/labels", `{"name":"release","color":"#2563EB"}`, http.StatusCreated)
	var label knowledge.Label
	decodeKnowledge(t, labelResponse, &label)
	requestKnowledge(t, server, http.MethodPost, "/api/tasks/"+createdTask.ID+"/labels/"+label.ID, "", http.StatusNoContent)
	labelsResponse := requestKnowledge(t, server, http.MethodGet, "/api/tasks/"+createdTask.ID+"/labels", "", http.StatusOK)
	var labels []knowledge.Label
	decodeKnowledge(t, labelsResponse, &labels)
	if len(labels) != 1 || labels[0].ID != label.ID {
		t.Fatalf("labels = %#v", labels)
	}
	requestKnowledge(t, server, http.MethodDelete, "/api/tasks/"+createdTask.ID+"/labels/"+label.ID, "", http.StatusNoContent)

	ciResponse := requestKnowledge(t, server, http.MethodPost, "/api/tasks/"+createdTask.ID+"/ci-snapshots",
		`{"provider":"github","commit_sha":"abcdef1234","state":"passed","checks":[{"name":"CI / go","state":"passed"}]}`, http.StatusCreated)
	var snapshot knowledge.CICheckSnapshot
	decodeKnowledge(t, ciResponse, &snapshot)
	if snapshot.State != "PASSED" || snapshot.TaskID != createdTask.ID {
		t.Fatalf("CI snapshot = %#v", snapshot)
	}
	ciListResponse := requestKnowledge(t, server, http.MethodGet, "/api/tasks/"+createdTask.ID+"/ci-snapshots", "", http.StatusOK)
	var snapshots []knowledge.CICheckSnapshot
	decodeKnowledge(t, ciListResponse, &snapshots)
	if len(snapshots) != 1 || snapshots[0].ID != snapshot.ID {
		t.Fatalf("CI snapshots = %#v", snapshots)
	}
}

func TestKnowledgeRoutesRejectInvalidInputAndMissingSubject(t *testing.T) {
	database := openHTTPTestDatabase(t)
	mux := http.NewServeMux()
	RegisterKnowledgeRoutes(mux, storage.NewKnowledgeStore(database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	requestKnowledge(t, server, http.MethodPost, "/api/labels", `{"name":"bad","color":"red"}`, http.StatusBadRequest)
	requestKnowledge(t, server, http.MethodPost, "/api/tasks/missing/decisions", `{"title":"决策","content":"内容"}`, http.StatusNotFound)
}

func TestExperienceRoutesCreateListPinAndChallenge(t *testing.T) {
	database := openHTTPTestDatabase(t)
	createdTask, err := storage.NewTaskStore(database).Create(t.Context(), task.CreateInput{Title: "经验任务"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterKnowledgeRoutes(mux, storage.NewKnowledgeStore(database))
	RegisterExperienceRoutes(mux, storage.NewExperienceStore(database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	response := requestKnowledge(t, server, http.MethodPost, "/api/tasks/"+createdTask.ID+"/experiences",
		`{"title":"先跑回归","summary":"改动状态机前先跑表驱动测试","guidance":"执行 go test","applicability":"状态迁移改动","project_wide":true,"source_run_id":"forged"}`, http.StatusCreated)
	var created experience.Record
	decodeKnowledge(t, response, &created)
	if created.Status != experience.StatusActive || created.VerificationCount != 1 || created.SourceRunID != "" {
		t.Fatalf("created experience = %#v", created)
	}
	requestKnowledge(t, server, http.MethodPost, "/api/experiences/"+created.ID+"/pin", `{"pinned":true}`, http.StatusOK)
	response = requestKnowledge(t, server, http.MethodGet, "/api/tasks/"+createdTask.ID+"/experiences?include_inactive=true", "", http.StatusOK)
	var items []experience.Record
	decodeKnowledge(t, response, &items)
	if len(items) != 1 || !items[0].Pinned {
		t.Fatalf("experiences = %#v", items)
	}
	now := time.Now().UTC()
	if _, err := database.ExecContext(t.Context(), `INSERT INTO runs(
id, purpose, task_id, status, profile_revision_id, claim_token_hash, lease_generation,
lease_expires_at, run_nonce, queued_at, claimed_at, created_at, updated_at, subject_version
) VALUES ('run-http-experience', 'IMPLEMENTATION', ?, 'RUNNING', 'profile-implementer-r1', 'hash', 1, ?, 'nonce', ?, ?, ?, ?, 1)`,
		createdTask.ID, now, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	candidate, err := storage.NewExperienceStore(database).CreateCandidate(t.Context(), experience.Input{
		TaskID: createdTask.ID, SourceRunID: "run-http-experience", Title: "保留测试命令",
		Summary: "只信任结构化命令事件", Guidance: "对比命令和退出码", Applicability: "记录 Agent 测试结果时",
	})
	if err != nil {
		t.Fatal(err)
	}
	response = requestKnowledge(t, server, http.MethodPost, "/api/experiences/"+candidate.ID+"/confirm", "", http.StatusOK)
	var confirmed experience.Record
	decodeKnowledge(t, response, &confirmed)
	if confirmed.Status != experience.StatusActive || confirmed.VerificationCount != 1 {
		t.Fatalf("confirmed experience = %#v", confirmed)
	}
	recalled, err := storage.NewExperienceStore(database).Recall(t.Context(), storage.RecallQuery{
		RunID: "run-http-experience", Purpose: domainrun.PurposeImplementation, TaskID: createdTask.ID,
		Text: "先跑回归", Limit: 5,
	})
	if err != nil || len(recalled) != 2 {
		t.Fatalf("recalls = %#v, %v", recalled, err)
	}
	response = requestKnowledge(t, server, http.MethodGet, "/api/runs/run-http-experience/experiences", "", http.StatusOK)
	var recallItems []storage.RecalledExperience
	decodeKnowledge(t, response, &recallItems)
	if len(recallItems) != 2 || recallItems[0].RecallID != recalled[0].RecallID {
		t.Fatalf("run recalls = %#v", recallItems)
	}
	requestKnowledge(t, server, http.MethodPost, "/api/experience-recalls/"+recalled[0].RecallID+"/outcome", `{"outcome":"HELPFUL"}`, http.StatusNoContent)
	requestKnowledge(t, server, http.MethodPost, "/api/experiences/"+created.ID+"/challenge", "", http.StatusOK)
	response = requestKnowledge(t, server, http.MethodGet, "/api/experiences/"+created.ID, "", http.StatusOK)
	decodeKnowledge(t, response, &created)
	if created.Status != experience.StatusChallenged {
		t.Fatalf("challenged experience = %#v", created)
	}
}

func TestExperienceRoutesManageTopicExperienceAndMapErrors(t *testing.T) {
	database := openHTTPTestDatabase(t)
	createdTopic, err := storage.NewTopicStore(database).Create(t.Context(), topic.CreateInput{Title: "经验主题"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterExperienceRoutes(mux, storage.NewExperienceStore(database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	requestKnowledge(t, server, http.MethodPost, "/api/topics/"+createdTopic.ID+"/experiences",
		`{"title":"先澄清","summary":"关键歧义先提问","guidance":"只提一个问题","applicability":"需求不明确"}`, http.StatusCreated)
	response := requestKnowledge(t, server, http.MethodGet, "/api/topics/"+createdTopic.ID+"/experiences", "", http.StatusOK)
	var items []experience.Record
	decodeKnowledge(t, response, &items)
	if len(items) != 1 {
		t.Fatalf("topic experiences = %#v", items)
	}
	requestKnowledge(t, server, http.MethodGet, "/api/experiences/missing", "", http.StatusNotFound)
	requestKnowledge(t, server, http.MethodPost, "/api/experience-recalls/missing/outcome", `{"outcome":"HELPFUL"}`, http.StatusNotFound)
}

func TestKnowledgeRoutesManageTopicKnowledgeAndReadSummaries(t *testing.T) {
	database := openHTTPTestDatabase(t)
	createdTopic, err := storage.NewTopicStore(database).Create(t.Context(), topic.CreateInput{Title: "部署约束"})
	if err != nil {
		t.Fatal(err)
	}
	createdTask, err := storage.NewTaskStore(database).Create(t.Context(), task.CreateInput{Title: "部署任务"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	store := storage.NewKnowledgeStore(database)
	RegisterKnowledgeRoutes(mux, store)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	response := requestKnowledge(t, server, http.MethodPost, "/api/topics/"+createdTopic.ID+"/decisions", `{"title":"仅本地运行","content":"只监听 loopback"}`, http.StatusCreated)
	var decision knowledge.Decision
	decodeKnowledge(t, response, &decision)
	response = requestKnowledge(t, server, http.MethodGet, "/api/topics/"+createdTopic.ID+"/decisions?include_superseded=true", "", http.StatusOK)
	var decisions []knowledge.Decision
	decodeKnowledge(t, response, &decisions)
	if len(decisions) != 1 || decisions[0].ID != decision.ID {
		t.Fatalf("topic decisions = %#v", decisions)
	}

	response = requestKnowledge(t, server, http.MethodPost, "/api/labels", `{"name":"security"}`, http.StatusCreated)
	var label knowledge.Label
	decodeKnowledge(t, response, &label)
	requestKnowledge(t, server, http.MethodPost, "/api/topics/"+createdTopic.ID+"/labels/"+label.ID, "", http.StatusNoContent)
	response = requestKnowledge(t, server, http.MethodGet, "/api/topics/"+createdTopic.ID+"/labels", "", http.StatusOK)
	var labels []knowledge.Label
	decodeKnowledge(t, response, &labels)
	requestKnowledge(t, server, http.MethodDelete, "/api/topics/"+createdTopic.ID+"/labels/"+label.ID, "", http.StatusNoContent)
	response = requestKnowledge(t, server, http.MethodGet, "/api/labels", "", http.StatusOK)
	decodeKnowledge(t, response, &labels)
	if len(labels) != 1 {
		t.Fatalf("labels = %#v", labels)
	}

	if _, err := database.ExecContext(t.Context(), `INSERT INTO runs(
id, purpose, task_id, status, profile_revision_id, claim_token_hash, lease_generation,
lease_expires_at, run_nonce, queued_at, claimed_at, created_at, updated_at, subject_version
) VALUES ('run-http-summary', 'IMPLEMENTATION', ?, 'SUCCEEDED', 'profile-implementer-r1', 'hash', 1,
?, 'nonce', ?, ?, ?, ?, 1)`, createdTask.ID, time.Now().UTC(), time.Now().UTC(), time.Now().UTC(), time.Now().UTC(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRunSummary(t.Context(), knowledge.RunSummary{RunID: "run-http-summary", Status: "SUCCEEDED", Summary: "完成部署", PassedTests: 2}); err != nil {
		t.Fatal(err)
	}
	response = requestKnowledge(t, server, http.MethodGet, "/api/runs/run-http-summary/summary", "", http.StatusOK)
	var summary knowledge.RunSummary
	decodeKnowledge(t, response, &summary)
	if summary.PassedTests != 2 {
		t.Fatalf("summary = %#v", summary)
	}
	response = requestKnowledge(t, server, http.MethodGet, "/api/tasks/"+createdTask.ID+"/ci-snapshots?limit=10", "", http.StatusOK)
	var snapshots []knowledge.CICheckSnapshot
	decodeKnowledge(t, response, &snapshots)
	if len(snapshots) != 0 {
		t.Fatalf("snapshots = %#v", snapshots)
	}
}

func requestKnowledge(t *testing.T, server *httptest.Server, method, path, body string, wantStatus int) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, server.URL+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus {
		defer response.Body.Close()
		var failure errorEnvelope
		_ = json.NewDecoder(response.Body).Decode(&failure)
		t.Fatalf("%s %s status = %d, want %d: %#v", method, path, response.StatusCode, wantStatus, failure)
	}
	if wantStatus == http.StatusNoContent {
		response.Body.Close()
	}
	return response
}

func decodeKnowledge(t *testing.T, response *http.Response, destination any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		t.Fatal(err)
	}
}
