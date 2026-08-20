package storage

import (
	"context"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/capability"
)

func TestCapabilityStoreCreatesProjectCatalog(t *testing.T) {
	ctx := context.Background()
	store := NewCapabilityStore(openTaskTestDatabase(t))
	skill, err := store.CreateSkill(ctx, capability.SkillInput{
		Name: "发布检查", SourcePath: ".agents/skills/release",
	}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	server, err := store.CreateMCPServer(ctx, capability.MCPServerInput{
		Name: "浏览器", ConfigName: "playwright",
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Skills) != 1 || catalog.Skills[0].ID != skill.ID ||
		len(catalog.MCPServers) != 1 || catalog.MCPServers[0].ID != server.ID {
		t.Fatalf("catalog = %#v", catalog)
	}
}

func TestCapabilityStoreRejectsDuplicateSources(t *testing.T) {
	ctx := context.Background()
	store := NewCapabilityStore(openTaskTestDatabase(t))
	input := capability.MCPServerInput{Name: "浏览器", ConfigName: "playwright"}
	if _, err := store.CreateMCPServer(ctx, input); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateMCPServer(ctx, input); err == nil {
		t.Fatal("CreateMCPServer() should reject a duplicate config name")
	}
}

func TestCapabilityStoreRefreshesSkillWithOptimisticVersion(t *testing.T) {
	ctx := context.Background()
	store := NewCapabilityStore(openTaskTestDatabase(t))
	created, err := store.CreateSkill(ctx, capability.SkillInput{
		Name: "发布检查", SourcePath: ".agents/skills/release",
	}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := store.RefreshSkill(ctx, created.ID, created.Version,
		"abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Version != 2 || refreshed.ContentSHA256 == created.ContentSHA256 {
		t.Fatalf("refreshed = %#v", refreshed)
	}
	if _, err := store.RefreshSkill(ctx, created.ID, created.Version, created.ContentSHA256); err == nil {
		t.Fatal("RefreshSkill() should reject a stale version")
	}
}
