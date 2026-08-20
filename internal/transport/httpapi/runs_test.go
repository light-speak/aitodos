package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	RegisterRunRoutes(mux, storage.NewRunStore(database))
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
