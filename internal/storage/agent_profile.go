package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/capability"
)

var (
	// ErrAgentProfileNotFound 表示指定 Agent Profile 不存在。
	ErrAgentProfileNotFound = errors.New("agent profile not found")
	// ErrAgentProfileRevisionConflict 表示当前修订已被其他请求更新。
	ErrAgentProfileRevisionConflict = errors.New("agent profile revision conflict")
)

const profileColumns = `
p.id, p.name, p.role, p.created_at, p.updated_at,
r.id, r.profile_id, r.revision, r.instructions, r.adapter, r.command, r.args_json, r.model,
r.max_input_tokens, r.reserved_output_tokens, r.recent_message_limit, r.retrieval_limit,
r.workspace_policy, r.approval_policy, r.timeout_seconds, r.created_at`

// AgentProfileStore 持久化固定职责及其不可变配置修订。
type AgentProfileStore struct {
	database *sql.DB
}

// NewAgentProfileStore 创建 Agent Profile 持久化服务。
func NewAgentProfileStore(database *sql.DB) *AgentProfileStore {
	return &AgentProfileStore{database: database}
}

// List 按固定职责顺序返回当前配置。
func (store *AgentProfileStore) List(ctx context.Context) ([]agentprofile.Profile, error) {
	rows, err := store.database.QueryContext(ctx, `SELECT `+profileColumns+`
FROM agent_profiles p
JOIN agent_profile_revisions r ON r.id = p.current_revision_id
ORDER BY CASE p.role
    WHEN 'PLANNER' THEN 1 WHEN 'TRIAGER' THEN 2 WHEN 'IMPLEMENTER' THEN 3
    WHEN 'REVISION' THEN 4 ELSE 5 END`)
	if err != nil {
		return nil, fmt.Errorf("list agent profiles: %w", err)
	}
	profiles := make([]agentprofile.Profile, 0, 5)
	for rows.Next() {
		profile, scanErr := scanAgentProfile(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate agent profiles: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close agent profiles: %w", err)
	}
	for index := range profiles {
		policy, policyErr := loadRevisionToolPolicy(ctx, store.database, profiles[index].CurrentRevision.ID)
		if policyErr != nil {
			return nil, policyErr
		}
		profiles[index].CurrentRevision.ToolPolicy = policy
	}
	return profiles, nil
}

// GetByRole 按固定职责返回当前配置。
func (store *AgentProfileStore) GetByRole(ctx context.Context, role agentprofile.Role) (agentprofile.Profile, error) {
	return store.queryProfile(ctx, "p.role = ?", role)
}

// Get 按 ID 返回当前配置。
func (store *AgentProfileStore) Get(ctx context.Context, profileID string) (agentprofile.Profile, error) {
	return store.queryProfile(ctx, "p.id = ?", profileID)
}

// GetRevision 按 ID 返回一个不可变配置修订。
func (store *AgentProfileStore) GetRevision(ctx context.Context, revisionID string) (agentprofile.Revision, error) {
	revision, err := scanAgentRevision(store.database.QueryRowContext(ctx, `
SELECT id, profile_id, revision, instructions, adapter, command, args_json, model,
       max_input_tokens, reserved_output_tokens, recent_message_limit, retrieval_limit,
       workspace_policy, approval_policy, timeout_seconds, created_at
FROM agent_profile_revisions WHERE id = ?`, revisionID))
	if errors.Is(err, sql.ErrNoRows) {
		return agentprofile.Revision{}, ErrAgentProfileNotFound
	}
	if err != nil {
		return agentprofile.Revision{}, err
	}
	revision.ToolPolicy, err = loadRevisionToolPolicy(ctx, store.database, revision.ID)
	return revision, err
}

// CreateRevision 创建不可变修订并原子切换当前修订。
func (store *AgentProfileStore) CreateRevision(
	ctx context.Context,
	profileID string,
	input agentprofile.RevisionInput,
) (agentprofile.Profile, error) {
	input = input.Normalized()
	input.ToolPolicy = input.ToolPolicy.Normalized()
	if err := input.Validate(); err != nil {
		return agentprofile.Profile{}, err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return agentprofile.Profile{}, fmt.Errorf("begin agent profile revision: %w", err)
	}
	defer transaction.Rollback()

	role, currentRevisionID, revision, err := currentProfileRevision(ctx, transaction, profileID)
	if err != nil {
		return agentprofile.Profile{}, err
	}
	workspacePolicy, approvalPolicy, err := agentprofile.PoliciesForRole(role)
	if err != nil {
		return agentprofile.Profile{}, err
	}
	revisionID, err := newID()
	if err != nil {
		return agentprofile.Profile{}, err
	}
	argsJSON, err := json.Marshal(input.Args)
	if err != nil {
		return agentprofile.Profile{}, fmt.Errorf("encode agent args: %w", err)
	}
	now := time.Now().UTC()
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO agent_profile_revisions(
    id, profile_id, revision, instructions, adapter, command, args_json, model,
    max_input_tokens, reserved_output_tokens, recent_message_limit, retrieval_limit,
    workspace_policy, approval_policy, timeout_seconds, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		revisionID, profileID, revision+1, input.Instructions, input.Adapter, input.Command,
		string(argsJSON), input.Model, input.MaxInputTokens, input.ReservedOutputTokens,
		input.RecentMessageLimit, input.RetrievalLimit, workspacePolicy, approvalPolicy,
		input.TimeoutSeconds, formatTime(now),
	); err != nil {
		return agentprofile.Profile{}, fmt.Errorf("insert agent profile revision: %w", err)
	}
	if err := insertRevisionToolPolicy(ctx, transaction, revisionID, input.ToolPolicy); err != nil {
		return agentprofile.Profile{}, err
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE agent_profiles SET current_revision_id = ?, updated_at = ?
WHERE id = ? AND current_revision_id = ?`, revisionID, formatTime(now), profileID, currentRevisionID)
	if err != nil {
		return agentprofile.Profile{}, fmt.Errorf("activate agent profile revision: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return agentprofile.Profile{}, fmt.Errorf("read agent profile update result: %w", err)
	}
	if changed != 1 {
		return agentprofile.Profile{}, ErrAgentProfileRevisionConflict
	}
	if err := transaction.Commit(); err != nil {
		return agentprofile.Profile{}, fmt.Errorf("commit agent profile revision: %w", err)
	}
	return store.Get(ctx, profileID)
}

// ListRevisions 按新到旧返回完整修订历史。
func (store *AgentProfileStore) ListRevisions(ctx context.Context, profileID string) ([]agentprofile.Revision, error) {
	if _, err := store.Get(ctx, profileID); err != nil {
		return nil, err
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT id, profile_id, revision, instructions, adapter, command, args_json, model,
       max_input_tokens, reserved_output_tokens, recent_message_limit, retrieval_limit,
       workspace_policy, approval_policy, timeout_seconds, created_at
FROM agent_profile_revisions WHERE profile_id = ? ORDER BY revision DESC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list agent profile revisions: %w", err)
	}
	revisions := make([]agentprofile.Revision, 0)
	for rows.Next() {
		revision, scanErr := scanAgentRevision(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		revisions = append(revisions, revision)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate agent profile revisions: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close agent profile revisions: %w", err)
	}
	for index := range revisions {
		policy, policyErr := loadRevisionToolPolicy(ctx, store.database, revisions[index].ID)
		if policyErr != nil {
			return nil, policyErr
		}
		revisions[index].ToolPolicy = policy
	}
	return revisions, nil
}

func (store *AgentProfileStore) queryProfile(ctx context.Context, predicate string, value any) (agentprofile.Profile, error) {
	profile, err := queryAgentProfile(ctx, store.database, predicate, value)
	if err != nil {
		return agentprofile.Profile{}, err
	}
	profile.CurrentRevision.ToolPolicy, err = loadRevisionToolPolicy(ctx, store.database, profile.CurrentRevision.ID)
	return profile, err
}

func currentProfileRevision(
	ctx context.Context,
	transaction *sql.Tx,
	profileID string,
) (agentprofile.Role, string, int64, error) {
	var role agentprofile.Role
	var currentRevisionID string
	var revision int64
	err := transaction.QueryRowContext(ctx, `
SELECT p.role, p.current_revision_id, r.revision
FROM agent_profiles p
JOIN agent_profile_revisions r ON r.id = p.current_revision_id
WHERE p.id = ?`, profileID).Scan(&role, &currentRevisionID, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", 0, ErrAgentProfileNotFound
	}
	if err != nil {
		return "", "", 0, fmt.Errorf("read current agent profile revision: %w", err)
	}
	return role, currentRevisionID, revision, nil
}

func queryAgentProfile(ctx context.Context, database *sql.DB, predicate string, value any) (agentprofile.Profile, error) {
	profile, err := scanAgentProfile(database.QueryRowContext(ctx, `SELECT `+profileColumns+`
FROM agent_profiles p
JOIN agent_profile_revisions r ON r.id = p.current_revision_id
WHERE `+predicate, value))
	if errors.Is(err, sql.ErrNoRows) {
		return agentprofile.Profile{}, ErrAgentProfileNotFound
	}
	return profile, err
}

func scanAgentProfile(scanner rowScanner) (agentprofile.Profile, error) {
	var profile agentprofile.Profile
	var revision agentprofile.Revision
	var profileCreated, profileUpdated, revisionCreated string
	var argsJSON string
	err := scanner.Scan(
		&profile.ID, &profile.Name, &profile.Role, &profileCreated, &profileUpdated,
		&revision.ID, &revision.ProfileID, &revision.Revision, &revision.Instructions,
		&revision.Adapter, &revision.Command, &argsJSON, &revision.Model,
		&revision.MaxInputTokens, &revision.ReservedOutputTokens, &revision.RecentMessageLimit,
		&revision.RetrievalLimit, &revision.WorkspacePolicy, &revision.ApprovalPolicy,
		&revision.TimeoutSeconds, &revisionCreated,
	)
	if err != nil {
		return agentprofile.Profile{}, err
	}
	if err := decodeAgentRevisionTimesAndArgs(&revision, argsJSON, revisionCreated); err != nil {
		return agentprofile.Profile{}, err
	}
	profile.CreatedAt, err = parseTime(profileCreated)
	if err != nil {
		return agentprofile.Profile{}, fmt.Errorf("parse agent profile created_at: %w", err)
	}
	profile.UpdatedAt, err = parseTime(profileUpdated)
	if err != nil {
		return agentprofile.Profile{}, fmt.Errorf("parse agent profile updated_at: %w", err)
	}
	profile.CurrentRevision = revision
	return profile, nil
}

func scanAgentRevision(scanner rowScanner) (agentprofile.Revision, error) {
	var revision agentprofile.Revision
	var argsJSON, createdAt string
	err := scanner.Scan(
		&revision.ID, &revision.ProfileID, &revision.Revision, &revision.Instructions,
		&revision.Adapter, &revision.Command, &argsJSON, &revision.Model,
		&revision.MaxInputTokens, &revision.ReservedOutputTokens, &revision.RecentMessageLimit,
		&revision.RetrievalLimit, &revision.WorkspacePolicy, &revision.ApprovalPolicy,
		&revision.TimeoutSeconds, &createdAt,
	)
	if err != nil {
		return agentprofile.Revision{}, err
	}
	if err := decodeAgentRevisionTimesAndArgs(&revision, argsJSON, createdAt); err != nil {
		return agentprofile.Revision{}, err
	}
	return revision, nil
}

func decodeAgentRevisionTimesAndArgs(revision *agentprofile.Revision, argsJSON string, createdAt string) error {
	if err := json.Unmarshal([]byte(argsJSON), &revision.Args); err != nil {
		return fmt.Errorf("decode agent args: %w", err)
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return fmt.Errorf("parse agent revision created_at: %w", err)
	}
	revision.CreatedAt = parsed
	return nil
}

type policyQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadRevisionToolPolicy(
	ctx context.Context,
	queryer policyQueryer,
	revisionID string,
) (capability.ToolPolicy, error) {
	policy := capability.ToolPolicy{Skills: []capability.SkillBinding{}, MCPServers: []capability.MCPBinding{}}
	skillRows, err := queryer.QueryContext(ctx, `
SELECT skill_id, required FROM agent_profile_revision_skills
WHERE profile_revision_id = ? ORDER BY skill_id`, revisionID)
	if err != nil {
		return capability.ToolPolicy{}, fmt.Errorf("list revision skills: %w", err)
	}
	for skillRows.Next() {
		var binding capability.SkillBinding
		if err := skillRows.Scan(&binding.SkillID, &binding.Required); err != nil {
			skillRows.Close()
			return capability.ToolPolicy{}, err
		}
		policy.Skills = append(policy.Skills, binding)
	}
	if err := skillRows.Err(); err != nil {
		skillRows.Close()
		return capability.ToolPolicy{}, err
	}
	if err := skillRows.Close(); err != nil {
		return capability.ToolPolicy{}, err
	}
	mcpRows, err := queryer.QueryContext(ctx, `
SELECT mcp_server_id, required, enabled_tools_json
FROM agent_profile_revision_mcp_servers
WHERE profile_revision_id = ? ORDER BY mcp_server_id`, revisionID)
	if err != nil {
		return capability.ToolPolicy{}, fmt.Errorf("list revision MCP servers: %w", err)
	}
	defer mcpRows.Close()
	for mcpRows.Next() {
		var binding capability.MCPBinding
		var toolsJSON string
		if err := mcpRows.Scan(&binding.ServerID, &binding.Required, &toolsJSON); err != nil {
			return capability.ToolPolicy{}, err
		}
		if err := json.Unmarshal([]byte(toolsJSON), &binding.EnabledTools); err != nil {
			return capability.ToolPolicy{}, fmt.Errorf("decode revision MCP tools: %w", err)
		}
		policy.MCPServers = append(policy.MCPServers, binding)
	}
	if err := mcpRows.Err(); err != nil {
		return capability.ToolPolicy{}, err
	}
	return policy, nil
}

func insertRevisionToolPolicy(
	ctx context.Context,
	transaction *sql.Tx,
	revisionID string,
	policy capability.ToolPolicyInput,
) error {
	for _, binding := range policy.Skills {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO agent_profile_revision_skills(profile_revision_id, skill_id, required)
VALUES (?, ?, ?)`, revisionID, binding.SkillID, binding.Required); err != nil {
			return fmt.Errorf("insert revision skill policy: %w", err)
		}
	}
	for _, binding := range policy.MCPServers {
		encoded, err := json.Marshal(binding.EnabledTools)
		if err != nil {
			return fmt.Errorf("encode revision MCP tools: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO agent_profile_revision_mcp_servers(
    profile_revision_id, mcp_server_id, required, enabled_tools_json
) VALUES (?, ?, ?, ?)`, revisionID, binding.ServerID, binding.Required, string(encoded)); err != nil {
			return fmt.Errorf("insert revision MCP policy: %w", err)
		}
	}
	return nil
}
