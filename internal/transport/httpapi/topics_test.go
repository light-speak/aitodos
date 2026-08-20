package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/discussion"
	"github.com/light-speak/aitodos/internal/domain/relation"
	"github.com/light-speak/aitodos/internal/domain/topic"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestTopicRoutesCreateListAndGet(t *testing.T) {
	server := newTopicTestServer(t)

	created := requestTopic(t, server.Client(), http.MethodPost, server.URL+"/api/topics", `{
		"title":"讨论 Agent 上下文",
		"description":"明确 Session 和持久上下文的边界"
	}`, http.StatusCreated)
	if created.Status != topic.StatusOpen || created.Version != 1 {
		t.Fatalf("created topic = %#v", created)
	}

	response, err := server.Client().Get(server.URL + "/api/topics")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET topics status = %d", response.StatusCode)
	}
	var listed []topic.Topic
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed topics = %#v", listed)
	}

	loaded := requestTopic(t, server.Client(), http.MethodGet, server.URL+"/api/topics/"+created.ID, "", http.StatusOK)
	if loaded.ID != created.ID {
		t.Fatalf("loaded topic = %#v", loaded)
	}
}

func TestTopicRoutesValidateInputAndNotFound(t *testing.T) {
	server := newTopicTestServer(t)
	requestTopic(t, server.Client(), http.MethodPost, server.URL+"/api/topics", `{"title":""}`, http.StatusBadRequest)
	created := requestTopic(t, server.Client(), http.MethodPost, server.URL+"/api/topics", `{"description":"只描述我想解决的问题"}`, http.StatusCreated)
	if created.Title != "只描述我想解决的问题" {
		t.Fatalf("derived title = %q", created.Title)
	}
	requestTopic(t, server.Client(), http.MethodPost, server.URL+"/api/topics", `{"title":"需求","unknown":true}`, http.StatusBadRequest)
	requestTopic(t, server.Client(), http.MethodGet, server.URL+"/api/topics/missing", "", http.StatusNotFound)
}

func TestTopicRoutesCreateAndListMessages(t *testing.T) {
	server := newTopicTestServer(t)
	createdTopic := requestTopic(t, server.Client(), http.MethodPost, server.URL+"/api/topics", `{"title":"讨论消息"}`, http.StatusCreated)

	createdMessage := requestMessage(
		t,
		server.Client(),
		http.MethodPost,
		server.URL+"/api/topics/"+createdTopic.ID+"/messages",
		`{"content":" 记录这个约束 "}`,
		http.StatusCreated,
	)
	if createdMessage.Content != "记录这个约束" || createdMessage.AuthorKind != discussion.AuthorHuman {
		t.Fatalf("created message = %#v", createdMessage)
	}

	response, err := server.Client().Get(server.URL + "/api/topics/" + createdTopic.ID + "/messages")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET messages status = %d", response.StatusCode)
	}
	var messages []discussion.Message
	if err := json.NewDecoder(response.Body).Decode(&messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID != createdMessage.ID {
		t.Fatalf("messages = %#v", messages)
	}

	requestMessage(t, server.Client(), http.MethodPost, server.URL+"/api/topics/"+createdTopic.ID+"/messages", `{"content":""}`, http.StatusBadRequest)
	requestMessage(t, server.Client(), http.MethodGet, server.URL+"/api/topics/missing/messages", "", http.StatusNotFound)
}

func TestTopicRoutesLinkTasksDirectlyAndFromMessages(t *testing.T) {
	server := newTopicTestServer(t)
	createdTopic := requestTopic(t, server.Client(), http.MethodPost, server.URL+"/api/topics", `{"title":"讨论搜索"}`, http.StatusCreated)
	firstTask := requestTask(t, server.Client(), http.MethodPost, server.URL+"/api/tasks", `{"title":"实现索引"}`, http.StatusCreated)
	secondTask := requestTask(t, server.Client(), http.MethodPost, server.URL+"/api/tasks", `{"title":"补充筛选"}`, http.StatusCreated)

	requestStatus(t, server.Client(), http.MethodPost, server.URL+"/api/topics/"+createdTopic.ID+"/relations", `{"task_id":"`+firstTask.ID+`"}`, http.StatusNoContent)
	message := requestMessage(t, server.Client(), http.MethodPost, server.URL+"/api/topics/"+createdTopic.ID+"/messages", `{
		"content":"这条讨论产生了筛选任务",
		"linked_task_ids":["`+secondTask.ID+`"]
	}`, http.StatusCreated)
	if len(message.LinkedTaskIDs) != 1 || message.LinkedTaskIDs[0] != secondTask.ID {
		t.Fatalf("message linked tasks = %#v", message.LinkedTaskIDs)
	}

	response, err := server.Client().Get(server.URL + "/api/topics/" + createdTopic.ID + "/relations")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var linked []relation.TaskAssociation
	if err := json.NewDecoder(response.Body).Decode(&linked); err != nil {
		t.Fatal(err)
	}
	if len(linked) != 2 || linked[1].Task.ID != secondTask.ID || linked[1].SourceMessageID != message.ID {
		t.Fatalf("topic relations = %#v", linked)
	}
	topicResponse, err := server.Client().Get(server.URL + "/api/tasks/" + secondTask.ID + "/topics")
	if err != nil {
		t.Fatal(err)
	}
	defer topicResponse.Body.Close()
	var linkedTopics []relation.TopicAssociation
	if err := json.NewDecoder(topicResponse.Body).Decode(&linkedTopics); err != nil {
		t.Fatal(err)
	}
	if len(linkedTopics) != 1 || linkedTopics[0].Topic.ID != createdTopic.ID {
		t.Fatalf("task topics = %#v", linkedTopics)
	}

	requestStatus(t, server.Client(), http.MethodDelete, server.URL+"/api/topics/"+createdTopic.ID+"/relations/"+firstTask.ID, "", http.StatusNoContent)
}

func newTopicTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	database := openHTTPTestDatabase(t)
	mux := http.NewServeMux()
	discussionStore := storage.NewDiscussionStore(database)
	relationStore := storage.NewRelationStore(database)
	RegisterTopicRoutes(mux, storage.NewTopicStore(database), discussionStore, relationStore)
	taskStore := storage.NewTaskStore(database)
	RegisterTaskRoutes(mux, taskStore, discussionStore, relationStore, storage.NewAssessmentStore(database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func requestMessage(
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	body string,
	wantStatus int,
) discussion.Message {
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
		return discussion.Message{}
	}
	var result discussion.Message
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func requestTopic(
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	body string,
	wantStatus int,
) topic.Topic {
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
		return topic.Topic{}
	}
	var result topic.Topic
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
