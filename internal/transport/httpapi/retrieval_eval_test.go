package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/retrievaleval"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestRetrievalEvalRoutesMaintainCasesAndRunMetrics(t *testing.T) {
	database := openHTTPTestDatabase(t)
	createdTask, err := storage.NewTaskStore(database).Create(t.Context(), task.CreateInput{Title: "检索质量评测", Description: "真实搜索路径"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterRetrievalEvalRoutes(mux, storage.NewRetrievalEvalStore(database, storage.NewSearchStore(database)))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	body := `{"query":"检索质量","kinds":["TASK"],"only_current":true,"document_id":"TASK:` + createdTask.ID + `","note":"核心结果"}`
	response, err := server.Client().Post(server.URL+"/api/retrieval-evals/cases", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", response.StatusCode)
	}
	var created retrievaleval.Case
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	casesResponse, err := server.Client().Get(server.URL + "/api/retrieval-evals/cases")
	if err != nil {
		t.Fatal(err)
	}
	defer casesResponse.Body.Close()
	var cases []retrievaleval.Case
	if err := json.NewDecoder(casesResponse.Body).Decode(&cases); err != nil || len(cases) != 1 {
		t.Fatalf("cases = %#v, err = %v", cases, err)
	}

	runResponse, err := server.Client().Post(server.URL+"/api/retrieval-evals/runs", "application/json", strings.NewReader(`{"k":10}`))
	if err != nil {
		t.Fatal(err)
	}
	defer runResponse.Body.Close()
	if runResponse.StatusCode != http.StatusCreated {
		t.Fatalf("run status = %d", runResponse.StatusCode)
	}
	var run retrievaleval.Run
	if err := json.NewDecoder(runResponse.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	if run.RecallAtK != 1 || run.MRR != 1 {
		t.Fatalf("run = %#v", run)
	}
	runsResponse, err := server.Client().Get(server.URL + "/api/retrieval-evals/runs?limit=10")
	if err != nil {
		t.Fatal(err)
	}
	defer runsResponse.Body.Close()
	var runs []retrievaleval.Run
	if err := json.NewDecoder(runsResponse.Body).Decode(&runs); err != nil || len(runs) != 1 {
		t.Fatalf("runs = %#v, err = %v", runs, err)
	}
	if len(runs[0].Results) != 0 {
		t.Fatalf("run list should not include detail results: %#v", runs[0].Results)
	}
	detailResponse, err := server.Client().Get(server.URL + "/api/retrieval-evals/runs/" + run.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer detailResponse.Body.Close()
	var detail retrievaleval.Run
	if err := json.NewDecoder(detailResponse.Body).Decode(&detail); err != nil || len(detail.Results) != 1 {
		t.Fatalf("run detail = %#v, err = %v", detail, err)
	}

	request, err := http.NewRequest(http.MethodDelete, server.URL+"/api/retrieval-evals/cases/"+created.ID+"/relevances/TASK:"+createdTask.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	deleteResponse, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	deleteResponse.Body.Close()
	if deleteResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleteResponse.StatusCode)
	}
}

func TestRetrievalEvalRoutesRejectInvalidInputAndEmptySuite(t *testing.T) {
	database := openHTTPTestDatabase(t)
	mux := http.NewServeMux()
	RegisterRetrievalEvalRoutes(mux, storage.NewRetrievalEvalStore(database, storage.NewSearchStore(database)))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	invalid, err := server.Client().Post(server.URL+"/api/retrieval-evals/cases", "application/json", strings.NewReader(`{"query":""}`))
	if err != nil {
		t.Fatal(err)
	}
	invalid.Body.Close()
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid status = %d", invalid.StatusCode)
	}
	empty, err := server.Client().Post(server.URL+"/api/retrieval-evals/runs", "application/json", strings.NewReader(`{"k":10}`))
	if err != nil {
		t.Fatal(err)
	}
	empty.Body.Close()
	if empty.StatusCode != http.StatusConflict {
		t.Fatalf("empty suite status = %d", empty.StatusCode)
	}
	badK, err := server.Client().Post(server.URL+"/api/retrieval-evals/runs", "application/json", strings.NewReader(`{"k":0}`))
	if err != nil {
		t.Fatal(err)
	}
	badK.Body.Close()
	if badK.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad K status = %d", badK.StatusCode)
	}
	badLimit, err := server.Client().Get(server.URL + "/api/retrieval-evals/runs?limit=invalid")
	if err != nil {
		t.Fatal(err)
	}
	badLimit.Body.Close()
	if badLimit.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad limit status = %d", badLimit.StatusCode)
	}
	missingRequest, err := http.NewRequest(http.MethodDelete, server.URL+"/api/retrieval-evals/cases/missing/relevances/TASK:missing", nil)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := server.Client().Do(missingRequest)
	if err != nil {
		t.Fatal(err)
	}
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing relevance status = %d", missing.StatusCode)
	}
	requestKnowledge(t, server, http.MethodPost, "/api/retrieval-evals/cases", `{`, http.StatusBadRequest)
	requestKnowledge(t, server, http.MethodPost, "/api/retrieval-evals/runs", `{`, http.StatusBadRequest)
	requestKnowledge(t, server, http.MethodGet, "/api/retrieval-evals/runs/missing", "", http.StatusNotFound)
}

func TestRetrievalEvalRoutesMapStorageFailures(t *testing.T) {
	database := openHTTPTestDatabase(t)
	mux := http.NewServeMux()
	RegisterRetrievalEvalRoutes(mux, storage.NewRetrievalEvalStore(database, storage.NewSearchStore(database)))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	requests := []struct {
		method string
		path   string
		body   string
		status int
	}{
		{method: http.MethodGet, path: "/api/retrieval-evals/cases", status: http.StatusInternalServerError},
		{method: http.MethodPost, path: "/api/retrieval-evals/cases", body: `{"query":"查询","document_id":"TASK:task"}`, status: http.StatusInternalServerError},
		{method: http.MethodDelete, path: "/api/retrieval-evals/cases/case/relevances/TASK:task", status: http.StatusInternalServerError},
		{method: http.MethodGet, path: "/api/retrieval-evals/runs", status: http.StatusInternalServerError},
		{method: http.MethodPost, path: "/api/retrieval-evals/runs", body: `{"k":10}`, status: http.StatusInternalServerError},
		{method: http.MethodGet, path: "/api/retrieval-evals/runs/run", status: http.StatusInternalServerError},
	}
	for _, item := range requests {
		requestKnowledge(t, server, item.method, item.path, item.body, item.status)
	}
}
