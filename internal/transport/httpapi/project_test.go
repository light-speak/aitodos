package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/light-speak/aitodos/internal/project"
)

func TestProjectRoutesReadAndUpdateWorkerSettings(t *testing.T) {
	currentProject := initializeHTTPTestProject(t)
	mux := http.NewServeMux()
	RegisterProjectRoutes(mux, currentProject)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	initial := requestProjectInfo(t, server.Client(), http.MethodGet, server.URL+"/api/project", "", http.StatusOK)
	if initial.WorkersEnabled || initial.MaxWorkers != 2 {
		t.Fatalf("initial project info = %#v", initial)
	}

	updated := requestProjectInfo(t, server.Client(), http.MethodPost, server.URL+"/api/project/workers", `{
		"enabled":true,
		"max_workers":4
	}`, http.StatusOK)
	if !updated.WorkersEnabled || updated.MaxWorkers != 4 {
		t.Fatalf("updated project info = %#v", updated)
	}

	loaded := requestProjectInfo(t, server.Client(), http.MethodGet, server.URL+"/api/project", "", http.StatusOK)
	if !loaded.WorkersEnabled || loaded.MaxWorkers != 4 {
		t.Fatalf("loaded project info = %#v", loaded)
	}
}

func TestProjectRoutesRejectInvalidWorkerSettings(t *testing.T) {
	currentProject := initializeHTTPTestProject(t)
	mux := http.NewServeMux()
	RegisterProjectRoutes(mux, currentProject)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	requestProjectInfo(t, server.Client(), http.MethodPost, server.URL+"/api/project/workers", `{
		"enabled":true,
		"max_workers":0
	}`, http.StatusBadRequest)
	loaded := requestProjectInfo(t, server.Client(), http.MethodGet, server.URL+"/api/project", "", http.StatusOK)
	if loaded.WorkersEnabled || loaded.MaxWorkers != 2 {
		t.Fatalf("invalid update changed project info = %#v", loaded)
	}
}

type projectInfoResponse struct {
	Name           string `json:"name"`
	Root           string `json:"root"`
	Agent          string `json:"agent"`
	WorkersEnabled bool   `json:"workers_enabled"`
	MaxWorkers     int    `json:"max_workers"`
}

func requestProjectInfo(
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	body string,
	wantStatus int,
) projectInfoResponse {
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
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d", method, url, response.StatusCode, wantStatus)
	}
	if wantStatus >= 400 {
		return projectInfoResponse{}
	}
	var result projectInfoResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func initializeHTTPTestProject(t *testing.T) *project.Project {
	t.Helper()
	repoRoot := t.TempDir()
	if output, err := exec.Command("git", "init", "--quiet", repoRoot).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	initialized, _, err := project.Initialize(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	return initialized
}
