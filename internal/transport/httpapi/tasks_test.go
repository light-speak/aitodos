package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/discussion"
	"github.com/light-speak/aitodos/internal/domain/relation"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestTaskRoutesCreateListGetAndQueue(t *testing.T) {
	server := newTaskTestServer(t)

	created := requestTask(t, server.Client(), http.MethodPost, server.URL+"/api/tasks", `{
		"title":"实现任务看板",
		"description":"项目级看板",
		"acceptance_criteria":"可以排队任务",
		"priority":10
	}`, http.StatusCreated)
	if created.Status != task.StatusBacklog {
		t.Fatalf("created status = %q", created.Status)
	}

	response, err := server.Client().Get(server.URL + "/api/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET tasks status = %d", response.StatusCode)
	}
	var listed []task.Task
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed tasks = %#v", listed)
	}

	loaded := requestTask(t, server.Client(), http.MethodGet, server.URL+"/api/tasks/"+created.ID, "", http.StatusOK)
	if loaded.ID != created.ID {
		t.Fatalf("loaded task = %#v", loaded)
	}

	queued := requestTask(t, server.Client(), http.MethodPost, server.URL+"/api/tasks/"+created.ID+"/queue", `{"version":1}`, http.StatusOK)
	if queued.Status != task.StatusReady || queued.Version != 2 {
		t.Fatalf("queued task = %#v", queued)
	}

	requestTask(t, server.Client(), http.MethodPost, server.URL+"/api/tasks/"+created.ID+"/queue", `{"version":2}`, http.StatusConflict)
	requestTask(t, server.Client(), http.MethodPost, server.URL+"/api/tasks/"+created.ID+"/queue", `{"version":1}`, http.StatusConflict)
}

func TestTaskRoutesValidateInputAndNotFound(t *testing.T) {
	server := newTaskTestServer(t)
	requestTask(t, server.Client(), http.MethodPost, server.URL+"/api/tasks", `{"title":""}`, http.StatusBadRequest)
	created := requestTask(t, server.Client(), http.MethodPost, server.URL+"/api/tasks", `{"description":"直接修复登录按钮"}`, http.StatusCreated)
	if created.Title != "直接修复登录按钮" {
		t.Fatalf("derived title = %q", created.Title)
	}
	requestTask(t, server.Client(), http.MethodPost, server.URL+"/api/tasks", `{"title":"任务","unknown":true}`, http.StatusBadRequest)
	requestTask(t, server.Client(), http.MethodGet, server.URL+"/api/tasks/missing", "", http.StatusNotFound)
	requestTask(t, server.Client(), http.MethodPost, server.URL+"/api/tasks/missing/queue", `{"version":1}`, http.StatusNotFound)
}

func TestTaskRoutesDiscussAndRelateTasks(t *testing.T) {
	server := newTaskTestServer(t)
	owner := requestTask(t, server.Client(), http.MethodPost, server.URL+"/api/tasks", `{"title":"前端交互"}`, http.StatusCreated)
	related := requestTask(t, server.Client(), http.MethodPost, server.URL+"/api/tasks", `{"title":"后端接口"}`, http.StatusCreated)

	createdMessage := requestMessage(t, server.Client(), http.MethodPost, server.URL+"/api/tasks/"+owner.ID+"/messages", `{
		"content":"需要后端接口配合",
		"linked_task_ids":["`+related.ID+`"]
	}`, http.StatusCreated)
	if len(createdMessage.LinkedTaskIDs) != 1 || createdMessage.LinkedTaskIDs[0] != related.ID {
		t.Fatalf("message = %#v", createdMessage)
	}

	messageResponse, err := server.Client().Get(server.URL + "/api/tasks/" + owner.ID + "/messages")
	if err != nil {
		t.Fatal(err)
	}
	defer messageResponse.Body.Close()
	var messages []discussion.Message
	if err := json.NewDecoder(messageResponse.Body).Decode(&messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID != createdMessage.ID {
		t.Fatalf("messages = %#v", messages)
	}

	relationResponse, err := server.Client().Get(server.URL + "/api/tasks/" + related.ID + "/relations")
	if err != nil {
		t.Fatal(err)
	}
	defer relationResponse.Body.Close()
	var linked []relation.TaskAssociation
	if err := json.NewDecoder(relationResponse.Body).Decode(&linked); err != nil {
		t.Fatal(err)
	}
	if len(linked) != 1 || linked[0].Task.ID != owner.ID || linked[0].SourceMessageID != createdMessage.ID {
		t.Fatalf("task relations = %#v", linked)
	}

	requestStatus(t, server.Client(), http.MethodDelete, server.URL+"/api/tasks/"+owner.ID+"/relations/"+related.ID, "", http.StatusNoContent)
	requestStatus(t, server.Client(), http.MethodPost, server.URL+"/api/tasks/"+owner.ID+"/relations", `{"task_id":"`+owner.ID+`"}`, http.StatusBadRequest)
}

func newTaskTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	database := openHTTPTestDatabase(t)
	mux := http.NewServeMux()
	discussionStore := storage.NewDiscussionStore(database)
	relationStore := storage.NewRelationStore(database)
	RegisterTaskRoutes(mux, storage.NewTaskStore(database), discussionStore, relationStore)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func requestStatus(
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	body string,
	wantStatus int,
) {
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
}

func openHTTPTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"), storage.ProjectMetadata{
		InstanceID:   "http-test-instance",
		Name:         "http-test",
		RepoRoot:     "/tmp/http-test",
		GitCommonDir: "/tmp/http-test/.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func requestTask(
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	body string,
	wantStatus int,
) task.Task {
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
		return task.Task{}
	}
	var result task.Task
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
