// Package capabilitycatalog 校验并登记当前项目允许使用的 Skill 与 Codex MCP 引用。
package capabilitycatalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/light-speak/aitodos/internal/domain/capability"
	"github.com/light-speak/aitodos/internal/storage"
)

const maxSkillBytes = 1 << 20

// Service 负责文件系统和 Codex CLI 校验，Store 只持久化脱敏元数据。
type Service struct {
	projectRoot  string
	codexCommand string
	store        *storage.CapabilityStore
}

// New 创建当前项目的能力目录服务。
func New(projectRoot, codexCommand string, store *storage.CapabilityStore) *Service {
	return &Service{projectRoot: projectRoot, codexCommand: codexCommand, store: store}
}

// List 返回当前项目能力目录。
func (service *Service) List(ctx context.Context) (capability.Catalog, error) {
	return service.store.List(ctx)
}

// AddSkill 校验 SKILL.md、计算哈希并登记来源路径。
func (service *Service) AddSkill(ctx context.Context, input capability.SkillInput) (capability.Skill, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return capability.Skill{}, err
	}
	path, err := resolveSkillDirectory(service.projectRoot, input.SourcePath)
	if err != nil {
		return capability.Skill{}, err
	}
	_, hash, err := readSkillFile(filepath.Join(path, "SKILL.md"))
	if err != nil {
		return capability.Skill{}, err
	}
	return service.store.CreateSkill(ctx, input, hash)
}

// RefreshSkill 重新读取已登记路径并以乐观版本更新内容哈希。
func (service *Service) RefreshSkill(ctx context.Context, skillID string, version int64) (capability.Skill, error) {
	current, err := service.store.GetSkill(ctx, strings.TrimSpace(skillID))
	if err != nil {
		return capability.Skill{}, err
	}
	_, hash, err := ReadSkillContent(service.projectRoot, current.SourcePath)
	if err != nil {
		return capability.Skill{}, err
	}
	return service.store.RefreshSkill(ctx, current.ID, version, hash)
}

// ReadSkillContent 按项目相对路径或显式绝对路径读取受限 Skill 内容与哈希。
func ReadSkillContent(projectRoot, sourcePath string) ([]byte, string, error) {
	path, err := resolveSkillDirectory(projectRoot, sourcePath)
	if err != nil {
		return nil, "", err
	}
	return readSkillFile(filepath.Join(path, "SKILL.md"))
}

// AddMCPServer 确认本机 Codex 已登记配置名后保存引用，不读取或保存连接 Secret。
func (service *Service) AddMCPServer(ctx context.Context, input capability.MCPServerInput) (capability.MCPServer, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return capability.MCPServer{}, err
	}
	if err := service.probeCodexMCP(ctx, input.ConfigName); err != nil {
		return capability.MCPServer{}, err
	}
	return service.store.CreateMCPServer(ctx, input)
}

func (service *Service) probeCodexMCP(ctx context.Context, configName string) error {
	if strings.TrimSpace(service.codexCommand) == "" {
		return errors.New("未配置 Codex 命令，无法校验 MCP")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, service.codexCommand, "mcp", "get", configName, "--json")
	command.Dir = service.projectRoot
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("校验 Codex MCP %q 超时", configName)
		}
		return fmt.Errorf("本机 Codex 未找到可用的 MCP 配置 %q", configName)
	}
	return nil
}

func resolveSkillDirectory(projectRoot, sourcePath string) (string, error) {
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	requested := sourcePath
	if !filepath.IsAbs(requested) {
		requested = filepath.Join(root, requested)
	}
	resolved, err := filepath.EvalSymlinks(requested)
	if err != nil {
		return "", fmt.Errorf("读取 Skill 路径失败: %w", err)
	}
	if !filepath.IsAbs(sourcePath) {
		resolvedRoot, rootErr := filepath.EvalSymlinks(root)
		if rootErr != nil {
			return "", fmt.Errorf("读取项目路径失败: %w", rootErr)
		}
		relative, relErr := filepath.Rel(resolvedRoot, resolved)
		if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", errors.New("Skill 相对路径解析后离开了项目目录")
		}
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("读取 Skill 目录失败: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("Skill 路径必须指向包含 SKILL.md 的目录")
	}
	return resolved, nil
}

func readSkillFile(path string) ([]byte, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("读取 SKILL.md 失败: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, "", fmt.Errorf("读取 SKILL.md 元数据失败: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxSkillBytes {
		return nil, "", errors.New("SKILL.md 必须是普通文件且不能超过 1 MiB")
	}
	content, err := io.ReadAll(io.LimitReader(file, maxSkillBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("读取 SKILL.md 内容失败: %w", err)
	}
	hash := sha256.Sum256(content)
	return content, hex.EncodeToString(hash[:]), nil
}
