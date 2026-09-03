package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/light-speak/aitodos/internal/storage"
)

func TestMCPAuditRouteReturnsBoundedEvents(t *testing.T) {
	database := openHTTPTestDatabase(t)
	mux := http.NewServeMux()
	RegisterMCPRoutes(mux, storage.NewMCPAuditStore(database))
	request := httptest.NewRequest(http.MethodGet, "/api/mcp/audit?limit=10", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "[]\n" {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/mcp/audit?limit=0", nil)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestRunMCPAuditRoutesReturnCallsAndResources(t *testing.T) {
	database := openHTTPTestDatabase(t)
	mux := http.NewServeMux()
	RegisterMCPRoutes(mux, storage.NewMCPAuditStore(database))

	for _, path := range []string{"/api/runs/missing/mcp-calls", "/api/runs/missing/resources"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.String() != "[]\n" {
			t.Fatalf("%s response = %d %s", path, response.Code, response.Body.String())
		}
	}
}

func TestMCPAuditRoutesRejectInvalidLimitAndStorageFailures(t *testing.T) {
	database := openHTTPTestDatabase(t)
	mux := http.NewServeMux()
	RegisterMCPRoutes(mux, storage.NewMCPAuditStore(database))

	for _, path := range []string{"/api/mcp/audit?limit=invalid", "/api/runs/run/mcp-calls?limit=invalid"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s response = %d %s", path, response.Code, response.Body.String())
		}
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		path   string
		status int
	}{
		{path: "/api/mcp/audit", status: http.StatusBadRequest},
		{path: "/api/runs/run/mcp-calls", status: http.StatusBadRequest},
		{path: "/api/runs/run/resources", status: http.StatusInternalServerError},
	} {
		request := httptest.NewRequest(http.MethodGet, item.path, nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != item.status {
			t.Fatalf("%s response = %d %s", item.path, response.Code, response.Body.String())
		}
	}
}
