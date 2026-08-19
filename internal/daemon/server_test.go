package daemon

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/project"
)

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
