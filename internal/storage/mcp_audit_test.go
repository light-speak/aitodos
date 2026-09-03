package storage

import (
	"context"
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
