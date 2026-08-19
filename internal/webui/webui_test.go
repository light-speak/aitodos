package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestNewHandlerServesEmbeddedApplicationAndImmutableAssets(t *testing.T) {
	handler, err := NewHandler()
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	index := readResponse(t, response, http.StatusOK)
	if !strings.Contains(index, `<div id="root"></div>`) {
		t.Fatalf("index does not contain application root: %s", index)
	}
	iconPath := regexp.MustCompile(`rel="icon"[^>]+href="([^"]+)"`).FindStringSubmatch(index)
	if len(iconPath) != 2 {
		t.Fatalf("index does not reference favicon: %s", index)
	}
	iconResponse, err := http.Get(server.URL + iconPath[1])
	if err != nil {
		t.Fatal(err)
	}
	_ = readResponse(t, iconResponse, http.StatusOK)

	assetPath := regexp.MustCompile(`src="(/[^"]+\.js)"`).FindStringSubmatch(index)
	if len(assetPath) != 2 {
		t.Fatalf("index does not reference JavaScript asset: %s", index)
	}
	assetResponse, err := http.Get(server.URL + assetPath[1])
	if err != nil {
		t.Fatal(err)
	}
	_ = readResponse(t, assetResponse, http.StatusOK)
	if cacheControl := assetResponse.Header.Get("Cache-Control"); cacheControl != "public, max-age=31536000, immutable" {
		t.Fatalf("asset Cache-Control = %q", cacheControl)
	}
}

func readResponse(t *testing.T, response *http.Response, wantStatus int) string {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		t.Fatalf("response status = %d, want %d", response.StatusCode, wantStatus)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
