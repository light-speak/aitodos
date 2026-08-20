package capability

import "testing"

func TestSkillInputNormalizeAndValidate(t *testing.T) {
	input := SkillInput{Name: " 项目发布 ", SourcePath: " .agents/skills/release "}.Normalized()
	if input.Name != "项目发布" || input.SourcePath != ".agents/skills/release" {
		t.Fatalf("normalized = %#v", input)
	}
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSkillInputRejectsRelativePathEscape(t *testing.T) {
	if err := (SkillInput{Name: "越界", SourcePath: "../shared-skill"}).Validate(); err == nil {
		t.Fatal("Validate() should reject a relative path escape")
	}
}

func TestMCPServerInputValidatesCodexConfigName(t *testing.T) {
	if err := (MCPServerInput{Name: "浏览器", ConfigName: "playwright"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (MCPServerInput{Name: "浏览器", ConfigName: "bad\nname"}).Validate(); err == nil {
		t.Fatal("Validate() should reject control characters")
	}
	for _, configName := range []string{"server.name", "server name", "server/name"} {
		if err := (MCPServerInput{Name: "无效", ConfigName: configName}).Validate(); err == nil {
			t.Fatalf("Validate() should reject %q", configName)
		}
	}
}

func TestToolPolicyInputRejectsDuplicatesAndBlankTools(t *testing.T) {
	duplicate := ToolPolicyInput{Skills: []SkillBindingInput{{SkillID: "skill-1"}, {SkillID: "skill-1"}}}
	if err := duplicate.Validate(); err == nil {
		t.Fatal("Validate() should reject duplicate skills")
	}
	blankTool := ToolPolicyInput{MCPServers: []MCPBindingInput{{ServerID: "mcp-1", EnabledTools: []string{"search", " "}}}}
	if err := blankTool.Validate(); err == nil {
		t.Fatal("Validate() should reject blank tool names")
	}
}
