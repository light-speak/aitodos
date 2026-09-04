package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestAgentProfileRoutesListAndCreateRevision(t *testing.T) {
	database := openHTTPTestDatabase(t)
	mux := http.NewServeMux()
	RegisterAgentProfileRoutes(mux, storage.NewAgentProfileStore(database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	profiles := requestAgentProfiles(t, server.Client(), server.URL+"/api/agent-profiles")
	if len(profiles) != 5 {
		t.Fatalf("profiles count = %d", len(profiles))
	}
	implementer := profiles[2]
	updated := requestAgentProfile(t, server.Client(), http.MethodPost,
		server.URL+"/api/agent-profiles/"+implementer.ID+"/revisions", `{
			"instructions":"实现当前任务并报告测试证据",
			"adapter":"generic",
			"command":"codex",
			"args":["exec","--json"],
			"model":"gpt-5",
			"max_input_tokens":64000,
			"reserved_output_tokens":12000,
			"recent_message_limit":20,
			"retrieval_limit":8,
			"timeout_seconds":3600
		}`, http.StatusCreated)
	if updated.CurrentRevision.Revision != 2 || updated.CurrentRevision.Command != "codex" {
		t.Fatalf("updated profile = %#v", updated)
	}

	historyResponse, err := server.Client().Get(server.URL + "/api/agent-profiles/" + implementer.ID + "/revisions")
	if err != nil {
		t.Fatal(err)
	}
	defer historyResponse.Body.Close()
	var history []agentprofile.Revision
	if err := json.NewDecoder(historyResponse.Body).Decode(&history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("history count = %d", len(history))
	}
}

func TestAgentProfileRoutesRejectPermissionOverride(t *testing.T) {
	database := openHTTPTestDatabase(t)
	mux := http.NewServeMux()
	RegisterAgentProfileRoutes(mux, storage.NewAgentProfileStore(database))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	requestAgentProfile(t, server.Client(), http.MethodPost,
		server.URL+"/api/agent-profiles/profile-planner/revisions", `{
			"instructions":"越权写代码",
			"adapter":"generic",
			"command":"codex",
			"args":[],
			"max_input_tokens":64000,
			"reserved_output_tokens":12000,
			"recent_message_limit":20,
			"retrieval_limit":8,
			"timeout_seconds":3600,
			"workspace_policy":"WRITE_TASK"
		}`, http.StatusBadRequest)
}

func TestAgentProfileRoutesConfigureCodexDefaults(t *testing.T) {
	database := openHTTPTestDatabase(t)
	mux := http.NewServeMux()
	RegisterAgentProfileRoutes(mux, storage.NewAgentProfileStore(database), func(command string) (string, error) {
		if command != "codex" {
			t.Fatalf("probe command = %q", command)
		}
		return "/usr/local/bin/codex", nil
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/agent-profiles/configure-codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var profiles []agentprofile.Profile
	if err := json.NewDecoder(response.Body).Decode(&profiles); err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 5 || profiles[0].CurrentRevision.Command != "/usr/local/bin/codex" {
		t.Fatalf("profiles = %#v", profiles)
	}
}

func TestAgentProfileRoutesReportMissingCodex(t *testing.T) {
	database := openHTTPTestDatabase(t)
	mux := http.NewServeMux()
	RegisterAgentProfileRoutes(mux, storage.NewAgentProfileStore(database), func(string) (string, error) {
		return "", errors.New("missing")
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/agent-profiles/configure-codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d", response.StatusCode)
	}
	requestAgentProfile(t, server.Client(), http.MethodPost,
		server.URL+"/api/agent-profiles/profile-planner/revisions", `{`, http.StatusBadRequest)
}

func requestAgentProfiles(t *testing.T, client *http.Client, url string) []agentprofile.Profile {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d", url, response.StatusCode)
	}
	var profiles []agentprofile.Profile
	if err := json.NewDecoder(response.Body).Decode(&profiles); err != nil {
		t.Fatal(err)
	}
	return profiles
}

func requestAgentProfile(
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	body string,
	wantStatus int,
) agentprofile.Profile {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d", method, url, response.StatusCode, wantStatus)
	}
	if wantStatus >= 400 {
		return agentprofile.Profile{}
	}
	var profile agentprofile.Profile
	if err := json.NewDecoder(response.Body).Decode(&profile); err != nil {
		t.Fatal(err)
	}
	return profile
}
