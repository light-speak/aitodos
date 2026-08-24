package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domainrun "github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/storage"
)

func TestRunRoutesExposeUsageSummary(t *testing.T) {
	database := openHTTPTestDatabase(t)
	if _, err := database.ExecContext(t.Context(), `
INSERT INTO run_usage(
    run_id, input_tokens, cached_input_tokens, cache_write_input_tokens,
    output_tokens, reasoning_output_tokens, model_requests, peak_input_tokens,
    source, captured_at
) SELECT id, 16533, 11008, 0, 285, 83, NULL, NULL, 'CODEX_JSONL',
         strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  FROM runs LIMIT 0`); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterRunRoutes(mux, storage.NewRunStore(database), t.TempDir())
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	response, err := server.Client().Get(server.URL + "/api/runs/usage")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var summary domainrun.UsageSummary
	if err := json.NewDecoder(response.Body).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	if summary.RunsWithUsage != 0 || summary.ByPurpose == nil {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestRunRoutesExposeTaskHistoryDetailAndLogs(t *testing.T) {
	database := openHTTPTestDatabase(t)
	store := storage.NewRunStore(database)
	artifactRoot := t.TempDir()
	runID := "run-observable"
	now := "2026-08-21T00:00:00Z"
	if _, err := database.ExecContext(t.Context(), `
INSERT INTO tasks(
    id, task_key, title, title_source, title_locked, description, acceptance_criteria,
    status, priority, target_branch, base_commit_sha, current_workspace_id, latest_run_id,
    assessment_input_version, version, created_at, updated_at
) VALUES ('task-observable', 'ATS-LOG', '查看失败日志', 'HUMAN', 1, '', '', 'READY', 2,
	      'main', '', '', ?, 1, 1, ?, ?)`, runID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `
INSERT INTO runs(
    id, purpose, topic_id, task_id, status, profile_revision_id,
    claim_token_hash, lease_generation, lease_expires_at, run_nonce,
    queued_at, claimed_at, started_at, finished_at, exit_code,
    failure_kind, failure_code, failure_message, failure_retryable,
    created_at, updated_at, subject_version
) VALUES (?, 'IMPLEMENTATION', NULL, 'task-observable', 'FAILED', 'profile-implementer-r1',
          'hash', 1, ?, 'nonce', ?, ?, ?, ?, 7,
          'AGENT_PROCESS', 'NON_ZERO_EXIT', 'agent failed', 0, ?, ?, 1)`,
		runID, now, now, now, now, now, now, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `
INSERT INTO run_events(id, run_id, sequence, event_type, payload_json, occurred_at) VALUES
('event-1', ?, 1, 'RUN_CLAIMED', '{"schema_version":1,"status":"CLAIMED"}', ?),
('event-2', ?, 2, 'RUN_STATUS_CHANGED', '{"schema_version":1,"from":"FINALIZING","to":"FAILED"}', ?)`,
		runID, now, runID, now); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(artifactRoot, "runs", runID, "stderr.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("compile failed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logHash := sha256.Sum256([]byte("compile failed\n"))
	if _, err := store.RecordArtifact(t.Context(), domainrun.Artifact{
		RunID: runID, Kind: "STDERR", RelativePath: "runs/" + runID + "/stderr.log",
		SHA256: hex.EncodeToString(logHash[:]), Size: 15,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListTaskRuns(t.Context(), "task-observable"); err != nil {
		t.Fatalf("ListTaskRuns() before HTTP: %v", err)
	}

	mux := http.NewServeMux()
	RegisterRunRoutes(mux, store, artifactRoot)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	assertRunResponseContains(t, server.URL+"/api/tasks/task-observable/runs", `"failure_code":"NON_ZERO_EXIT"`)
	assertRunResponseContains(t, server.URL+"/api/runs/"+runID, `"artifacts"`)
	assertRunResponseContains(t, server.URL+"/api/runs/"+runID+"/logs?stream=stderr", `"content":"compile failed\n"`)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/runs/"+runID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Last-Event-ID", "1")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	eventStream, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(eventStream), "id: 2\n") || strings.Contains(string(eventStream), "id: 1\n") {
		t.Fatalf("SSE response = %d: %s", response.StatusCode, eventStream)
	}
}

func TestRunRoutesQueryAndRequestCancellation(t *testing.T) {
	database := openHTTPTestDatabase(t)
	now := "2026-08-21T00:00:00Z"
	if _, err := database.ExecContext(t.Context(), `
INSERT INTO tasks(
    id, task_key, title, title_source, title_locked, description, acceptance_criteria,
    status, priority, target_branch, base_commit_sha, current_workspace_id, latest_run_id,
    assessment_input_version, version, created_at, updated_at
) VALUES ('task-cancel', 'ATS-CANCEL', '取消执行', 'HUMAN', 1, '', '', 'RUNNING', 2,
          'main', '', '', '', 1, 2, ?, ?);
INSERT INTO runs(
    id, purpose, task_id, status, profile_revision_id, claim_token_hash,
    lease_generation, lease_expires_at, run_nonce, queued_at, claimed_at,
    started_at, created_at, updated_at, subject_version
) VALUES ('run-cancel', 'IMPLEMENTATION', 'task-cancel', 'RUNNING', 'profile-implementer-r1',
          'hash', 1, ?, 'nonce', ?, ?, ?, ?, ?, 1)`, now, now, now, now, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `
INSERT INTO runs(
    id, purpose, task_id, status, profile_revision_id, claim_token_hash,
    lease_generation, lease_expires_at, run_nonce, queued_at, claimed_at,
    started_at, finished_at, created_at, updated_at, subject_version
) VALUES ('run-finished', 'IMPLEMENTATION', 'task-cancel', 'SUCCEEDED', 'profile-implementer-r1',
          'hash-old', 1, ?, 'nonce-old', ?, ?, ?, ?, ?, ?, 1)`, now, now, now, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE tasks SET latest_run_id = 'run-cancel' WHERE id = 'task-cancel'`); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	store := storage.NewRunStore(database)
	RegisterRunRoutes(mux, store, t.TempDir())
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	assertRunResponseContains(t, server.URL+"/api/runs?status=RUNNING&purpose=IMPLEMENTATION&limit=1", `"id":"run-cancel"`)
	response, err := server.Client().Get(server.URL + "/api/runs?active=true&limit=10")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"id":"run-cancel"`)) || bytes.Contains(body, []byte(`run-finished`)) {
		t.Fatalf("active runs response = %d: %s", response.StatusCode, body)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/api/runs/run-cancel/cancel", bytes.NewBufferString(`{"reason":"停止错误方向"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err = server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err = io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusAccepted || !bytes.Contains(body, []byte(`"cancel_reason":"停止错误方向"`)) {
		t.Fatalf("cancel response = %d: %s", response.StatusCode, body)
	}
	events, err := store.ListEvents(t.Context(), "run-cancel", 0, 10)
	if err != nil || len(events) != 1 || events[0].Type != domainrun.EventCancelRequested {
		t.Fatalf("cancel events = %#v, %v", events, err)
	}
}

func assertRunResponseContains(t *testing.T, url, expected string) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d: %s", url, response.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(expected)) {
		t.Fatalf("GET %s body = %s, want %s", url, body, expected)
	}
}
