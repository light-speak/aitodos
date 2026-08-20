package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/plan"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestPlanRoutesCreateReviewAndApprove(t *testing.T) {
	database := openHTTPTestDatabase(t)
	mux := http.NewServeMux()
	discussions := storage.NewDiscussionStore(database)
	relations := storage.NewRelationStore(database)
	topics := storage.NewTopicStore(database)
	RegisterTopicRoutes(mux, topics, discussions, relations)
	RegisterPlanRoutes(mux, storage.NewPlanStore(database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	createdTopic := requestTopic(t, server.Client(), http.MethodPost, server.URL+"/api/topics", `{"title":"搜索能力"}`, http.StatusCreated)
	created := requestPlanView(t, server.Client(), http.MethodPost, server.URL+"/api/topics/"+createdTopic.ID+"/plans", `{
		"expected_topic_version":1,
		"summary":"实现全文搜索",
		"rationale":"先做本地索引",
		"risks":"索引过期",
		"drafts":[{
			"title":"建立搜索索引","description":"索引 Topic 与 Task",
			"acceptance_criteria":"可以搜索 Topic","priority":1,
			"test_cases":[{"title":"Topic 可检索","description":"输入关键词返回 Topic","required":true}]
		}]
	}`, http.StatusCreated)
	if created.Plan.Status != plan.StatusInReview || len(created.Revision.Drafts) != 1 {
		t.Fatalf("created = %#v", created)
	}
	response := requestJSONResponse(t, server.Client(), http.MethodPost,
		server.URL+"/api/plans/"+created.Plan.ID+"/approve", `{
			"expected_topic_version":2,"revision_id":"`+created.Revision.ID+`","comment":"批准"
		}`, http.StatusOK)
	var approved struct {
		Plan  plan.View   `json:"plan"`
		Tasks []task.Task `json:"tasks"`
	}
	if err := json.NewDecoder(response.Body).Decode(&approved); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if approved.Plan.Plan.Status != plan.StatusApproved || len(approved.Tasks) != 1 {
		t.Fatalf("approved = %#v", approved)
	}
}

func TestPlanRoutesReturnNullWithoutPlan(t *testing.T) {
	database := openHTTPTestDatabase(t)
	mux := http.NewServeMux()
	store := storage.NewPlanStore(database)
	RegisterPlanRoutes(mux, store)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	response := requestJSONResponse(t, server.Client(), http.MethodGet, server.URL+"/api/topics/missing/plans/current", "", http.StatusOK)
	var current *plan.View
	if err := json.NewDecoder(response.Body).Decode(&current); err != nil || current != nil {
		t.Fatalf("current = %#v, err = %v", current, err)
	}
	response.Body.Close()
}

func TestPlanRoutesRejectBlankChangesComment(t *testing.T) {
	database := openHTTPTestDatabase(t)
	mux := http.NewServeMux()
	discussions := storage.NewDiscussionStore(database)
	relations := storage.NewRelationStore(database)
	topics := storage.NewTopicStore(database)
	RegisterTopicRoutes(mux, topics, discussions, relations)
	RegisterPlanRoutes(mux, storage.NewPlanStore(database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	createdTopic := requestTopic(t, server.Client(), http.MethodPost, server.URL+"/api/topics", `{"title":"Plan 反馈"}`, http.StatusCreated)
	created := requestPlanView(t, server.Client(), http.MethodPost, server.URL+"/api/topics/"+createdTopic.ID+"/plans", `{
		"expected_topic_version":1,"summary":"验证反馈","drafts":[{"title":"实现验证","priority":2}]
	}`, http.StatusCreated)
	requestJSONResponse(t, server.Client(), http.MethodPost,
		server.URL+"/api/plans/"+created.Plan.ID+"/request-changes", `{
			"expected_topic_version":2,"revision_id":"`+created.Revision.ID+`","comment":"  "
		}`, http.StatusBadRequest).Body.Close()
}

func requestPlanView(t *testing.T, client *http.Client, method, url, body string, wantStatus int) plan.View {
	t.Helper()
	response := requestJSONResponse(t, client, method, url, body, wantStatus)
	defer response.Body.Close()
	var result plan.View
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func requestJSONResponse(t *testing.T, client *http.Client, method, url, body string, wantStatus int) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus {
		defer response.Body.Close()
		var payload unknownEnvelope
		_ = json.NewDecoder(response.Body).Decode(&payload)
		t.Fatalf("%s %s status = %d, want %d: %#v", method, url, response.StatusCode, wantStatus, payload)
	}
	return response
}

type unknownEnvelope map[string]any
