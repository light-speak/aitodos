// Package capability 定义项目 Skill/MCP 目录与不可变 Agent Tool Policy。
package capability

import (
	"errors"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Skill 是项目允许 Agent 加载的版本化指令来源。
type Skill struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	SourcePath    string    `json:"source_path"`
	ContentSHA256 string    `json:"content_sha256"`
	Enabled       bool      `json:"enabled"`
	Version       int64     `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// MCPServer 是项目引用的本机 Codex MCP 配置。
type MCPServer struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	ConfigName string    `json:"config_name"`
	Enabled    bool      `json:"enabled"`
	Version    int64     `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Catalog 是当前项目可供 Profile 选择的能力目录。
type Catalog struct {
	Skills     []Skill     `json:"skills"`
	MCPServers []MCPServer `json:"mcp_servers"`
}

// SkillInput 是创建或更新 Skill 引用的输入。
type SkillInput struct {
	Name       string `json:"name"`
	SourcePath string `json:"source_path"`
}

// Normalized 清理 Skill 输入。
func (input SkillInput) Normalized() SkillInput {
	input.Name = strings.TrimSpace(input.Name)
	input.SourcePath = strings.TrimSpace(input.SourcePath)
	return input
}

// Validate 校验 Skill 名称与安全路径形式。
func (input SkillInput) Validate() error {
	input = input.Normalized()
	if utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 100 {
		return errors.New("Skill 名称长度必须为 1 到 100")
	}
	if input.SourcePath == "" || len(input.SourcePath) > 4096 {
		return errors.New("Skill 路径不能为空且最长 4096 个字符")
	}
	cleaned := filepath.Clean(input.SourcePath)
	if !filepath.IsAbs(cleaned) && (cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator))) {
		return errors.New("Skill 相对路径不得离开项目目录")
	}
	return nil
}

// MCPServerInput 是创建或更新本机 Codex MCP 引用的输入。
type MCPServerInput struct {
	Name       string `json:"name"`
	ConfigName string `json:"config_name"`
}

// Normalized 清理 MCP 输入。
func (input MCPServerInput) Normalized() MCPServerInput {
	input.Name = strings.TrimSpace(input.Name)
	input.ConfigName = strings.TrimSpace(input.ConfigName)
	return input
}

// Validate 校验 MCP 显示名和 Codex 配置名。
func (input MCPServerInput) Validate() error {
	input = input.Normalized()
	if utf8.RuneCountInString(input.Name) < 1 || utf8.RuneCountInString(input.Name) > 100 {
		return errors.New("MCP 名称长度必须为 1 到 100")
	}
	if utf8.RuneCountInString(input.ConfigName) < 1 || utf8.RuneCountInString(input.ConfigName) > 200 {
		return errors.New("Codex MCP 配置名长度必须为 1 到 200")
	}
	if !isCodexConfigName(input.ConfigName) {
		return errors.New("Codex MCP 配置名只能包含 ASCII 字母、数字、下划线和连字符")
	}
	return nil
}

func isCodexConfigName(value string) bool {
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return value != ""
}

// SkillBindingInput 描述 Profile 对一个 Skill 的采用策略。
type SkillBindingInput struct {
	SkillID  string `json:"skill_id"`
	Required bool   `json:"required"`
}

// MCPBindingInput 描述 Profile 对一个 MCP Server 与 Tool 的允许范围。
type MCPBindingInput struct {
	ServerID     string   `json:"server_id"`
	Required     bool     `json:"required"`
	EnabledTools []string `json:"enabled_tools"`
}

// ToolPolicyInput 是 Profile Revision 保存的能力选择。
type ToolPolicyInput struct {
	Skills     []SkillBindingInput `json:"skills"`
	MCPServers []MCPBindingInput   `json:"mcp_servers"`
}

// Normalized 清理能力 ID 与 Tool 名称。
func (input ToolPolicyInput) Normalized() ToolPolicyInput {
	for index := range input.Skills {
		input.Skills[index].SkillID = strings.TrimSpace(input.Skills[index].SkillID)
	}
	for index := range input.MCPServers {
		input.MCPServers[index].ServerID = strings.TrimSpace(input.MCPServers[index].ServerID)
		for toolIndex := range input.MCPServers[index].EnabledTools {
			input.MCPServers[index].EnabledTools[toolIndex] = strings.TrimSpace(input.MCPServers[index].EnabledTools[toolIndex])
		}
	}
	return input
}

// Validate 校验 Profile 能力选择不含空值或重复项。
func (input ToolPolicyInput) Validate() error {
	input = input.Normalized()
	if len(input.Skills) > 100 || len(input.MCPServers) > 100 {
		return errors.New("每个 Profile 最多选择 100 个 Skill 和 100 个 MCP")
	}
	seenSkills := make(map[string]struct{}, len(input.Skills))
	for _, binding := range input.Skills {
		if binding.SkillID == "" {
			return errors.New("skill_id 不能为空")
		}
		if _, exists := seenSkills[binding.SkillID]; exists {
			return errors.New("Skill 不能重复选择")
		}
		seenSkills[binding.SkillID] = struct{}{}
	}
	seenServers := make(map[string]struct{}, len(input.MCPServers))
	for _, binding := range input.MCPServers {
		if binding.ServerID == "" {
			return errors.New("server_id 不能为空")
		}
		if _, exists := seenServers[binding.ServerID]; exists {
			return errors.New("MCP Server 不能重复选择")
		}
		seenServers[binding.ServerID] = struct{}{}
		seenTools := make(map[string]struct{}, len(binding.EnabledTools))
		for _, tool := range binding.EnabledTools {
			if tool == "" {
				return errors.New("MCP Tool 名称不能为空")
			}
			if _, exists := seenTools[tool]; exists {
				return errors.New("MCP Tool 不能重复选择")
			}
			seenTools[tool] = struct{}{}
		}
	}
	return nil
}

// SkillBinding 是 Revision 中解析后的 Skill 引用。
type SkillBinding struct {
	SkillID  string `json:"skill_id"`
	Required bool   `json:"required"`
}

// MCPBinding 是 Revision 中解析后的 MCP 引用。
type MCPBinding struct {
	ServerID     string   `json:"server_id"`
	Required     bool     `json:"required"`
	EnabledTools []string `json:"enabled_tools"`
}

// ToolPolicy 是 Agent Profile Revision 的不可变能力选择。
type ToolPolicy struct {
	Skills     []SkillBinding `json:"skills"`
	MCPServers []MCPBinding   `json:"mcp_servers"`
}

// SkillSnapshot 是 Run 创建时解析并固化的 Skill 版本。
type SkillSnapshot struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SourcePath     string `json:"source_path"`
	ContentSHA256  string `json:"content_sha256"`
	CatalogVersion int64  `json:"catalog_version"`
	Enabled        bool   `json:"enabled"`
	Required       bool   `json:"required"`
}

// MCPServerSnapshot 是 Run 创建时解析并固化的 MCP Tool 范围。
type MCPServerSnapshot struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	ConfigName     string   `json:"config_name"`
	CatalogVersion int64    `json:"catalog_version"`
	Enabled        bool     `json:"enabled"`
	Required       bool     `json:"required"`
	EnabledTools   []string `json:"enabled_tools"`
}

// ToolPolicySnapshot 是 Run 事实的一部分，创建后不可修改。
type ToolPolicySnapshot struct {
	ProfileRevisionID string              `json:"profile_revision_id"`
	Skills            []SkillSnapshot     `json:"skills"`
	MCPServers        []MCPServerSnapshot `json:"mcp_servers"`
}
