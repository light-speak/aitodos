package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/mcpaudit"
	"github.com/light-speak/aitodos/internal/domain/topic"
)

func TestMCPAuditStoreAppendsSanitizedLifecycle(t *testing.T) {
	store := NewMCPAuditStore(openTaskTestDatabase(t))
	ctx := context.Background()
	started, err := store.Append(ctx, MCPAuditInput{
		CallID: "call-1", ClientName: "codex", ToolName: "search_items",
		Phase: mcpaudit.PhaseStarted, ArgumentKeys: []string{"query", "token"},
		ArgumentsSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if err != nil {
		t.Fatal(err)
	}
	resultBytes := int64(42)
	if _, err := store.Append(ctx, MCPAuditInput{
		CallID: "call-1", ClientName: "codex", ToolName: "search_items",
		Phase: mcpaudit.PhaseCompleted, ArgumentKeys: []string{"query", "token"},
		ArgumentsSHA256: started.ArgumentsSHA256, ResultBytes: &resultBytes,
	}); err != nil {
		t.Fatal(err)
	}
	items, err := store.List(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Phase != mcpaudit.PhaseStarted || items[1].ResultBytes == nil || *items[1].ResultBytes != 42 {
		t.Fatalf("events = %#v", items)
	}
	for _, item := range items {
		if item.ArgumentsSHA256 == "" || item.ErrorMessage == "secret" {
			t.Fatalf("unsanitized event = %#v", item)
		}
	}
}

func TestMCPAuditStoreRejectsInvalidInput(t *testing.T) {
	store := NewMCPAuditStore(openTaskTestDatabase(t))
	if _, err := store.Append(context.Background(), MCPAuditInput{CallID: "call"}); err == nil {
		t.Fatal("Append() error = nil")
	}
	if _, err := store.List(context.Background(), 0); err == nil {
		t.Fatal("List() error = nil")
	}
}

func TestMCPAuditStoreBoundsUntrustedMetadata(t *testing.T) {
	store := NewMCPAuditStore(openTaskTestDatabase(t))
	keys := make([]string, 0, 120)
	for index := 0; index < 120; index++ {
		keys = append(keys, fmt.Sprintf("key-%03d-%s", index, strings.Repeat("x", 250)))
	}
	created, err := store.Append(context.Background(), MCPAuditInput{
		CallID: "large-call", ClientName: "codex", ToolName: "tool", Phase: mcpaudit.PhaseFailed,
		ArgumentKeys: keys, ArgumentsSHA256: strings.Repeat("a", 64), ErrorMessage: strings.Repeat("错", 2500),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.ArgumentKeys) != 100 || len([]rune(created.ArgumentKeys[0])) > 200 || len([]rune(created.ErrorMessage)) != 2000 {
		t.Fatalf("bounded audit event = %#v", created)
	}
}

func TestMCPAuditStoreTracksOutboundRunCallsAndBrowserLease(t *testing.T) {
	database := openTaskTestDatabase(t)
	ctx := context.Background()
	configureProfile(t, database, agentprofile.RolePlanner)
	_, err := NewTopicStore(database).Create(ctx, topic.CreateInput{Title: "审计浏览器调用"})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := NewRunStore(database).ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	store := NewMCPAuditStore(database)
	input := MCPAuditInput{
		CallID: "tool-1", Direction: "OUTBOUND", RunID: claim.Run.ID,
		ClientName: "codex-app-server", ServerName: "playwright", ToolName: "browser_navigate",
		Phase: mcpaudit.PhaseStarted, ArgumentKeys: []string{"url"},
		ArgumentsSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	if _, err := store.Append(ctx, input); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OpenResourceLease(ctx, claim.Run.ID, "BROWSER_SESSION", "playwright"); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListRun(ctx, claim.Run.ID, 10)
	if err != nil || len(items) != 1 || items[0].Direction != "OUTBOUND" || items[0].ServerName != "playwright" {
		t.Fatalf("events = %#v, %v", items, err)
	}
	if err := store.ReleaseRunResources(ctx, claim.Run.ID, false, "agent process exited"); err != nil {
		t.Fatal(err)
	}
	leases, err := store.ListRunResourceLeases(ctx, claim.Run.ID)
	if err != nil || len(leases) != 1 || leases[0].State != "RELEASED" || leases[0].ReleasedAt == nil {
		t.Fatalf("leases = %#v, %v", leases, err)
	}
}

func TestMCPAuditStoreRejectsInvalidQueriesAndResourceLeases(t *testing.T) {
	store := NewMCPAuditStore(openTaskTestDatabase(t))
	ctx := context.Background()
	for _, limit := range []int{-1, 0, 501} {
		if _, err := store.List(ctx, limit); err == nil {
			t.Fatalf("List(limit=%d) unexpectedly succeeded", limit)
		}
	}
	for _, query := range []struct {
		runID string
		limit int
	}{{"", 10}, {"run", 0}, {"run", 501}} {
		if _, err := store.ListRun(ctx, query.runID, query.limit); err == nil {
			t.Fatalf("ListRun(%q, %d) unexpectedly succeeded", query.runID, query.limit)
		}
	}
	for _, input := range []struct{ runID, kind, provider string }{
		{"", "BROWSER_SESSION", "playwright"},
		{"run", "OTHER", "playwright"},
		{"run", "BROWSER_SESSION", ""},
		{"run", "BROWSER_SESSION", strings.Repeat("p", 201)},
	} {
		if _, err := store.OpenResourceLease(ctx, input.runID, input.kind, input.provider); err == nil {
			t.Fatalf("OpenResourceLease(%#v) unexpectedly succeeded", input)
		}
	}
	if err := store.ReleaseRunResources(ctx, "", false, "reason"); err == nil {
		t.Fatal("ReleaseRunResources() accepted an empty run")
	}
	if err := store.ReleaseRunResources(ctx, "run", false, strings.Repeat("r", 1001)); err == nil {
		t.Fatal("ReleaseRunResources() accepted an oversized reason")
	}

	invalidAudit := MCPAuditInput{
		CallID: "call", ClientName: "client", ToolName: "tool", Phase: mcpaudit.PhaseStarted,
		ArgumentsSHA256: strings.Repeat("a", 64),
	}
	for _, mutate := range []func(*MCPAuditInput){
		func(input *MCPAuditInput) { input.Direction = "SIDEWAYS" },
		func(input *MCPAuditInput) { input.Direction, input.RunID, input.ServerName = "OUTBOUND", "", "server" },
		func(input *MCPAuditInput) { input.Direction, input.RunID, input.ServerName = "OUTBOUND", "run", "" },
		func(input *MCPAuditInput) { input.Phase = "UNKNOWN" },
	} {
		input := invalidAudit
		mutate(&input)
		if _, err := store.Append(ctx, input); err == nil {
			t.Fatalf("Append(%#v) unexpectedly succeeded", input)
		}
	}
}

func TestMCPAuditStoreReopensAndAbandonsBrowserLease(t *testing.T) {
	database := openTaskTestDatabase(t)
	ctx := context.Background()
	configureProfile(t, database, agentprofile.RolePlanner)
	if _, err := NewTopicStore(database).Create(ctx, topic.CreateInput{Title: "遗留浏览器"}); err != nil {
		t.Fatal(err)
	}
	claim, err := NewRunStore(database).ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	store := NewMCPAuditStore(database)
	first, err := store.OpenResourceLease(ctx, claim.Run.ID, "BROWSER_SESSION", "playwright")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.OpenResourceLease(ctx, claim.Run.ID, "BROWSER_SESSION", "playwright")
	if err != nil || second.ID != first.ID || second.State != "ACTIVE" {
		t.Fatalf("reopened lease = %#v, %v", second, err)
	}
	if err := store.ReleaseRunResources(ctx, claim.Run.ID, true, "进程异常退出"); err != nil {
		t.Fatal(err)
	}
	leasses, err := store.ListRunResourceLeases(ctx, claim.Run.ID)
	if err != nil || len(leasses) != 1 || leasses[0].State != "ABANDONED" || leasses[0].CleanupReason == "" {
		t.Fatalf("abandoned leases = %#v, %v", leasses, err)
	}
}

func TestMCPAuditStoreReportsCorruptRowsAndClosedDatabase(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	store := NewMCPAuditStore(database)
	if _, err := database.ExecContext(ctx, `INSERT INTO mcp_call_events(
id, call_id, client_name, tool_name, phase, argument_keys_json, arguments_sha256,
error_message, occurred_at, direction, server_name
) VALUES ('corrupt-audit', 'call', 'client', 'tool', 'STARTED', '{}', ?, '', ?, 'INBOUND', '')`,
		strings.Repeat("a", 64), formatTime(time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(ctx, 10); err == nil {
		t.Fatal("List() accepted non-array argument keys")
	}
	if _, err := database.ExecContext(ctx, `UPDATE mcp_call_events SET argument_keys_json = '[]', occurred_at = 'invalid' WHERE id = 'corrupt-audit'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(ctx, 10); err == nil {
		t.Fatal("List() accepted invalid occurrence time")
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	closed := NewMCPAuditStore(database)
	valid := MCPAuditInput{
		CallID: "call", ClientName: "client", ToolName: "tool", Phase: mcpaudit.PhaseStarted,
		ArgumentsSHA256: strings.Repeat("a", 64),
	}
	calls := []func() error{
		func() error { _, err := closed.Append(ctx, valid); return err },
		func() error { _, err := closed.List(ctx, 10); return err },
		func() error { _, err := closed.ListRun(ctx, "run", 10); return err },
		func() error {
			_, err := closed.OpenResourceLease(ctx, "run", "BROWSER_SESSION", "playwright")
			return err
		},
		func() error { return closed.ReleaseRunResources(ctx, "run", false, "closed") },
		func() error { _, err := closed.ListRunResourceLeases(ctx, "run"); return err },
	}
	for index, call := range calls {
		if err := call(); err == nil || errors.Is(err, context.Canceled) {
			t.Fatalf("closed call %d error = %v", index, err)
		}
	}
}
