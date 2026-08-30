package cli

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/daemon"
	"github.com/light-speak/aitodos/internal/project"
)

func TestStartRunsInForegroundUntilContextCancellation(t *testing.T) {
	repoRoot := initializeCLIProject(t)
	currentProject, err := project.Load(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	restoreWorkingDirectory(t, repoRoot)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout synchronizedBuffer
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, []string{"start"}, &stdout, &stderr)
	}()

	waitForCLIStatus(t, currentProject, true)
	select {
	case code := <-done:
		t.Fatalf("start exited before cancellation with code %d: %s", code, stderr.String())
	default:
	}
	if strings.Contains(stdout.String(), "后台") || !strings.Contains(stdout.String(), "前台已启动") {
		t.Fatalf("start output = %q", stdout.String())
	}

	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("start exit code = %d, stderr = %q", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("start did not stop after context cancellation")
	}
	waitForCLIStatus(t, currentProject, false)
}

func TestStartPortFlagOverridesProjectConfiguration(t *testing.T) {
	repoRoot := initializeCLIProject(t)
	restoreWorkingDirectory(t, repoRoot)
	port := availablePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, []string{"start", "--port", fmt.Sprint(port)}, &stdout, &stderr)
	}()

	currentProject, err := project.Load(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	waitForCLIStatus(t, currentProject, true)
	metadata, running := daemon.Status(context.Background(), currentProject)
	if !running || metadata.Port != port {
		t.Fatalf("Status() = %#v, %v; want port %d", metadata, running, port)
	}

	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("start exit code = %d, stderr = %q", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("start did not stop after context cancellation")
	}
}

func TestStartUsesFixedPortFromLocalConfiguration(t *testing.T) {
	repoRoot := initializeCLIProject(t)
	restoreWorkingDirectory(t, repoRoot)
	port := availablePort(t)
	currentProject, err := project.Load(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	setLocalServerPort(t, currentProject.Paths.LocalConfig, port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, []string{"start"}, &stdout, &stderr)
	}()

	currentProject, err = project.Load(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	waitForCLIStatus(t, currentProject, true)
	metadata, running := daemon.Status(context.Background(), currentProject)
	if !running || metadata.Port != port {
		t.Fatalf("Status() = %#v, %v; want port %d", metadata, running, port)
	}

	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("start exit code = %d, stderr = %q", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("start did not stop after context cancellation")
	}
}

func TestStartRejectsRemovedNoOpenFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"start", "--no-open"}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("Run() = %d, stderr = %q", code, stderr.String())
	}
}

func TestStartRejectsRemovedForegroundFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"start", "--foreground"}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("Run() = %d, stderr = %q", code, stderr.String())
	}
}

func TestHelpOnlyDocumentsForegroundStart(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() = %d, stderr = %q", code, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "start [--port PORT]") ||
		!strings.Contains(output, "不会自动打开浏览器") ||
		strings.Contains(output, "--no-open") ||
		strings.Contains(output, "--foreground") ||
		strings.Contains(output, "后台") {
		t.Fatalf("help output = %q", output)
	}
}

func availablePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func setLocalServerPort(t *testing.T, path string, port int) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(content), "port = 0", "port = "+fmt.Sprint(port), 1)
	if updated == string(content) {
		t.Fatalf("local config does not contain default server port: %s", content)
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
}

func initializeCLIProject(t *testing.T) string {
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
	if _, _, err := project.Initialize(context.Background(), resolved); err != nil {
		t.Fatal(err)
	}
	return resolved
}

func restoreWorkingDirectory(t *testing.T, directory string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}

type synchronizedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *synchronizedBuffer) Write(content []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(content)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func waitForCLIStatus(t *testing.T, currentProject *project.Project, wantRunning bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, running := daemon.Status(context.Background(), currentProject)
		if running == wantRunning {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("daemon running = %v, want %v", running, wantRunning)
		case <-ticker.C:
		}
	}
}
