package agentprofile

import "testing"

func TestRevisionInputValidate(t *testing.T) {
	valid := RevisionInput{
		Instructions: "实现当前 Task 并提供测试证据",
		Adapter:      "generic", Command: "codex", Args: []string{"exec"}, Model: "gpt-5",
		MaxInputTokens: 32000, ReservedOutputTokens: 8000,
		RecentMessageLimit: 20, RetrievalLimit: 8, TimeoutSeconds: 1800,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*RevisionInput)
	}{
		{name: "instructions", mutate: func(input *RevisionInput) { input.Instructions = "" }},
		{name: "command", mutate: func(input *RevisionInput) { input.Command = "" }},
		{name: "input budget", mutate: func(input *RevisionInput) { input.MaxInputTokens = 1024 }},
		{name: "output reserve", mutate: func(input *RevisionInput) { input.ReservedOutputTokens = input.MaxInputTokens }},
		{name: "recent messages", mutate: func(input *RevisionInput) { input.RecentMessageLimit = 201 }},
		{name: "retrieval", mutate: func(input *RevisionInput) { input.RetrievalLimit = -1 }},
		{name: "timeout", mutate: func(input *RevisionInput) { input.TimeoutSeconds = 10 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			if err := input.Validate(); err == nil {
				t.Fatal("Validate() error = nil")
			}
		})
	}
}

func TestCodexRevisionRequiresExecSubcommand(t *testing.T) {
	input := RevisionInput{
		Instructions: "实现当前 Task", Adapter: "codex", Command: "codex", Args: []string{"--json"},
		MaxInputTokens: 32000, ReservedOutputTokens: 8000,
		RecentMessageLimit: 20, RetrievalLimit: 8, TimeoutSeconds: 1800,
	}
	if err := input.Validate(); err == nil {
		t.Fatal("Validate() should require codex exec as the first argument")
	}
	input.Args = []string{"exec", "--json"}
	if err := input.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestCodexRevisionRejectsConflictingApprovalAndSandboxFlags(t *testing.T) {
	input := RevisionInput{
		Instructions: "实现当前 Task", Adapter: "codex", Command: "codex",
		Args:           []string{"exec", "--sandbox", "workspace-write", "--approve-for-me"},
		MaxInputTokens: 32000, ReservedOutputTokens: 8000,
		RecentMessageLimit: 20, RetrievalLimit: 8, TimeoutSeconds: 1800,
	}
	if err := input.Validate(); err == nil {
		t.Fatal("Validate() should reject --sandbox combined with --approve-for-me")
	}
}

func TestRolePoliciesCannotBeConfiguredByPrompt(t *testing.T) {
	tests := []struct {
		role      Role
		workspace WorkspacePolicy
		approval  ApprovalPolicy
	}{
		{RolePlanner, WorkspaceNone, ApprovalReadOnly},
		{RoleTriager, WorkspaceNone, ApprovalReadOnly},
		{RoleImplementer, WorkspaceWriteTask, ApprovalWorkspaceWrite},
		{RoleRevision, WorkspaceWriteTask, ApprovalWorkspaceWrite},
		{RoleReviewer, WorkspaceReadOnly, ApprovalReadOnly},
	}
	for _, test := range tests {
		workspace, approval, err := PoliciesForRole(test.role)
		if err != nil {
			t.Fatalf("PoliciesForRole(%q) error = %v", test.role, err)
		}
		if workspace != test.workspace || approval != test.approval {
			t.Fatalf("PoliciesForRole(%q) = %q, %q", test.role, workspace, approval)
		}
	}
	if _, _, err := PoliciesForRole("UNKNOWN"); err == nil {
		t.Fatal("PoliciesForRole(UNKNOWN) error = nil")
	}
}
