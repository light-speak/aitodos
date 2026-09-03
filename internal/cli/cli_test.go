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
	stdout := newSynchronizedBuffer()
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, []string{"start"}, stdout, &stderr)
	}()

	waitForCLIStatus(t, currentProject, true)
	select {
	case code := <-done:
		t.Fatalf("start exited before cancellation with code %d: %s", code, stderr.String())
	default:
	}
	output := stdout.waitForSubstring(t, "前台已启动")
	if strings.Contains(output, "后台") {
		t.Fatalf("start output = %q", output)
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

func TestVersionDoesNotRequireInitializedProject(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"version"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "ats dev") || stderr.Len() != 0 {
		t.Fatalf("Run() = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestMCPRunsProjectReadOnlyProtocol(t *testing.T) {
	repoRoot := initializeCLIProject(t)
	restoreWorkingDirectory(t, repoRoot)
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"test","version":"1"}}}` + "\n")
	var stdout bytes.Buffer
	if err := runMCP(context.Background(), nil, input, &stdout); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"name":"aitodos-project"`) {
		t.Fatalf("MCP output = %q", stdout.String())
	}
}

func TestInitStatusStopAndOpenWithoutRunningDaemon(t *testing.T) {
	repoRoot := t.TempDir()
	command := exec.Command("git", "init", "--quiet", repoRoot)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	restoreWorkingDirectory(t, repoRoot)

	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("init code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "已初始化 ATS 项目") {
		t.Fatalf("init output = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"init"}, &stdout, &stderr); code != 0 {
		t.Fatalf("second init code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ATS 项目已经初始化") {
		t.Fatalf("second init output = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"status"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "ATS 项目未运行") {
		t.Fatalf("status code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"stop"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "ATS 项目未运行") {
		t.Fatalf("stop code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"open"}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "请先执行 ats start") {
		t.Fatalf("open code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestBackupRestoreAndDoctorCommands(t *testing.T) {
	repoRoot := initializeCLIProject(t)
	restoreWorkingDirectory(t, repoRoot)
	backupPath := filepath.Join(repoRoot, ".tmp", "project.zip")

	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"doctor"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "完整性检查通过") {
		t.Fatalf("doctor code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"backup", "--output", backupPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("backup code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup file: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"restore", "--input", backupPath}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "恢复完成") {
		t.Fatalf("restore code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestCommandsRejectInvalidArguments(t *testing.T) {
	repoRoot := initializeCLIProject(t)
	restoreWorkingDirectory(t, repoRoot)
	t.Setenv("ATS_CLAIM_FD", "invalid")
	t.Setenv("ATS_LEASE_GENERATION", "1")

	tests := []struct {
		name string
		args []string
		code int
		want string
	}{
		{name: "missing command", code: 2, want: "用法"},
		{name: "unknown command", args: []string{"unknown"}, code: 2, want: "未知命令"},
		{name: "init positional", args: []string{"init", "extra"}, code: 1, want: "不接受位置参数"},
		{name: "start positional", args: []string{"start", "extra"}, code: 1, want: "不接受位置参数"},
		{name: "invalid port", args: []string{"start", "--port", "65536"}, code: 1, want: "0 到 65535"},
		{name: "status positional", args: []string{"status", "extra"}, code: 1, want: "不接受位置参数"},
		{name: "stop positional", args: []string{"stop", "extra"}, code: 1, want: "不接受位置参数"},
		{name: "open positional", args: []string{"open", "extra"}, code: 1, want: "不接受位置参数"},
		{name: "mcp positional", args: []string{"mcp", "extra"}, code: 1, want: "不接受位置参数"},
		{name: "backup positional", args: []string{"backup", "extra"}, code: 1, want: "不接受位置参数"},
		{name: "restore missing input", args: []string{"restore"}, code: 1, want: "需要 --input"},
		{name: "doctor positional", args: []string{"doctor", "extra"}, code: 1, want: "不接受位置参数"},
		{name: "runner missing flags", args: []string{"runner"}, code: 1, want: "需要 --project"},
		{name: "runner invalid claim fd", args: []string{"runner", "--project", repoRoot, "--run", "run-1", "--nonce", "nonce"}, code: 1, want: "缺少 Claim 环境"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), test.args, &stdout, &stderr)
			if code != test.code || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("Run(%q) = %d, stdout = %q, stderr = %q; want code %d containing %q", test.args, code, stdout.String(), stderr.String(), test.code, test.want)
			}
		})
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
	mu      sync.Mutex
	buffer  bytes.Buffer
	updated chan struct{}
}

func newSynchronizedBuffer() *synchronizedBuffer {
	return &synchronizedBuffer{updated: make(chan struct{}, 1)}
}

func (b *synchronizedBuffer) Write(content []byte) (int, error) {
	b.mu.Lock()
	written, err := b.buffer.Write(content)
	b.mu.Unlock()
	select {
	case b.updated <- struct{}{}:
	default:
	}
	return written, err
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func (b *synchronizedBuffer) waitForSubstring(t *testing.T, expected string) string {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		output := b.String()
		if strings.Contains(output, expected) {
			return output
		}
		select {
		case <-b.updated:
		case <-timer.C:
			t.Fatalf("start output = %q, want substring %q", output, expected)
		}
	}
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
