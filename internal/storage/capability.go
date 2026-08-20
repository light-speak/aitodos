package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/light-speak/aitodos/internal/domain/capability"
)

var (
	// ErrCapabilityNotFound 表示项目能力不存在。
	ErrCapabilityNotFound = errors.New("capability not found")
	// ErrCapabilityConflict 表示能力来源已登记或版本冲突。
	ErrCapabilityConflict = errors.New("capability conflict")
)

// CapabilityStore 持久化项目 Skill 与本机 MCP 引用目录。
type CapabilityStore struct {
	database *sql.DB
}

// NewCapabilityStore 创建项目能力目录存储。
func NewCapabilityStore(database *sql.DB) *CapabilityStore {
	return &CapabilityStore{database: database}
}

// List 返回项目当前能力目录。
func (store *CapabilityStore) List(ctx context.Context) (capability.Catalog, error) {
	skills, err := store.listSkills(ctx)
	if err != nil {
		return capability.Catalog{}, err
	}
	servers, err := store.listMCPServers(ctx)
	if err != nil {
		return capability.Catalog{}, err
	}
	return capability.Catalog{Skills: skills, MCPServers: servers}, nil
}

// CreateSkill 登记已校验并计算哈希的 Skill 路径。
func (store *CapabilityStore) CreateSkill(
	ctx context.Context,
	input capability.SkillInput,
	contentSHA256 string,
) (capability.Skill, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return capability.Skill{}, err
	}
	if len(contentSHA256) != 64 {
		return capability.Skill{}, errors.New("Skill 内容哈希必须是 SHA-256")
	}
	id, err := newID()
	if err != nil {
		return capability.Skill{}, err
	}
	now := time.Now().UTC()
	created := capability.Skill{
		ID: id, Name: input.Name, SourcePath: input.SourcePath, ContentSHA256: contentSHA256,
		Enabled: true, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	_, err = store.database.ExecContext(ctx, `
INSERT INTO project_skills(id, name, source_path, content_sha256, enabled, version, created_at, updated_at)
VALUES (?, ?, ?, ?, 1, 1, ?, ?)`, created.ID, created.Name, created.SourcePath,
		created.ContentSHA256, formatTime(now), formatTime(now))
	if err != nil {
		return capability.Skill{}, fmt.Errorf("%w: create project skill: %v", ErrCapabilityConflict, err)
	}
	return created, nil
}

// CreateMCPServer 登记一个已存在的本机 Codex MCP 配置名。
func (store *CapabilityStore) CreateMCPServer(
	ctx context.Context,
	input capability.MCPServerInput,
) (capability.MCPServer, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return capability.MCPServer{}, err
	}
	id, err := newID()
	if err != nil {
		return capability.MCPServer{}, err
	}
	now := time.Now().UTC()
	created := capability.MCPServer{
		ID: id, Name: input.Name, ConfigName: input.ConfigName,
		Enabled: true, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	_, err = store.database.ExecContext(ctx, `
INSERT INTO project_mcp_servers(id, name, config_name, enabled, version, created_at, updated_at)
VALUES (?, ?, ?, 1, 1, ?, ?)`, created.ID, created.Name, created.ConfigName,
		formatTime(now), formatTime(now))
	if err != nil {
		return capability.MCPServer{}, fmt.Errorf("%w: create project MCP server: %v", ErrCapabilityConflict, err)
	}
	return created, nil
}

// GetSkill 返回一个项目 Skill 引用。
func (store *CapabilityStore) GetSkill(ctx context.Context, id string) (capability.Skill, error) {
	row := store.database.QueryRowContext(ctx, `
SELECT id, name, source_path, content_sha256, enabled, version, created_at, updated_at
FROM project_skills WHERE id = ?`, id)
	item, err := scanSkill(row)
	if errors.Is(err, sql.ErrNoRows) {
		return capability.Skill{}, ErrCapabilityNotFound
	}
	if err != nil {
		return capability.Skill{}, fmt.Errorf("get project skill: %w", err)
	}
	return item, nil
}

// RefreshSkill 以乐观版本更新已重新校验的 Skill 内容哈希。
func (store *CapabilityStore) RefreshSkill(
	ctx context.Context,
	id string,
	expectedVersion int64,
	contentSHA256 string,
) (capability.Skill, error) {
	if id == "" || expectedVersion < 1 || len(contentSHA256) != 64 {
		return capability.Skill{}, errors.New("Skill 刷新参数无效")
	}
	now := time.Now().UTC()
	result, err := store.database.ExecContext(ctx, `
UPDATE project_skills
SET content_sha256 = ?, version = version + 1, updated_at = ?
WHERE id = ? AND version = ?`, contentSHA256, formatTime(now), id, expectedVersion)
	if err != nil {
		return capability.Skill{}, fmt.Errorf("refresh project skill: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return capability.Skill{}, fmt.Errorf("read refreshed project skill count: %w", err)
	}
	if changed != 1 {
		return capability.Skill{}, ErrCapabilityConflict
	}
	return store.GetSkill(ctx, id)
}

func (store *CapabilityStore) listSkills(ctx context.Context) ([]capability.Skill, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, name, source_path, content_sha256, enabled, version, created_at, updated_at
FROM project_skills ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list project skills: %w", err)
	}
	defer rows.Close()
	result := make([]capability.Skill, 0)
	for rows.Next() {
		item, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

type capabilityRowScanner interface {
	Scan(...any) error
}

func scanSkill(scanner capabilityRowScanner) (capability.Skill, error) {
	var item capability.Skill
	var enabled bool
	var createdAt, updatedAt string
	if err := scanner.Scan(&item.ID, &item.Name, &item.SourcePath, &item.ContentSHA256,
		&enabled, &item.Version, &createdAt, &updatedAt); err != nil {
		return capability.Skill{}, err
	}
	item.Enabled = enabled
	if err := parseCapabilityTimes(&item.CreatedAt, &item.UpdatedAt, createdAt, updatedAt); err != nil {
		return capability.Skill{}, err
	}
	return item, nil
}

func (store *CapabilityStore) listMCPServers(ctx context.Context) ([]capability.MCPServer, error) {
	rows, err := store.database.QueryContext(ctx, `
SELECT id, name, config_name, enabled, version, created_at, updated_at
FROM project_mcp_servers ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list project MCP servers: %w", err)
	}
	defer rows.Close()
	result := make([]capability.MCPServer, 0)
	for rows.Next() {
		var item capability.MCPServer
		var enabled bool
		var createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.Name, &item.ConfigName, &enabled,
			&item.Version, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled
		if err := parseCapabilityTimes(&item.CreatedAt, &item.UpdatedAt, createdAt, updatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func parseCapabilityTimes(created, updated *time.Time, createdAt, updatedAt string) error {
	var err error
	*created, err = parseTime(createdAt)
	if err != nil {
		return fmt.Errorf("parse capability created_at: %w", err)
	}
	*updated, err = parseTime(updatedAt)
	if err != nil {
		return fmt.Errorf("parse capability updated_at: %w", err)
	}
	return nil
}
