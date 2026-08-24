package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/approvalrequest"
	domainrun "github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/storage"
)

const maxAppServerMessageBytes = 8 << 20

var errApprovalRunCancelled = errors.New("run cancelled while waiting for approval")

type appServerApprovalBridge struct {
	store           *storage.RunStore
	runID           string
	claimToken      string
	leaseGeneration int64
}

type appServerMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type appServerClient struct {
	command  *exec.Cmd
	stdin    io.WriteCloser
	messages chan appServerMessage
	readErr  chan error
	wait     chan error
	stdout   *boundedLog
	stderr   *boundedLog
}

func invokeCodexAppServer(
	ctx context.Context,
	currentProject *project.Project,
	revision agentprofile.Revision,
	purpose domainrun.Purpose,
	workspacePath string,
	runID string,
	promptPath string,
	prompt string,
	toolArgs []string,
	externalSessionID string,
	approvals appServerApprovalBridge,
	cancelRequested func() (bool, error),
) processResult {
	client, err := startAppServer(currentProject, revision, purpose, workspacePath, runID, promptPath, toolArgs)
	if err != nil {
		return processResult{Status: domainrun.StatusFailed, ExitCode: -1, Code: "SPAWN_FAILED", Err: err}
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(revision.TimeoutSeconds)*time.Second)
	defer cancel()
	stopPolling := monitorAppServerCancellation(runCtx, cancel, cancelRequested)
	defer stopPolling()
	outcome := runAppServerTurn(runCtx, client, revision, purpose, workspacePath, prompt, externalSessionID, approvals)
	closeErr := closeAppServer(client)
	if outcome.Err == nil && closeErr != nil {
		outcome.Status, outcome.Code, outcome.Err = domainrun.StatusFailed, "APP_SERVER_EXIT", closeErr
	}
	outcome.ExitCode = processExitCode(client.command.ProcessState)
	outcome.Stdout, outcome.Stderr = client.stdout.Bytes(), client.stderr.Bytes()
	outcome.StdoutTruncated, outcome.StderrTruncated = client.stdout.Truncated(), client.stderr.Truncated()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		outcome.Status, outcome.Code, outcome.Err = domainrun.StatusTimedOut, "TIMEOUT", errors.New("agent timed out")
	} else if errors.Is(runCtx.Err(), context.Canceled) && outcome.Err != nil {
		outcome.Status, outcome.Code = domainrun.StatusCancelled, "USER_CANCELLED"
	}
	return outcome
}

func startAppServer(
	currentProject *project.Project,
	revision agentprofile.Revision,
	purpose domainrun.Purpose,
	workspacePath string,
	runID string,
	promptPath string,
	toolArgs []string,
) (*appServerClient, error) {
	args := append([]string{"app-server", "--stdio"}, toolArgs...)
	args = append(args, revision.Args...)
	command := exec.Command(revision.Command, args...)
	command.Dir = workspacePath
	command.Env = agentEnvironment(currentProject, purpose, runID, workspacePath, promptPath)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open app server stdin: %w", err)
	}
	stdoutPipe, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open app server stdout: %w", err)
	}
	client := &appServerClient{
		command: command, stdin: stdin, messages: make(chan appServerMessage), readErr: make(chan error, 1),
		wait: make(chan error, 1), stdout: newBoundedLog(maxLogBytes), stderr: newBoundedLog(maxLogBytes),
	}
	command.Stderr = client.stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	go readAppServerMessages(stdoutPipe, client.stdout, client.messages, client.readErr)
	go func() { client.wait <- command.Wait() }()
	return client, nil
}

func readAppServerMessages(reader io.Reader, log *boundedLog, messages chan<- appServerMessage, result chan<- error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maxAppServerMessageBytes)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		_, _ = log.Write(append(line, '\n'))
		var message appServerMessage
		if err := json.Unmarshal(line, &message); err != nil {
			result <- fmt.Errorf("decode app server message: %w", err)
			return
		}
		messages <- message
	}
	if err := scanner.Err(); err != nil {
		result <- fmt.Errorf("read app server output: %w", err)
		return
	}
	result <- io.EOF
}

