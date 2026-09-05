package httpapi

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/objective"
	"github.com/light-speak/aitodos/internal/domain/topic"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestObjectiveRoutesCreateCheckpointAndControlLifecycle(t *testing.T) {
	database := openHTTPTestDatabase(t)
	rootTopic, err := storage.NewTopicStore(database).Create(t.Context(), topic.CreateInput{Title: "生产目标"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterObjectiveRoutes(mux, storage.NewObjectiveStore(database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	response := requestKnowledge(t, server, http.MethodPost, "/api/objectives", `{
		"root_topic_id":"`+rootTopic.ID+`",
		"statement":"达到生产可用",
		"constraints":["不自动 push"],
		"completion_criteria":["测试通过"]
	}`, http.StatusCreated)
	var created objective.View
	decodeKnowledge(t, response, &created)
	if created.Objective.Status != objective.StatusActive {
		t.Fatalf("created = %#v", created)
	}
	response = requestKnowledge(t, server, http.MethodGet, "/api/objectives/"+created.Objective.ID, "", http.StatusOK)
	var loaded objective.View
	decodeKnowledge(t, response, &loaded)
	if loaded.Objective.ID != created.Objective.ID {
		t.Fatalf("loaded = %#v", loaded)
	}

	response = requestKnowledge(t, server, http.MethodGet, "/api/objective", "", http.StatusOK)
	var current objective.View
	decodeKnowledge(t, response, &current)
	criterionID := current.Revision.CompletionCriteria[0].ID
	response = requestKnowledge(t, server, http.MethodPost, "/api/objectives/"+current.Objective.ID+"/checkpoints", `{
		"expected_version":1,
		"summary":"完成测试",
		"criteria":[{"criterion_id":"`+criterionID+`","status":"SATISFIED","evidence":"go test ./..."}],
		"stop_reason":"READY_TO_COMPLETE",
		"next_action":"等待人工确认"
	}`, http.StatusCreated)
	var checkpointed objective.View
	decodeKnowledge(t, response, &checkpointed)
	if checkpointed.LatestCheckpoint == nil || checkpointed.Progress.CriteriaSatisfied != 1 {
		t.Fatalf("checkpointed = %#v", checkpointed)
	}
	response = requestKnowledge(t, server, http.MethodGet, "/api/objectives/"+current.Objective.ID+"/checkpoints", "", http.StatusOK)
	var checkpoints []objective.Checkpoint
	decodeKnowledge(t, response, &checkpoints)
	if len(checkpoints) != 1 || checkpoints[0].Summary != "完成测试" {
		t.Fatalf("checkpoints = %#v", checkpoints)
	}

	response = requestKnowledge(t, server, http.MethodPost, "/api/objectives/"+current.Objective.ID+"/achieve", `{"expected_version":2}`, http.StatusOK)
	var achieved objective.View
	decodeKnowledge(t, response, &achieved)
	if achieved.Objective.Status != objective.StatusAchieved {
		t.Fatalf("achieved = %#v", achieved)
	}
	response = requestKnowledge(t, server, http.MethodGet, "/api/objective", "", http.StatusOK)
	var empty any
	if err := json.NewDecoder(response.Body).Decode(&empty); err != nil || empty != nil {
		t.Fatalf("current after achievement = %#v, %v", empty, err)
	}

	response = requestKnowledge(t, server, http.MethodPost, "/api/objectives", `{
		"root_topic_id":"`+rootTopic.ID+`",
		"statement":"持续维护",
		"completion_criteria":["稳定"]
	}`, http.StatusCreated)
	var second objective.View
	decodeKnowledge(t, response, &second)
	response = requestKnowledge(t, server, http.MethodPost, "/api/objectives/"+second.Objective.ID+"/pause", `{"expected_version":1}`, http.StatusOK)
	decodeKnowledge(t, response, &second)
	response = requestKnowledge(t, server, http.MethodPost, "/api/objectives/"+second.Objective.ID+"/resume", `{"expected_version":2}`, http.StatusOK)
	decodeKnowledge(t, response, &second)
	requestKnowledge(t, server, http.MethodPost, "/api/objectives/"+second.Objective.ID+"/cancel", `{"expected_version":3}`, http.StatusOK)
}

func TestObjectiveRoutesRejectInvalidAndConflictingCommands(t *testing.T) {
	database := openHTTPTestDatabase(t)
	rootTopic, err := storage.NewTopicStore(database).Create(t.Context(), topic.CreateInput{Title: "目标"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterObjectiveRoutes(mux, storage.NewObjectiveStore(database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	requestKnowledge(t, server, http.MethodPost, "/api/objectives", `{"root_topic_id":"missing","statement":"x","completion_criteria":["done"]}`, http.StatusNotFound)
	response := requestKnowledge(t, server, http.MethodPost, "/api/objectives", `{"root_topic_id":"`+rootTopic.ID+`","statement":"x","completion_criteria":["done"]}`, http.StatusCreated)
	var created objective.View
	decodeKnowledge(t, response, &created)
	requestKnowledge(t, server, http.MethodPost, "/api/objectives", `{"root_topic_id":"`+rootTopic.ID+`","statement":"two","completion_criteria":["done"]}`, http.StatusConflict)
	requestKnowledge(t, server, http.MethodPost, "/api/objectives/"+created.Objective.ID+"/achieve", `{"expected_version":1}`, http.StatusConflict)
	requestKnowledge(t, server, http.MethodPost, "/api/objectives/"+created.Objective.ID+"/pause", `{"expected_version":99}`, http.StatusConflict)
	requestKnowledge(t, server, http.MethodPost, "/api/objectives/"+created.Objective.ID+"/checkpoints", `{`, http.StatusBadRequest)
	requestKnowledge(t, server, http.MethodPost, "/api/objectives/"+created.Objective.ID+"/checkpoints", `{
		"expected_version":1,
		"summary":"invalid",
		"stop_reason":"UNKNOWN"
	}`, http.StatusBadRequest)
	requestKnowledge(t, server, http.MethodPost, "/api/objectives/"+created.Objective.ID+"/unknown", `{"expected_version":1}`, http.StatusNotFound)
	requestKnowledge(t, server, http.MethodGet, "/api/objectives/missing", "", http.StatusNotFound)
}

func TestObjectiveRoutesHideUnexpectedStorageErrors(t *testing.T) {
	database := openHTTPTestDatabase(t)
	mux := http.NewServeMux()
	RegisterObjectiveRoutes(mux, storage.NewObjectiveStore(database))
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/objective", nil)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "OBJECTIVE_OPERATION_FAILED" || payload.Error.Message != "长期目标操作失败" {
		t.Fatalf("payload = %#v", payload)
	}
	if payload.Error.Message == sql.ErrConnDone.Error() {
		t.Fatal("response leaked storage error")
	}
}
