package storage

import (
	"context"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/capability"
)

func TestAgentProfileStoreListsRoleDefaults(t *testing.T) {
	store := NewAgentProfileStore(openTaskTestDatabase(t))
	profiles, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantRoles := []agentprofile.Role{
		agentprofile.RolePlanner,
		agentprofile.RoleTriager,
		agentprofile.RoleImplementer,
		agentprofile.RoleRevision,
		agentprofile.RoleReviewer,
	}
	if len(profiles) != len(wantRoles) {
		t.Fatalf("profiles count = %d, want %d", len(profiles), len(wantRoles))
	}
	for index, wantRole := range wantRoles {
		profile := profiles[index]
		if profile.Role != wantRole || profile.CurrentRevision.Revision != 1 {
			t.Fatalf("profiles[%d] = %#v", index, profile)
		}
		workspace, approval, policyErr := agentprofile.PoliciesForRole(wantRole)
		if policyErr != nil {
			t.Fatal(policyErr)
		}
		if profile.CurrentRevision.WorkspacePolicy != workspace || profile.CurrentRevision.ApprovalPolicy != approval {
			t.Fatalf("profile policy = %#v", profile.CurrentRevision)
		}
	}
}

func TestAgentProfileStoreCreatesImmutableRevision(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	store := NewAgentProfileStore(database)
	capabilities := NewCapabilityStore(database)
	skill, err := capabilities.CreateSkill(ctx, capability.SkillInput{
		Name: "实现规范", SourcePath: ".agents/skills/implementation",
	}, "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	if err != nil {
		t.Fatal(err)
	}
	mcpServer, err := capabilities.CreateMCPServer(ctx, capability.MCPServerInput{
		Name: "代码搜索", ConfigName: "code-search",
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.GetByRole(ctx, agentprofile.RoleImplementer)
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateRevision(ctx, profile.ID, agentprofile.RevisionInput{
		Instructions: "只实现当前 Task，并报告实际测试证据",
		Adapter:      "codex", Command: "codex", Args: []string{"exec", "--json"}, Model: "gpt-5",
		MaxInputTokens: 64000, ReservedOutputTokens: 12000,
		RecentMessageLimit: 30, RetrievalLimit: 10, TimeoutSeconds: 3600,
		ToolPolicy: capability.ToolPolicyInput{
			Skills: []capability.SkillBindingInput{{SkillID: skill.ID, Required: true}},
			MCPServers: []capability.MCPBindingInput{{
				ServerID: mcpServer.ID, Required: true, EnabledTools: []string{"search"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.CurrentRevision.Revision != 2 || created.CurrentRevision.Command != "codex" {
		t.Fatalf("created profile = %#v", created)
	}
	if created.CurrentRevision.WorkspacePolicy != agentprofile.WorkspaceWriteTask {
		t.Fatalf("workspace policy = %q", created.CurrentRevision.WorkspacePolicy)
	}
	if len(created.CurrentRevision.ToolPolicy.Skills) != 1 ||
		len(created.CurrentRevision.ToolPolicy.MCPServers) != 1 ||
		created.CurrentRevision.ToolPolicy.MCPServers[0].EnabledTools[0] != "search" {
		t.Fatalf("tool policy = %#v", created.CurrentRevision.ToolPolicy)
	}

	history, err := store.ListRevisions(ctx, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Revision != 2 || history[1].Revision != 1 {
		t.Fatalf("revision history = %#v", history)
	}
	if history[1].Instructions != profile.CurrentRevision.Instructions {
		t.Fatal("creating revision changed revision 1")
	}
}