func monitorAppServerCancellation(
	ctx context.Context,
	cancel context.CancelFunc,
	cancelRequested func() (bool, error),
) func() {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				requested, err := cancelRequested()
				if err != nil || requested {
					cancel()
					return
				}
			}
		}
	}()
	return func() { close(stop) }
}

func runAppServerTurn(
	ctx context.Context,
	client *appServerClient,
	revision agentprofile.Revision,
	purpose domainrun.Purpose,
	workspacePath string,
	prompt string,
	externalSessionID string,
	approvals appServerApprovalBridge,
) processResult {
	if err := initializeAppServer(ctx, client); err != nil {
		return appServerFailure("APP_SERVER_INITIALIZE", err)
	}
	threadID, err := startOrResumeThread(ctx, client, revision, workspacePath, externalSessionID)
	if err != nil {
		return appServerFailure("APP_SERVER_THREAD", err)
	}
	turnID, err := startAppServerTurn(ctx, client, revision, workspacePath, threadID, prompt)
	if err != nil {
		return appServerFailure("APP_SERVER_TURN", err)
	}
	status, err := waitForAppServerTurn(ctx, client, approvals, purpose, workspacePath, threadID, turnID)
	if err != nil {
		return appServerFailure("APP_SERVER_PROTOCOL", err)
	}
	return processResult{Status: status, ExternalSessionID: threadID}
}

func initializeAppServer(ctx context.Context, client *appServerClient) error {
	params := map[string]any{
		"clientInfo":   map[string]string{"name": "aitodos", "version": "1"},
		"capabilities": map[string]any{"experimentalApi": true},
	}
	if err := client.request(ctx, 1, "initialize", params, &struct{}{}); err != nil {
		return err
	}
	return client.write(map[string]any{"method": "initialized"})
}

func startOrResumeThread(
	ctx context.Context,
	client *appServerClient,
	revision agentprofile.Revision,
	workspacePath string,
	externalSessionID string,
) (string, error) {
	params := appServerThreadParams(revision, workspacePath)
	method := "thread/start"
	if externalSessionID != "" {
		method = "thread/resume"
		params["threadId"] = externalSessionID
	} else {
		params["serviceName"] = "aitodos"
	}
	var response struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := client.request(ctx, 2, method, params, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.Thread.ID) == "" {
		return "", errors.New("app server did not return a thread id")
	}
	return response.Thread.ID, nil
}

func appServerThreadParams(revision agentprofile.Revision, workspacePath string) map[string]any {
	params := map[string]any{
		"cwd": workspacePath, "approvalPolicy": appServerApprovalPolicy(), "approvalsReviewer": "user",
		"sandbox": appServerSandboxMode(revision.WorkspacePolicy),
	}
	if revision.Model != "" {
		params["model"] = revision.Model
	}
	return params
}

func startAppServerTurn(
	ctx context.Context,
	client *appServerClient,
	revision agentprofile.Revision,
	workspacePath string,
	threadID string,
	prompt string,
) (string, error) {
	params := map[string]any{
		"threadId": threadID, "cwd": workspacePath,
		"input":          []map[string]string{{"type": "text", "text": prompt}},
		"approvalPolicy": appServerApprovalPolicy(), "approvalsReviewer": "user",
		"sandboxPolicy": appServerSandboxPolicy(revision.WorkspacePolicy, workspacePath),
	}
	if revision.Model != "" {
		params["model"] = revision.Model
	}
	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := client.request(ctx, 3, "turn/start", params, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.Turn.ID) == "" {
		return "", errors.New("app server did not return a turn id")
	}
	return response.Turn.ID, nil
}

