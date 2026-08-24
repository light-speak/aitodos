package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	if info.CurrentBranch != "main" || info.HeadSHA == "" || len(info.Branches) != 3 {
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

func TestGitRoutesUpdateTaskTargetBranchBeforeWorkspace(t *testing.T) {
	server, database := newGitTestServer(t)
	store := storage.NewTaskStore(database)
	created, err := store.Create(context.Background(), task.CreateInput{Title: "选择目标分支", TargetBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}

	body := requestJSON(t, server.Client(), http.MethodPut,
		server.URL+"/api/tasks/"+created.ID+"/target-branch",
		fmt.Sprintf(`{"target_branch":"release/macos","expected_version":%d}`, created.Version), http.StatusOK)
	var updated task.Task
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.TargetBranch != "release/macos" {
		t.Fatalf("updated task = %#v", updated)
	}
	requestJSON(t, server.Client(), http.MethodPut,
		server.URL+"/api/tasks/"+created.ID+"/target-branch",
		fmt.Sprintf(`{"target_branch":"missing","expected_version":%d}`, updated.Version), http.StatusBadRequest)
}

func TestTaskRouteValidatesSelectedLocalTargetBranch(t *testing.T) {
	server, _ := newGitTestServer(t)
	body := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/tasks",
		`{"description":"构建 macOS 包","target_branch":"release/macos"}`, http.StatusCreated)
	var created task.Task
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	if created.TargetBranch != "release/macos" {
		t.Fatalf("created task = %#v", created)
	}
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/tasks",
		`{"description":"错误分支","target_branch":"missing"}`, http.StatusBadRequest)
}

func TestGitRoutesExposeChangesAndManualReview(t *testing.T) {
	server, database := newGitTestServer(t)
	store := storage.NewTaskStore(database)
	created, err := store.Create(context.Background(), task.CreateInput{Title: "人工验收"})
	if err != nil {
		t.Fatal(err)
	}
	body := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/tasks/"+created.ID+"/workspace", "", http.StatusOK)
	var worktree workspace.Workspace
	if err := json.Unmarshal(body, &worktree); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree.Path, "README.md"), []byte("# changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changes := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/tasks/"+created.ID+"/changes", "", http.StatusOK)
	if !strings.Contains(string(changes), `"path":"README.md"`) {
		t.Fatalf("changes = %s", changes)
	}
	current, err := store.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/tasks/"+created.ID+"/submit-review",
		fmt.Sprintf(`{"version":%d}`, current.Version), http.StatusOK)
	reviewed := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/tasks/"+created.ID+"/reviews",
		fmt.Sprintf(`{"version":%d,"decision":"ACCEPTED"}`, current.Version+1), http.StatusOK)
	if !strings.Contains(string(reviewed), `"status":"ACCEPTED"`) || !strings.Contains(string(reviewed), `"commit_sha":"`) {
		t.Fatalf("review = %s", reviewed)
	}
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
	runHTTPGit(t, repositoryRoot, "branch", "release/macos")
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
	manager := gitworkflow.New(currentProject, database)
	discussionStore := storage.NewDiscussionStore(database)
	relationStore := storage.NewRelationStore(database)
	RegisterTaskRoutes(mux, storage.NewTaskStore(database), discussionStore, relationStore, storage.NewAssessmentStore(database), manager)
	RegisterGitWorkflowRoutes(mux, manager)
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
