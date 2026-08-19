package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/release"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/workspace"
	"github.com/light-speak/aitodos/internal/gitworkflow"
	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestGitRoutesCreateWorkspaceAndRelease(t *testing.T) {
	server, database := newGitTestServer(t)
	taskStore := storage.NewTaskStore(database)
	createdTask, err := taskStore.Create(context.Background(), task.CreateInput{Title: "发布面板"})
	if err != nil {
		t.Fatal(err)
	}

	workspaceResponse := requestJSON(t, server.Client(), http.MethodGet,
		server.URL+"/api/tasks/"+createdTask.ID+"/workspace", "", http.StatusOK)
	if string(workspaceResponse) != "null\n" {
		t.Fatalf("workspace before creation = %q", workspaceResponse)
	}
	workspaceBody := requestJSON(t, server.Client(), http.MethodPost,
		server.URL+"/api/tasks/"+createdTask.ID+"/workspace", "", http.StatusOK)
	var createdWorkspace workspace.Workspace
	if err := json.Unmarshal(workspaceBody, &createdWorkspace); err != nil {
		t.Fatal(err)
	}
	if createdWorkspace.TaskID != createdTask.ID || createdWorkspace.State != workspace.StateReady {
		t.Fatalf("workspace = %#v", createdWorkspace)
	}

	infoBody := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/git", "", http.StatusOK)
	var info gitworkflow.RepositoryInfo
	if err := json.Unmarshal(infoBody, &info); err != nil {
		t.Fatal(err)
	}
	if info.CurrentBranch != "main" || info.HeadSHA == "" || len(info.Branches) != 2 {
		t.Fatalf("repository info = %#v", info)
	}

	releaseBody := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/releases",
		`{"version":"v1.0.1","source_branch":"main"}`, http.StatusCreated)
	var createdRelease release.Release
	if err := json.Unmarshal(releaseBody, &createdRelease); err != nil {
		t.Fatal(err)
	}
	if createdRelease.Status != release.StatusTagged || createdRelease.TagName != "v1.0.1" {
		t.Fatalf("release = %#v", createdRelease)
	}

	listBody := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/releases", "", http.StatusOK)
	var releases []release.Release
	if err := json.Unmarshal(listBody, &releases); err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 || releases[0].ID != createdRelease.ID {
		t.Fatalf("releases = %#v", releases)
	}
}

func TestGitRoutesValidateAndReportMissingTask(t *testing.T) {
	server, _ := newGitTestServer(t)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/releases",
		`{"version":"one","source_branch":"main"}`, http.StatusBadRequest)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/tasks/missing/workspace", "", http.StatusNotFound)
}

func newGitTestServer(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()
	temporaryRoot, err := filepath.Abs(filepath.Join("..", "..", ".tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(temporaryRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	repositoryRoot, err := os.MkdirTemp(temporaryRoot, "http-git-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repositoryRoot) })
	runHTTPGit(t, repositoryRoot, "init", "--quiet", "--initial-branch=main")
	runHTTPGit(t, repositoryRoot, "config", "user.name", "AiTodos Test")
	runHTTPGit(t, repositoryRoot, "config", "user.email", "aitodos@example.invalid")
	if err := os.WriteFile(filepath.Join(repositoryRoot, "README.md"), []byte("# test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runHTTPGit(t, repositoryRoot, "add", "README.md")
	runHTTPGit(t, repositoryRoot, "commit", "--quiet", "-m", "initial")
	currentProject, _, err := project.Initialize(context.Background(), repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	database, err := storage.OpenExisting(context.Background(), currentProject.Paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	mux := http.NewServeMux()
	RegisterGitWorkflowRoutes(mux, gitworkflow.New(currentProject, database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, database
}

func requestJSON(t *testing.T, client *http.Client, method, url, body string, wantStatus int) []byte {
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
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(response.Body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d: %s", method, url, response.StatusCode, wantStatus, buffer.String())
	}
	return buffer.Bytes()
}

func runHTTPGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", directory}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}