func appServerApprovalPolicy() map[string]any {
	return map[string]any{"granular": map[string]bool{
		"mcp_elicitations": true, "request_permissions": true, "rules": true,
		"sandbox_approval": true, "skill_approval": true,
	}}
}

func appServerSandboxMode(policy agentprofile.WorkspacePolicy) string {
	if policy == agentprofile.WorkspaceWriteTask {
		return "workspace-write"
	}
	return "read-only"
}

func appServerSandboxPolicy(policy agentprofile.WorkspacePolicy, workspacePath string) map[string]any {
	if policy == agentprofile.WorkspaceWriteTask {
		return map[string]any{"type": "workspaceWrite", "networkAccess": false, "writableRoots": []string{workspacePath}}
	}
	return map[string]any{"type": "readOnly", "networkAccess": false}
}

func waitForAppServerTurn(
	ctx context.Context,
	client *appServerClient,
	approvals appServerApprovalBridge,
	purpose domainrun.Purpose,
	workspacePath string,
	threadID string,
	turnID string,
) (domainrun.Status, error) {
	for {
		message, err := client.next(ctx)
		if err != nil {
			return domainrun.StatusFailed, err
		}
		if isAppServerApprovalMethod(message.Method) {
			if err := handleAppServerApproval(ctx, client, approvals, message, threadID, turnID); err != nil {
				return domainrun.StatusFailed, err
			}
			continue
		}
		if message.Method != "turn/completed" {
			continue
		}
		status, finalMessage, err := parseCompletedTurn(message.Params, threadID, turnID)
		if err != nil {
			return domainrun.StatusFailed, err
		}
		if err := persistAppServerFinalResult(purpose, workspacePath, finalMessage); err != nil {
			return domainrun.StatusFailed, err
		}
		return status, nil
	}
}

func handleAppServerApproval(
	ctx context.Context,
	client *appServerClient,
	bridge appServerApprovalBridge,
	message appServerMessage,
	threadID string,
	turnID string,
) error {
	input, grant, err := mapCodexApprovalRequest(message.Method, message.ID, message.Params)
	if err != nil {
		return err
	}
	created, err := bridge.store.CreateApprovalRequest(ctx, bridge.runID, bridge.claimToken, bridge.leaseGeneration, input)
	if err != nil {
		return err
	}
	decision, err := bridge.waitDecision(ctx, client, created.ID)
	if err != nil {
		_ = bridge.store.ClearApprovalRequest(context.Background(), created.ID, bridge.claimToken, bridge.leaseGeneration)
		return err
	}
	response, interrupt, err := codexApprovalResponse(message.Method, decision, grant)
	if err != nil {
		return err
	}
	if err := client.writeRawResponse(message.ID, response); err != nil {
		return err
	}
	if interrupt {
		return client.write(map[string]any{
			"id": 4, "method": "turn/interrupt", "params": map[string]string{"threadId": threadID, "turnId": turnID},
		})
	}
	return nil
}

func (bridge appServerApprovalBridge) waitDecision(
	ctx context.Context,
	client *appServerClient,
	requestID string,
) (approvalrequest.Decision, error) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case err := <-client.readErr:
			return "", err
		case <-ticker.C:
			current, err := bridge.store.GetApprovalRequest(ctx, requestID)
			if err != nil {
				return "", err
			}
			if current.Status == approvalrequest.StatusResolved {
				return current.Decision, nil
			}
			if current.Status == approvalrequest.StatusCleared {
				return "", errors.New("approval request was cleared")
			}
		}
	}
}

