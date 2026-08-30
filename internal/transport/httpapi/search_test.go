package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/search"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestSearchRoutesFilterAndPaginateProjectDocuments(t *testing.T) {
	database := openHTTPTestDatabase(t)
	tasks := storage.NewTaskStore(database)
	first, err := tasks.Create(t.Context(), task.CreateInput{Title: "实现项目搜索", Description: "SQLite FTS5"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.Create(t.Context(), task.CreateInput{Title: "搜索结果分页", Description: "稳定游标"}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterSearchRoutes(mux, storage.NewSearchStore(database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	response, err := server.Client().Get(server.URL + "/api/search?q=%E6%90%9C%E7%B4%A2&kind=TASK&limit=1")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/search status = %d", response.StatusCode)
	}
	var page search.Page
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Kind != search.KindTask || page.NextCursor == "" {
		t.Fatalf("page = %#v", page)
	}
	if page.Items[0].SourceID != first.ID && page.Items[0].Title != "搜索结果分页" {
		t.Fatalf("unexpected first result = %#v", page.Items[0])
	}

	nextResponse, err := server.Client().Get(server.URL + "/api/search?q=%E6%90%9C%E7%B4%A2&kind=TASK&limit=1&cursor=" + page.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	defer nextResponse.Body.Close()
	var next search.Page
	if err := json.NewDecoder(nextResponse.Body).Decode(&next); err != nil {
		t.Fatal(err)
	}
	if len(next.Items) != 1 || next.Items[0].SourceID == page.Items[0].SourceID {
		t.Fatalf("next page = %#v", next)
	}
}

func TestSearchRoutesRejectInvalidQueries(t *testing.T) {
	mux := http.NewServeMux()
	RegisterSearchRoutes(mux, storage.NewSearchStore(openHTTPTestDatabase(t)))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	for _, path := range []string{
		"/api/search",
		"/api/search?q=test&kind=UNKNOWN",
		"/api/search?q=test&limit=51",
		"/api/search?q=test&cursor=invalid",
		"/api/search?q=test&updated_after=not-a-time",
	} {
		response, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET %s status = %d, want 400", path, response.StatusCode)
		}
	}
}
