package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/quality"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestQualityRoutesCreateEvidenceAndReadProgress(t *testing.T) {
	database := openHTTPTestDatabase(t)
	taskStore := storage.NewTaskStore(database)
	created, err := taskStore.Create(t.Context(), task.CreateInput{Title: "进度页面"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterQualityRoutes(mux, storage.NewQualityStore(database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	requestQuality(t, server.Client(), http.MethodPost, server.URL+"/api/tasks/"+created.ID+"/estimates", `{
		"points":5,"remaining_points":3,"confidence":0.8,"rationale":"页面已完成"
	}`, http.StatusCreated)
	testCase := requestTestCase(t, server.Client(), http.MethodPost, server.URL+"/api/tasks/"+created.ID+"/test-cases", `{
		"title":"浏览器流程","description":"打开进度页","required":true,"sort_order":0
	}`, http.StatusCreated)
	createdResult := requestTestResult(t, server.Client(), http.MethodPost,
		server.URL+"/api/tasks/"+created.ID+"/test-cases/"+testCase.ID+"/results", `{
			"outcome":"PASSED","summary":"E2E 通过"
		}`, http.StatusCreated)
	if createdResult.EvidenceKind != quality.EvidenceHuman || createdResult.Command != "" ||
		createdResult.ArtifactRef != "" || createdResult.SourceRunID != "" || createdResult.ExitCode != nil {
		t.Fatalf("HTTP evidence must be human-owned: %#v", createdResult)
	}

	response, err := server.Client().Get(server.URL + "/api/tasks/" + created.ID + "/quality")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var taskQuality quality.TaskQuality
	if err := json.NewDecoder(response.Body).Decode(&taskQuality); err != nil {
		t.Fatal(err)
	}
	if taskQuality.Estimate == nil || len(taskQuality.TestCases) != 1 || taskQuality.TestCases[0].LatestResult == nil {
		t.Fatalf("quality = %#v", taskQuality)
	}

	progressResponse, err := server.Client().Get(server.URL + "/api/progress")
	if err != nil {
		t.Fatal(err)
	}
	defer progressResponse.Body.Close()
	var progress quality.ProjectProgress
	if err := json.NewDecoder(progressResponse.Body).Decode(&progress); err != nil {
		t.Fatal(err)
	}
	if progress.EstimatedTasks != 1 || progress.RequiredTests != 1 || progress.VerifiedPassedTests != 1 {
		t.Fatalf("progress = %#v", progress)
	}
}

func TestQualityRoutesRejectInvalidAndAgentOnlyEvidence(t *testing.T) {
	database := openHTTPTestDatabase(t)
	created, err := storage.NewTaskStore(database).Create(t.Context(), task.CreateInput{Title: "校验测试证据"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterQualityRoutes(mux, storage.NewQualityStore(database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	requestQuality(t, server.Client(), http.MethodPost, server.URL+"/api/tasks/"+created.ID+"/estimates", `{`, http.StatusBadRequest)
	requestQuality(t, server.Client(), http.MethodPost, server.URL+"/api/tasks/"+created.ID+"/estimates", `{"points":0}`, http.StatusBadRequest)
	requestQuality(t, server.Client(), http.MethodPost, server.URL+"/api/tasks/"+created.ID+"/test-cases", `{"title":""}`, http.StatusBadRequest)
	testCase := requestTestCase(t, server.Client(), http.MethodPost, server.URL+"/api/tasks/"+created.ID+"/test-cases", `{"title":"自动验证","required":true}`, http.StatusCreated)
	requestQuality(t, server.Client(), http.MethodPost,
		server.URL+"/api/tasks/"+created.ID+"/test-cases/"+testCase.ID+"/results",
		`{"outcome":"PASSED","evidence_kind":"AGENT_REPORT","summary":"声称通过"}`, http.StatusBadRequest)
	requestQuality(t, server.Client(), http.MethodPost,
		server.URL+"/api/tasks/"+created.ID+"/test-cases/"+testCase.ID+"/results",
		`{"outcome":"PASSED","evidence_kind":"COMMAND","summary":"伪造命令","command":"go test ./...","exit_code":0}`, http.StatusBadRequest)
	requestQuality(t, server.Client(), http.MethodPost,
		server.URL+"/api/tasks/"+created.ID+"/test-cases/"+testCase.ID+"/results", `{`, http.StatusBadRequest)

	response, err := server.Client().Get(server.URL + "/api/tasks/missing/quality")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("missing quality status = %d", response.StatusCode)
	}
}

func requestQuality(t *testing.T, client *http.Client, method string, url string, body string, wantStatus int) {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d", method, url, response.StatusCode, wantStatus)
	}
}

func requestTestCase(t *testing.T, client *http.Client, method string, url string, body string, wantStatus int) quality.TestCase {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d", method, url, response.StatusCode, wantStatus)
	}
	var result quality.TestCase
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func requestTestResult(t *testing.T, client *http.Client, method string, url string, body string, wantStatus int) quality.TestResult {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d", method, url, response.StatusCode, wantStatus)
	}
	var result quality.TestResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
