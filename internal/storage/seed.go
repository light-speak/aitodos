package storage

// baselineSeed 保存新项目必须存在的内置 Agent Profile；用户配置会创建新 Revision。
const baselineSeed = `INSERT INTO agent_profiles(id, name, role, current_revision_id, created_at, updated_at) VALUES
('profile-planner', '规划 Agent', 'PLANNER', 'profile-planner-r1', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
('profile-triager', '任务评估 Agent', 'TRIAGER', 'profile-triager-r1', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
('profile-implementer', '实现 Agent', 'IMPLEMENTER', 'profile-implementer-r1', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
('profile-revision', '修订 Agent', 'REVISION', 'profile-revision-r1', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
('profile-reviewer', '审查 Agent', 'REVIEWER', 'profile-reviewer-r1', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

INSERT INTO agent_profile_revisions(
    id, profile_id, revision, instructions, adapter, command, args_json, model,
    max_input_tokens, reserved_output_tokens, recent_message_limit, retrieval_limit,
    workspace_policy, approval_policy, timeout_seconds, created_at
) VALUES
('profile-planner-r1', 'profile-planner', 1, '分析 Topic，提出必要澄清，生成可审核的 Plan、Task 草案、验收标准、测试项和估算。未经人工批准不得执行正式 Task。', 'generic', '', '[]', '', 64000, 12000, 30, 10, 'NONE', 'READ_ONLY', 3600, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
('profile-triager-r1', 'profile-triager', 1, '只评估当前正式 Task：生成简洁动宾标题，按固定六维标准给出 0 到 4 的原始评分、置信度、依据、假设和拆分建议。不得修改代码、优先级、模型或权限。', 'generic', '', '[]', '', 32000, 6000, 20, 8, 'NONE', 'READ_ONLY', 900, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
('profile-implementer-r1', 'profile-implementer', 1, '只实现当前 Task，在受管 Workspace 内工作，满足验收标准并报告实际执行的测试证据。', 'generic', '', '[]', '', 64000, 12000, 20, 8, 'WRITE_TASK', 'WORKSPACE_WRITE', 3600, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
('profile-revision-r1', 'profile-revision', 1, '针对最近一次驳回意见修订当前 Task，保留正确改动，重新运行受影响测试并报告证据。', 'generic', '', '[]', '', 64000, 12000, 20, 8, 'WRITE_TASK', 'WORKSPACE_WRITE', 3600, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
('profile-reviewer-r1', 'profile-reviewer', 1, '只读审查当前 Task 的验收标准、Diff 和测试证据，明确区分已验证事实与推断。', 'generic', '', '[]', '', 48000, 10000, 15, 8, 'READ_ONLY', 'READ_ONLY', 1800, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

INSERT INTO project_agent_defaults(purpose, profile_id) VALUES
('PLANNING', 'profile-planner'),
('TRIAGE', 'profile-triager'),
('IMPLEMENTATION', 'profile-implementer'),
('REVISION', 'profile-revision'),
('REVIEW', 'profile-reviewer');`