func mapCodexApprovalRequest(
	method string,
	requestID json.RawMessage,
	params json.RawMessage,
) (approvalrequest.CreateInput, json.RawMessage, error) {
	if !json.Valid(requestID) || len(requestID) == 0 {
		return approvalrequest.CreateInput{}, nil, errors.New("approval request has invalid id")
	}
	var payload struct {
		ItemID         string          `json:"itemId"`
		Reason         string          `json:"reason"`
		Command        string          `json:"command"`
		CWD            string          `json:"cwd"`
		GrantRoot      string          `json:"grantRoot"`
		Permissions    json.RawMessage `json:"permissions"`
		NetworkContext *struct {
			Host     string `json:"host"`
			Protocol string `json:"protocol"`
		} `json:"networkApprovalContext"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return approvalrequest.CreateInput{}, nil, fmt.Errorf("decode approval params: %w", err)
	}
	kind := approvalrequest.KindCommand
	if method == "item/fileChange/requestApproval" {
		kind = approvalrequest.KindFileChange
	} else if method == "item/permissions/requestApproval" {
		kind = approvalrequest.KindPermissions
	} else if method != "item/commandExecution/requestApproval" {
		return approvalrequest.CreateInput{}, nil, fmt.Errorf("unsupported approval method %q", method)
	}
	input := approvalrequest.CreateInput{
		ExternalRequestID: string(requestID), ItemID: payload.ItemID, Kind: kind,
		Reason: payload.Reason, Command: payload.Command, CWD: payload.CWD, GrantRoot: payload.GrantRoot,
		Available: []approvalrequest.Decision{
			approvalrequest.DecisionAcceptOnce, approvalrequest.DecisionAcceptSession,
			approvalrequest.DecisionDecline, approvalrequest.DecisionCancelRun,
		},
	}
	if payload.NetworkContext != nil {
		input.Kind, input.Host, input.Protocol = approvalrequest.KindNetwork, payload.NetworkContext.Host, payload.NetworkContext.Protocol
	}
	if kind == approvalrequest.KindPermissions && len(payload.Permissions) > 0 {
		input.GrantRoot = boundedPermissionSummary(payload.Permissions)
	}
	return input, payload.Permissions, input.Validate()
}

func boundedPermissionSummary(permissions json.RawMessage) string {
	compact := &bytes.Buffer{}
	if json.Compact(compact, permissions) != nil {
		return ""
	}
	value := compact.String()
	if len(value) > 4000 {
		return value[:3997] + "..."
	}
	return value
}

func codexApprovalResponse(
	method string,
	decision approvalrequest.Decision,
	requestedPermissions json.RawMessage,
) (json.RawMessage, bool, error) {
	if method != "item/permissions/requestApproval" {
		value := map[approvalrequest.Decision]string{
			approvalrequest.DecisionAcceptOnce: "accept", approvalrequest.DecisionAcceptSession: "acceptForSession",
			approvalrequest.DecisionDecline: "decline", approvalrequest.DecisionCancelRun: "cancel",
		}[decision]
		if value == "" {
			return nil, false, errors.New("unsupported approval decision")
		}
		encoded, err := json.Marshal(map[string]string{"decision": value})
		return encoded, decision == approvalrequest.DecisionCancelRun, err
	}
	permissions := json.RawMessage(`{}`)
	scope := "turn"
	if decision == approvalrequest.DecisionAcceptOnce || decision == approvalrequest.DecisionAcceptSession {
		if !json.Valid(requestedPermissions) {
			return nil, false, errors.New("permission request has invalid grant")
		}
		permissions = requestedPermissions
	}
	if decision == approvalrequest.DecisionAcceptSession {
		scope = "session"
	}
	encoded, err := json.Marshal(struct {
		Permissions json.RawMessage `json:"permissions"`
		Scope       string          `json:"scope"`
	}{Permissions: permissions, Scope: scope})
	return encoded, decision == approvalrequest.DecisionCancelRun, err
}

func isAppServerApprovalMethod(method string) bool {
	return method == "item/commandExecution/requestApproval" ||
		method == "item/fileChange/requestApproval" || method == "item/permissions/requestApproval"
}

func parseCompletedTurn(params json.RawMessage, expectedThreadID, expectedTurnID string) (domainrun.Status, string, error) {
	var completed struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
			Items []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"items"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(params, &completed); err != nil {
		return domainrun.StatusFailed, "", fmt.Errorf("decode completed turn: %w", err)
	}
	if completed.ThreadID != expectedThreadID || completed.Turn.ID != expectedTurnID {
		return domainrun.StatusFailed, "", errors.New("completed turn identity mismatch")
	}
	final := ""
	for _, item := range completed.Turn.Items {
		if item.Type == "agentMessage" {
			final = item.Text
		}
	}
	switch completed.Turn.Status {
	case "completed":
		return domainrun.StatusSucceeded, final, nil
	case "interrupted":
		return domainrun.StatusCancelled, final, nil
	case "failed":
		message := "Codex turn failed"
		if completed.Turn.Error != nil && completed.Turn.Error.Message != "" {
			message = completed.Turn.Error.Message
		}
		return domainrun.StatusFailed, final, errors.New(message)
	default:
		return domainrun.StatusFailed, final, fmt.Errorf("unexpected completed turn status %q", completed.Turn.Status)
	}
}

func persistAppServerFinalResult(purpose domainrun.Purpose, workspacePath, finalMessage string) error {
	if strings.TrimSpace(finalMessage) == "" {
		return nil
	}
	path := filepath.Join(workspacePath, resultFileName)
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	content := []byte(strings.TrimSpace(finalMessage))
	if !json.Valid(content) {
		if purpose == domainrun.PurposeTriage || purpose == domainrun.PurposePlanning {
			return errors.New("structured final response is not valid JSON")
		}
		return nil
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("write app server result: %w", err)
	}
	return nil
}

func appServerFailure(code string, err error) processResult {
	return processResult{Status: domainrun.StatusFailed, ExitCode: -1, Code: code, Err: err}
}

func (client *appServerClient) request(ctx context.Context, id int, method string, params any, result any) error {
	if err := client.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	expected := fmt.Sprintf("%d", id)
	for {
		message, err := client.next(ctx)
		if err != nil {
			return err
		}
		if string(message.ID) != expected {
			continue
		}
		if message.Error != nil {
			return fmt.Errorf("app server %s failed (%d): %s", method, message.Error.Code, message.Error.Message)
		}
		if err := json.Unmarshal(message.Result, result); err != nil {
			return fmt.Errorf("decode app server %s response: %w", method, err)
		}
		return nil
	}
}

func (client *appServerClient) next(ctx context.Context) (appServerMessage, error) {
	select {
	case <-ctx.Done():
		return appServerMessage{}, ctx.Err()
	case err := <-client.readErr:
		return appServerMessage{}, err
	case message := <-client.messages:
		return message, nil
	}
}

func (client *appServerClient) write(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if _, err := client.stdin.Write(encoded); err != nil {
		return fmt.Errorf("write app server message: %w", err)
	}
	return nil
}

func (client *appServerClient) writeRawResponse(id json.RawMessage, result json.RawMessage) error {
	if !json.Valid(id) || !json.Valid(result) {
		return errors.New("invalid app server response payload")
	}
	message := append([]byte(`{"id":`), id...)
	message = append(message, []byte(`,"result":`)...)
	message = append(message, result...)
	message = append(message, '}', '\n')
	if _, err := client.stdin.Write(message); err != nil {
		return fmt.Errorf("write app server approval response: %w", err)
	}
	return nil
}

func closeAppServer(client *appServerClient) error {
	_ = client.stdin.Close()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case err := <-client.wait:
		return err
	case <-timer.C:
		_ = syscall.Kill(-client.command.Process.Pid, syscall.SIGTERM)
		killTimer := time.NewTimer(2 * time.Second)
		defer killTimer.Stop()
		select {
		case <-client.wait:
			return nil
		case <-killTimer.C:
			_ = syscall.Kill(-client.command.Process.Pid, syscall.SIGKILL)
			<-client.wait
			return nil
		}
	}
}
