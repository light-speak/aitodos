package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/assessment"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestAssessmentRouteReturnsExplicitUnknownState(t *testing.T) {
	database := openHTTPTestDatabase(t)
	tasks := storage.NewTaskStore(database)
	created, err := tasks.Create(t.Context(), task.CreateInput{Description: "等待 AI 评估"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterAssessmentRoutes(mux, tasks, storage.NewAssessmentStore(database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	response, err := server.Client().Get(server.URL + "/api/tasks/" + created.ID + "/assessment")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var result struct {
		Current *assessment.Assessment  `json:"current"`
		History []assessment.Assessment `json:"history"`
		Stale   bool                    `json:"stale"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Current != nil || len(result.History) != 0 || result.Stale {
		t.Fatalf("assessment response = %#v", result)
	}
}
