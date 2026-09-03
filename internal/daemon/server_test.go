package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/project"
)

func TestLocalRequestGuardRejectsCrossSiteMutationAndForeignHost(t *testing.T) {
	called := 0
	handler := localRequestGuard(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		called++
		response.WriteHeader(http.StatusNoContent)
	}))

	foreignHost := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/health", nil)
	foreignHost.Host = "attacker.example"
	foreignHostResponse := httptest.NewRecorder()
	handler.ServeHTTP(foreignHostResponse, foreignHost)
	if foreignHostResponse.Code != http.StatusForbidden {
		t.Fatalf("foreign host status = %d", foreignHostResponse.Code)
	}

	crossSite := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/project/workers", bytes.NewBufferString(`{"enabled":true}`))
	crossSite.Host = "127.0.0.1"
	crossSite.Header.Set("Origin", "https://attacker.example")
	crossSite.Header.Set("Content-Type", "text/plain")
	crossSiteResponse := httptest.NewRecorder()
	handler.ServeHTTP(crossSiteResponse, crossSite)
	if crossSiteResponse.Code != http.StatusForbidden {
		t.Fatalf("cross-site status = %d", crossSiteResponse.Code)
	}

	sameOrigin := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/project/workers", bytes.NewBufferString(`{"enabled":true}`))
	sameOrigin.Host = "127.0.0.1"
	sameOrigin.Header.Set("Origin", "http://127.0.0.1")
	sameOriginResponse := httptest.NewRecorder()
	handler.ServeHTTP(sameOriginResponse, sameOrigin)
	if sameOriginResponse.Code != http.StatusNoContent || called != 1 {
		t.Fatalf("same-origin status = %d, called = %d", sameOriginResponse.Code, called)
	}
}

func TestServePublishesHealthAndStopsCleanly(t *testing.T) {
	repoRoot := initProject(t)
	currentProject, err := project.Load(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan Metadata, 1)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, currentProject, "test-nonce", 0, func(metadata Metadata) {
			ready <- metadata
		})
	}()

	var metadata Metadata
	select {
	case metadata = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("Serve() did not become ready")
	}

	response, err := http.Get(metadata.URL + "/api/health")
	if err != nil {
		t.Fatalf("GET health: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", response.StatusCode)
	}

	var health Health
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health.Status != "ok" || health.ProjectInstanceID != currentProject.InstanceID {
		t.Fatalf("health = %#v", health)
	}

	topicsResponse, err := http.Get(metadata.URL + "/api/topics")
	if err != nil {
		t.Fatalf("GET topics: %v", err)
	}
	defer topicsResponse.Body.Close()
	if topicsResponse.StatusCode != http.StatusOK {
		t.Fatalf("topics status = %d, want 200", topicsResponse.StatusCode)
	}

	status, running := Status(context.Background(), currentProject)
	if !running || status.Nonce != "test-nonce" {
		t.Fatalf("Status() = %#v, %v", status, running)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve() did not stop")
	}

	if _, running := Status(context.Background(), currentProject); running {
		t.Fatal("Status() running = true after shutdown")
	}
}

func TestServeUsesConfiguredPort(t *testing.T) {
	repoRoot := initProject(t)
	currentProject, err := project.Load(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan Metadata, 1)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, currentProject, "fixed-port", port, func(metadata Metadata) {
			ready <- metadata
		})
	}()

	select {
	case metadata := <-ready:
		if metadata.Port != port {
			t.Fatalf("metadata port = %d, want %d", metadata.Port, port)
		}
	case err := <-done:
		t.Fatalf("Serve() exited before ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Serve() did not become ready")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve() did not stop")
	}
}

func TestStopSignalsRunningDaemonProcess(t *testing.T) {
	repoRoot := initProject(t)
	currentProject, err := project.Load(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestDaemonStopHelper$")
	command.Env = append(os.Environ(), "ATS_DAEMON_STOP_HELPER=1")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	var alive atomic.Bool
	alive.Store(true)
	waited := make(chan error, 1)
	go func() {
		waited <- command.Wait()
		alive.Store(false)
	}()
	nonce := "stop-test"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		if !alive.Load() {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(response).Encode(Health{
			Status: "ok", ProjectInstanceID: currentProject.InstanceID, Nonce: nonce, PID: command.Process.Pid,
		})
	}))
	t.Cleanup(server.Close)
	metadata := Metadata{
		ProjectInstanceID: currentProject.InstanceID, PID: command.Process.Pid, URL: server.URL, Nonce: nonce,
	}
	if err := writeMetadata(currentProject.Paths.DaemonState, metadata); err != nil {
		t.Fatal(err)
	}

	stopped, err := Stop(context.Background(), currentProject)
	if err != nil || !stopped {
		t.Fatalf("Stop() = %v, %v", stopped, err)
	}
	select {
	case <-waited:
	case <-time.After(3 * time.Second):
		t.Fatal("daemon helper did not exit")
	}
}

func TestStopReturnsFalseWhenDaemonIsNotRunning(t *testing.T) {
	repoRoot := initProject(t)
	currentProject, err := project.Load(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := Stop(context.Background(), currentProject)
	if err != nil || stopped {
		t.Fatalf("Stop() = %v, %v", stopped, err)
	}
}

func TestDaemonStopHelper(t *testing.T) {
	if os.Getenv("ATS_DAEMON_STOP_HELPER") != "1" {
		return
	}
	time.Sleep(time.Minute)
}

func initProject(t *testing.T) string {
	t.Helper()

	repoRoot := t.TempDir()
	command := exec.Command("git", "init", "--quiet", repoRoot)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	resolved, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	repoRoot = resolved
	if _, _, err := project.Initialize(context.Background(), repoRoot); err != nil {
		t.Fatal(err)
	}
	return repoRoot
}
