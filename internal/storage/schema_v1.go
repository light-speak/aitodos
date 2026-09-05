package storage

// baselineSchema 是 1.0 前压平后的完整数据库基线。
const baselineSchema = `CREATE TABLE schema_metadata (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    epoch INTEGER NOT NULL CHECK (epoch >= 1),
    version INTEGER NOT NULL CHECK (version >= 1),
    applied_at TEXT NOT NULL
);

CREATE TABLE project_metadata (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    instance_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    repo_root TEXT NOT NULL,
    git_common_dir TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    task_key TEXT NOT NULL UNIQUE,
    title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 200),
    description TEXT NOT NULL,
    acceptance_criteria TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN (
        'BACKLOG', 'READY', 'RUNNING', 'REVIEW', 'ACCEPTED',
        'CHANGES_REQUESTED', 'BLOCKED', 'CANCELLED'
    )),
    priority INTEGER NOT NULL DEFAULT 0,
    target_branch TEXT NOT NULL DEFAULT '',
    base_commit_sha TEXT NOT NULL DEFAULT '',
    current_workspace_id TEXT NOT NULL DEFAULT '',
    latest_run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL CHECK (version >= 1)
, title_source TEXT NOT NULL DEFAULT 'HUMAN'
    CHECK (title_source IN ('PROVISIONAL', 'AI', 'HUMAN')), title_locked INTEGER NOT NULL DEFAULT 1
    CHECK (title_locked IN (0, 1)), assessment_input_version INTEGER NOT NULL DEFAULT 1
    CHECK (assessment_input_version >= 1), source_plan_revision_id TEXT REFERENCES plan_revisions(id) ON DELETE RESTRICT, source_plan_task_draft_id TEXT REFERENCES plan_task_drafts(id) ON DELETE RESTRICT, archived_at TEXT);

CREATE TABLE topics (
    id TEXT PRIMARY KEY,
    topic_key TEXT NOT NULL UNIQUE,
    title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 200),
    description TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN (
        'OPEN', 'NEEDS_CLARIFICATION', 'PLAN_REVIEW', 'PLANNED', 'CLOSED'
    )),
    current_plan_id TEXT NOT NULL DEFAULT '',
    current_summary_id TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL CHECK (version >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE threads (
    id TEXT PRIMARY KEY,
    topic_id TEXT REFERENCES topics(id) ON DELETE CASCADE,
    task_id TEXT REFERENCES tasks(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (
        (topic_id IS NOT NULL AND task_id IS NULL) OR
        (topic_id IS NULL AND task_id IS NOT NULL)
    )
);

CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    author_kind TEXT NOT NULL CHECK (author_kind IN ('HUMAN', 'AGENT', 'SYSTEM')),
    content TEXT NOT NULL CHECK (length(trim(content)) BETWEEN 1 AND 20000),
    created_at TEXT NOT NULL,
    UNIQUE(thread_id, sequence)
);

CREATE TABLE artifacts (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('IMAGE')),
    original_name TEXT NOT NULL,
    original_media_type TEXT NOT NULL,
    original_relative_path TEXT NOT NULL UNIQUE,
    original_size INTEGER NOT NULL CHECK (original_size > 0),
    original_sha256 TEXT NOT NULL,
    optimized_media_type TEXT NOT NULL,
    optimized_relative_path TEXT NOT NULL UNIQUE,
    optimized_size INTEGER NOT NULL CHECK (optimized_size > 0),
    optimized_sha256 TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE message_task_links (
    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    PRIMARY KEY(message_id, task_id)
);

CREATE TABLE topic_task_links (
    topic_id TEXT NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    source_message_id TEXT REFERENCES messages(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(topic_id, task_id)
);

CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL UNIQUE REFERENCES tasks(id) ON DELETE CASCADE,
    path TEXT NOT NULL UNIQUE,
    branch_name TEXT NOT NULL UNIQUE,
    target_branch TEXT NOT NULL,
    base_commit_sha TEXT NOT NULL,
    head_sha TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK (state IN ('PROVISIONING', 'READY', 'DIRTY', 'QUARANTINED', 'ERROR')),
    dirty INTEGER NOT NULL DEFAULT 0 CHECK (dirty IN (0, 1)),
    failure_message TEXT NOT NULL DEFAULT '',
    last_verified_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE releases (
    id TEXT PRIMARY KEY,
    version TEXT NOT NULL UNIQUE,
    tag_name TEXT NOT NULL UNIQUE,
    source_branch TEXT NOT NULL,
    commit_sha TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('CREATING', 'TAGGED', 'FAILED')),
    failure_message TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    tagged_at TEXT
);

CREATE TABLE release_tasks (
    release_id TEXT NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    PRIMARY KEY(release_id, task_id)
);

CREATE TABLE task_reviews (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    decision TEXT NOT NULL CHECK (decision IN ('ACCEPTED', 'REJECTED')),
    comment TEXT NOT NULL,
    commit_sha TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE TABLE agent_profile_revisions (
    id TEXT PRIMARY KEY,
    profile_id TEXT NOT NULL REFERENCES agent_profiles(id) ON DELETE RESTRICT,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    instructions TEXT NOT NULL,
    adapter TEXT NOT NULL,
    command TEXT NOT NULL,
    args_json TEXT NOT NULL,
    model TEXT NOT NULL,
    max_input_tokens INTEGER NOT NULL CHECK (max_input_tokens >= 4096),
    reserved_output_tokens INTEGER NOT NULL CHECK (
        reserved_output_tokens >= 512 AND reserved_output_tokens < max_input_tokens
    ),
    recent_message_limit INTEGER NOT NULL CHECK (recent_message_limit BETWEEN 0 AND 200),
    retrieval_limit INTEGER NOT NULL CHECK (retrieval_limit BETWEEN 0 AND 100),
    workspace_policy TEXT NOT NULL CHECK (workspace_policy IN ('NONE', 'READ_ONLY', 'WRITE_TASK')),
    approval_policy TEXT NOT NULL CHECK (approval_policy IN ('READ_ONLY', 'WORKSPACE_WRITE')),
    timeout_seconds INTEGER NOT NULL CHECK (timeout_seconds BETWEEN 30 AND 86400),
    created_at TEXT NOT NULL,
    UNIQUE(profile_id, revision)
);

CREATE TABLE task_estimates (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    points INTEGER NOT NULL CHECK (points IN (1, 2, 3, 5, 8, 13)),
    remaining_points INTEGER NOT NULL CHECK (remaining_points BETWEEN 0 AND points),
    confidence REAL NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    rationale TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('AI', 'HUMAN')),
    source_run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    CHECK (source != 'AI' OR source_run_id IS NOT NULL),
    UNIQUE(task_id, revision)
);

CREATE TABLE task_test_cases (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    required INTEGER NOT NULL CHECK (required IN (0, 1)),
    sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
    created_by TEXT NOT NULL CHECK (created_by IN ('HUMAN', 'AGENT')),
    source_run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (created_by != 'AGENT' OR source_run_id IS NOT NULL)
);

CREATE TABLE task_test_results (
    id TEXT PRIMARY KEY,
    test_case_id TEXT NOT NULL REFERENCES task_test_cases(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    outcome TEXT NOT NULL CHECK (outcome IN ('PASSED', 'FAILED', 'BLOCKED')),
    evidence_kind TEXT NOT NULL CHECK (evidence_kind IN ('COMMAND', 'HUMAN', 'AGENT_REPORT')),
    summary TEXT NOT NULL,
    command TEXT NOT NULL DEFAULT '',
    artifact_ref TEXT NOT NULL DEFAULT '',
    source_run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL, exit_code INTEGER,
    CHECK (evidence_kind != 'COMMAND' OR length(trim(command)) > 0),
    CHECK (evidence_kind != 'AGENT_REPORT' OR source_run_id IS NOT NULL)
);

CREATE TABLE run_artifacts (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('PROMPT', 'CONTEXT_MANIFEST', 'STDOUT', 'STDERR', 'RESULT')),
    relative_path TEXT NOT NULL UNIQUE,
    sha256 TEXT NOT NULL,
    size INTEGER NOT NULL CHECK (size >= 0),
    truncated INTEGER NOT NULL CHECK (truncated IN (0, 1)),
    created_at TEXT NOT NULL,
    UNIQUE(run_id, kind)
);

CREATE TABLE agent_profiles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    role TEXT NOT NULL UNIQUE CHECK (role IN (
        'PLANNER', 'TRIAGER', 'IMPLEMENTER', 'REVISION', 'REVIEWER'
    )),
    current_revision_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY(current_revision_id) REFERENCES agent_profile_revisions(id) DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE project_agent_defaults (
    purpose TEXT PRIMARY KEY CHECK (purpose IN (
        'PLANNING', 'TRIAGE', 'IMPLEMENTATION', 'REVISION', 'REVIEW'
    )),
    profile_id TEXT NOT NULL REFERENCES agent_profiles(id) ON DELETE RESTRICT
);

CREATE TABLE task_assessments (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    task_assessment_version INTEGER NOT NULL CHECK (task_assessment_version >= 1),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    suggested_title TEXT NOT NULL,
    applied_title TEXT NOT NULL,
    technical_complexity INTEGER NOT NULL CHECK (technical_complexity BETWEEN 0 AND 4),
    requirement_uncertainty INTEGER NOT NULL CHECK (requirement_uncertainty BETWEEN 0 AND 4),
    change_scope INTEGER NOT NULL CHECK (change_scope BETWEEN 0 AND 4),
    validation_burden INTEGER NOT NULL CHECK (validation_burden BETWEEN 0 AND 4),
    human_dependency INTEGER NOT NULL CHECK (human_dependency BETWEEN 0 AND 4),
    risk_and_reversibility INTEGER NOT NULL CHECK (risk_and_reversibility BETWEEN 0 AND 4),
    weighted_score REAL NOT NULL CHECK (weighted_score BETWEEN 0 AND 4),
    complexity TEXT NOT NULL CHECK (complexity IN ('C1', 'C2', 'C3', 'C4', 'C5')),
    autonomy TEXT NOT NULL CHECK (autonomy IN ('A0', 'A1', 'A2', 'A3')),
    confidence REAL NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    rationale TEXT NOT NULL,
    assumptions_json TEXT NOT NULL,
    split_recommended INTEGER NOT NULL CHECK (split_recommended IN (0, 1)),
    split_rationale TEXT NOT NULL,
    source_run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    UNIQUE(task_id, revision),
    UNIQUE(source_run_id)
);

CREATE TABLE runs (
    id TEXT PRIMARY KEY,
    purpose TEXT NOT NULL CHECK (purpose IN (
        'PLANNING', 'TRIAGE', 'IMPLEMENTATION', 'REVISION', 'REVIEW'
    )),
    topic_id TEXT REFERENCES topics(id) ON DELETE RESTRICT,
    task_id TEXT REFERENCES tasks(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN (
        'CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING', 'NEEDS_INPUT',
        'SUCCEEDED', 'FAILED', 'CANCELLED', 'TIMED_OUT', 'LOST'
    )),
    profile_revision_id TEXT NOT NULL REFERENCES agent_profile_revisions(id) ON DELETE RESTRICT,
    retry_of_run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    continuation_of_run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    claim_token_hash TEXT NOT NULL,
    lease_generation INTEGER NOT NULL CHECK (lease_generation >= 1),
    lease_expires_at TEXT NOT NULL,
    runner_pid INTEGER,
    runner_started_at TEXT,
    run_nonce TEXT NOT NULL,
    queued_at TEXT NOT NULL,
    claimed_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    exit_code INTEGER,
    failure_kind TEXT NOT NULL DEFAULT '',
    failure_code TEXT NOT NULL DEFAULT '',
    failure_message TEXT NOT NULL DEFAULT '',
    failure_retryable INTEGER CHECK (failure_retryable IS NULL OR failure_retryable IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    subject_version INTEGER NOT NULL DEFAULT 0 CHECK (subject_version >= 0), cancel_requested_at TEXT, cancel_reason TEXT NOT NULL DEFAULT ''
    CHECK (length(cancel_reason) <= 1000), agent_session_id TEXT REFERENCES agent_sessions(id) ON DELETE RESTRICT, session_resumed INTEGER NOT NULL DEFAULT 0
    CHECK (session_resumed IN (0, 1)), runner_identity TEXT NOT NULL DEFAULT '', lease_heartbeat_at TEXT, agent_pid INTEGER, agent_started_at TEXT, agent_identity TEXT NOT NULL DEFAULT '', agent_process_released_at TEXT,
    CHECK (
        (purpose = 'PLANNING' AND topic_id IS NOT NULL AND task_id IS NULL) OR
        (purpose != 'PLANNING' AND topic_id IS NULL AND task_id IS NOT NULL)
    )
);

CREATE TABLE plans (
    id TEXT PRIMARY KEY,
    plan_key TEXT NOT NULL UNIQUE,
    topic_id TEXT NOT NULL UNIQUE REFERENCES topics(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('IN_REVIEW', 'CHANGES_REQUESTED', 'APPROVED')),
    current_revision_id TEXT NOT NULL,
    approved_revision_id TEXT,
    version INTEGER NOT NULL CHECK (version >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY(current_revision_id) REFERENCES plan_revisions(id) DEFERRABLE INITIALLY DEFERRED,
    FOREIGN KEY(approved_revision_id) REFERENCES plan_revisions(id) ON DELETE RESTRICT
);

CREATE TABLE plan_revisions (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE RESTRICT,
    revision_number INTEGER NOT NULL CHECK (revision_number >= 1),
    summary TEXT NOT NULL CHECK (length(trim(summary)) BETWEEN 1 AND 20000),
    rationale TEXT NOT NULL,
    risks TEXT NOT NULL,
    source_run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    previous_revision_id TEXT REFERENCES plan_revisions(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL, readiness_json TEXT
    CHECK (readiness_json IS NULL OR json_valid(readiness_json)),
    UNIQUE(plan_id, revision_number)
);

CREATE TABLE plan_task_drafts (
    id TEXT PRIMARY KEY,
    plan_revision_id TEXT NOT NULL REFERENCES plan_revisions(id) ON DELETE RESTRICT,
    draft_key TEXT NOT NULL,
    title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 200),
    description TEXT NOT NULL,
    acceptance_criteria TEXT NOT NULL,
    priority INTEGER NOT NULL CHECK (priority BETWEEN 0 AND 3),
    proposed_order INTEGER NOT NULL CHECK (proposed_order >= 0),
    UNIQUE(plan_revision_id, draft_key),
    UNIQUE(plan_revision_id, proposed_order)
);

CREATE TABLE plan_task_draft_test_cases (
    id TEXT PRIMARY KEY,
    task_draft_id TEXT NOT NULL REFERENCES plan_task_drafts(id) ON DELETE RESTRICT,
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    description TEXT NOT NULL,
    required INTEGER NOT NULL CHECK (required IN (0, 1)),
    sort_order INTEGER NOT NULL CHECK (sort_order >= 0),
    UNIQUE(task_draft_id, sort_order)
);

CREATE TABLE plan_reviews (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE RESTRICT,
    plan_revision_id TEXT NOT NULL REFERENCES plan_revisions(id) ON DELETE RESTRICT,
    decision TEXT NOT NULL CHECK (decision IN ('APPROVED', 'CHANGES_REQUESTED')),
    comment TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE project_skills (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    source_path TEXT NOT NULL UNIQUE,
    content_sha256 TEXT NOT NULL CHECK (length(content_sha256) = 64),
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    version INTEGER NOT NULL CHECK (version >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE project_mcp_servers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    config_name TEXT NOT NULL UNIQUE,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    version INTEGER NOT NULL CHECK (version >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE agent_profile_revision_skills (
    profile_revision_id TEXT NOT NULL REFERENCES agent_profile_revisions(id) ON DELETE RESTRICT,
    skill_id TEXT NOT NULL REFERENCES project_skills(id) ON DELETE RESTRICT,
    required INTEGER NOT NULL CHECK (required IN (0, 1)),
    PRIMARY KEY(profile_revision_id, skill_id)
);

CREATE TABLE agent_profile_revision_mcp_servers (
    profile_revision_id TEXT NOT NULL REFERENCES agent_profile_revisions(id) ON DELETE RESTRICT,
    mcp_server_id TEXT NOT NULL REFERENCES project_mcp_servers(id) ON DELETE RESTRICT,
    required INTEGER NOT NULL CHECK (required IN (0, 1)),
    enabled_tools_json TEXT NOT NULL,
    PRIMARY KEY(profile_revision_id, mcp_server_id)
);

CREATE TABLE run_tool_policy_snapshots (
    run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    snapshot_json TEXT NOT NULL,
    sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
    created_at TEXT NOT NULL
);

CREATE TABLE run_usage (
    run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    input_tokens INTEGER CHECK (input_tokens IS NULL OR input_tokens >= 0),
    cached_input_tokens INTEGER CHECK (cached_input_tokens IS NULL OR cached_input_tokens >= 0),
    cache_write_input_tokens INTEGER CHECK (cache_write_input_tokens IS NULL OR cache_write_input_tokens >= 0),
    output_tokens INTEGER CHECK (output_tokens IS NULL OR output_tokens >= 0),
    reasoning_output_tokens INTEGER CHECK (reasoning_output_tokens IS NULL OR reasoning_output_tokens >= 0),
    model_requests INTEGER CHECK (model_requests IS NULL OR model_requests >= 1),
    peak_input_tokens INTEGER CHECK (peak_input_tokens IS NULL OR peak_input_tokens >= 0),
    source TEXT NOT NULL CHECK (source IN ('CODEX_JSONL')),
    captured_at TEXT NOT NULL,
    CHECK (
        input_tokens IS NOT NULL OR cached_input_tokens IS NOT NULL OR
        cache_write_input_tokens IS NOT NULL OR output_tokens IS NOT NULL OR
        reasoning_output_tokens IS NOT NULL OR model_requests IS NOT NULL OR
        peak_input_tokens IS NOT NULL
    ),
    CHECK (input_tokens IS NULL OR cached_input_tokens IS NULL OR cached_input_tokens <= input_tokens),
    CHECK (input_tokens IS NULL OR peak_input_tokens IS NULL OR peak_input_tokens <= input_tokens)
);

CREATE TABLE run_workspace_snapshots (
    run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,
    branch_name TEXT NOT NULL,
    target_branch TEXT NOT NULL,
    base_commit_sha TEXT NOT NULL,
    head_before TEXT NOT NULL,
    head_after TEXT NOT NULL,
    dirty_before INTEGER NOT NULL CHECK (dirty_before IN (0, 1)),
    dirty_after INTEGER NOT NULL CHECK (dirty_after IN (0, 1)),
    state_after TEXT NOT NULL CHECK (state_after IN ('READY', 'DIRTY', 'QUARANTINED', 'ERROR')),
    captured_at TEXT NOT NULL
);

CREATE TABLE run_events (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    event_type TEXT NOT NULL CHECK (length(trim(event_type)) BETWEEN 1 AND 100),
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json)),
    occurred_at TEXT NOT NULL,
    UNIQUE(run_id, sequence)
);

CREATE TABLE agent_sessions (
    id TEXT PRIMARY KEY,
    topic_id TEXT REFERENCES topics(id) ON DELETE RESTRICT,
    task_id TEXT REFERENCES tasks(id) ON DELETE RESTRICT,
    profile_revision_id TEXT NOT NULL REFERENCES agent_profile_revisions(id) ON DELETE RESTRICT,
    model TEXT NOT NULL DEFAULT '',
    adapter TEXT NOT NULL,
    external_session_id TEXT NOT NULL CHECK (length(trim(external_session_id)) BETWEEN 1 AND 255),
    status TEXT NOT NULL CHECK (status IN ('ACTIVE', 'INVALID')),
    last_run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (
        (topic_id IS NOT NULL AND task_id IS NULL) OR
        (topic_id IS NULL AND task_id IS NOT NULL)
    )
);

CREATE TABLE approval_requests (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    external_request_id TEXT NOT NULL,
    item_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL CHECK (kind IN ('COMMAND', 'FILE_CHANGE', 'NETWORK', 'PERMISSIONS')),
    reason TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 4000),
    command_text TEXT NOT NULL DEFAULT '' CHECK (length(command_text) <= 16000),
    cwd TEXT NOT NULL DEFAULT '' CHECK (length(cwd) <= 4000),
    host TEXT NOT NULL DEFAULT '' CHECK (length(host) <= 1000),
    protocol TEXT NOT NULL DEFAULT '' CHECK (length(protocol) <= 100),
    grant_root TEXT NOT NULL DEFAULT '' CHECK (length(grant_root) <= 4000),
    available_decisions_json TEXT NOT NULL CHECK (json_valid(available_decisions_json)),
    status TEXT NOT NULL CHECK (status IN ('OPEN', 'RESOLVED', 'CLEARED')),
    decision TEXT NOT NULL DEFAULT '' CHECK (decision IN ('', 'ACCEPT_ONCE', 'ACCEPT_SESSION', 'DECLINE', 'CANCEL_RUN')),
    version INTEGER NOT NULL CHECK (version >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    resolved_at TEXT,
    UNIQUE(run_id, external_request_id)
);

CREATE TABLE run_finalization_intents (
    run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    terminal_status TEXT NOT NULL CHECK (terminal_status IN (
        'NEEDS_INPUT', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'TIMED_OUT', 'LOST'
    )),
    exit_code INTEGER NOT NULL,
    failure_kind TEXT NOT NULL DEFAULT '',
    failure_code TEXT NOT NULL DEFAULT '',
    failure_message TEXT NOT NULL DEFAULT '',
    failure_retryable INTEGER CHECK (failure_retryable IS NULL OR failure_retryable IN (0, 1)),
    clarification_json TEXT CHECK (clarification_json IS NULL OR json_valid(clarification_json)),
    created_at TEXT NOT NULL,
    completed_at TEXT
, planning_result_json TEXT
    CHECK (planning_result_json IS NULL OR json_valid(planning_result_json)), task_reply TEXT
    CHECK (task_reply IS NULL OR length(trim(task_reply)) BETWEEN 1 AND 20000), closure_json TEXT
    CHECK (closure_json IS NULL OR json_valid(closure_json)));

CREATE TABLE topic_events (
    id TEXT PRIMARY KEY,
    topic_id TEXT NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    event_type TEXT NOT NULL CHECK (event_type IN (
        'TOPIC_CREATED', 'TOPIC_STATUS_CHANGED', 'TOPIC_MESSAGE_ADDED', 'TOPIC_PLANNING_REQUESTED'
    )),
    payload_json TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    UNIQUE(topic_id, sequence)
);

CREATE TABLE task_feedback_turns (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    source_message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    retry_of_feedback_id TEXT UNIQUE REFERENCES task_feedback_turns(id) ON DELETE RESTRICT,
    intent TEXT NOT NULL CHECK (intent IN ('DISCUSS', 'REQUEST_CHANGES')),
    status TEXT NOT NULL CHECK (status IN ('QUEUED', 'RUNNING', 'ANSWERED', 'APPLIED', 'FAILED')),
    run_id TEXT UNIQUE REFERENCES runs(id) ON DELETE RESTRICT,
    response_message_id TEXT UNIQUE REFERENCES messages(id) ON DELETE RESTRICT,
    failure_message TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK (retry_of_feedback_id IS NULL OR retry_of_feedback_id != id),
    CHECK (
        (intent = 'DISCUSS' AND status IN ('QUEUED', 'RUNNING', 'ANSWERED', 'FAILED')) OR
        (intent = 'REQUEST_CHANGES' AND status = 'APPLIED')
    ),
    CHECK ((status = 'QUEUED' AND run_id IS NULL) OR status != 'QUEUED'),
    CHECK ((status IN ('RUNNING', 'ANSWERED', 'FAILED') AND run_id IS NOT NULL) OR
           status NOT IN ('RUNNING', 'ANSWERED', 'FAILED')),
    CHECK ((status = 'ANSWERED' AND response_message_id IS NOT NULL) OR
           (status != 'ANSWERED' AND response_message_id IS NULL))
);

CREATE TABLE task_feedback_events (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    feedback_id TEXT NOT NULL REFERENCES task_feedback_turns(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    status TEXT NOT NULL CHECK (status IN ('QUEUED', 'RUNNING', 'ANSWERED', 'APPLIED', 'FAILED')),
    run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    response_message_id TEXT REFERENCES messages(id) ON DELETE RESTRICT,
    failure_message TEXT NOT NULL DEFAULT '',
    occurred_at TEXT NOT NULL,
    UNIQUE(task_id, sequence)
);

CREATE TABLE task_integration_attempts (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    review_id TEXT NOT NULL REFERENCES task_reviews(id) ON DELETE RESTRICT,
    operation TEXT NOT NULL CHECK (operation IN ('INTEGRATE', 'SYNC')),
    status TEXT NOT NULL CHECK (status IN (
        'RUNNING', 'SUCCEEDED', 'NEEDS_SYNC', 'SYNCED', 'CONFLICT', 'FAILED'
    )),
    target_branch TEXT NOT NULL CHECK (length(trim(target_branch)) BETWEEN 1 AND 255),
    source_commit_sha TEXT NOT NULL CHECK (length(source_commit_sha) >= 7),
    target_before_sha TEXT NOT NULL CHECK (length(target_before_sha) >= 7),
    target_after_sha TEXT NOT NULL DEFAULT '',
    workspace_after_sha TEXT NOT NULL DEFAULT '',
    failure_kind TEXT NOT NULL DEFAULT '' CHECK (length(failure_kind) <= 100),
    failure_message TEXT NOT NULL DEFAULT '' CHECK (length(failure_message) <= 4000),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE mcp_call_events (
    id TEXT PRIMARY KEY,
    call_id TEXT NOT NULL CHECK (length(trim(call_id)) BETWEEN 1 AND 200),
    client_name TEXT NOT NULL CHECK (length(trim(client_name)) BETWEEN 1 AND 200),
    tool_name TEXT NOT NULL CHECK (length(trim(tool_name)) BETWEEN 1 AND 200),
    phase TEXT NOT NULL CHECK (phase IN ('STARTED', 'COMPLETED', 'FAILED')),
    argument_keys_json TEXT NOT NULL CHECK (json_valid(argument_keys_json)),
    arguments_sha256 TEXT NOT NULL CHECK (length(arguments_sha256) = 64),
    result_bytes INTEGER CHECK (result_bytes IS NULL OR result_bytes >= 0),
    error_message TEXT NOT NULL DEFAULT '' CHECK (length(error_message) <= 2000),
    occurred_at TEXT NOT NULL
, direction TEXT NOT NULL DEFAULT 'INBOUND'
    CHECK (direction IN ('INBOUND', 'OUTBOUND')), run_id TEXT REFERENCES runs(id) ON DELETE CASCADE, server_name TEXT NOT NULL DEFAULT '' CHECK (length(server_name) <= 200));

CREATE TABLE task_relations (
    source_task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    target_task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    relation_type TEXT NOT NULL CHECK (relation_type IN (
        'RELATES_TO', 'BLOCKS', 'PARENT_OF', 'SUPERSEDES', 'DERIVED_FROM'
    )),
    source_message_id TEXT REFERENCES messages(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    CHECK(source_task_id != target_task_id),
    CHECK(relation_type != 'RELATES_TO' OR source_task_id < target_task_id),
    PRIMARY KEY(source_task_id, target_task_id, relation_type)
);

CREATE TABLE task_events (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    event_type TEXT NOT NULL CHECK (event_type IN (
        'TASK_CREATED', 'TASK_STATUS_CHANGED', 'TASK_TITLE_CHANGED',
        'TASK_TARGET_BRANCH_CHANGED', 'TASK_DETAILS_CHANGED', 'TASK_ARCHIVED'
    )),
    payload_json TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    UNIQUE(task_id, sequence)
);

CREATE TABLE decisions (
    id TEXT PRIMARY KEY,
    decision_key TEXT NOT NULL UNIQUE,
    topic_id TEXT REFERENCES topics(id) ON DELETE CASCADE,
    task_id TEXT REFERENCES tasks(id) ON DELETE CASCADE,
    title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 200),
    content TEXT NOT NULL CHECK (length(trim(content)) BETWEEN 1 AND 20000),
    status TEXT NOT NULL CHECK (status IN ('ACTIVE', 'SUPERSEDED')),
    supersedes_decision_id TEXT REFERENCES decisions(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    CHECK ((topic_id IS NOT NULL AND task_id IS NULL) OR (topic_id IS NULL AND task_id IS NOT NULL)),
    CHECK (supersedes_decision_id IS NULL OR supersedes_decision_id != id)
);

CREATE TABLE labels (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE COLLATE NOCASE CHECK (length(trim(name)) BETWEEN 1 AND 100),
    color TEXT NOT NULL CHECK (color GLOB '#[0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f]'),
    created_at TEXT NOT NULL
);

CREATE TABLE topic_labels (
    topic_id TEXT NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    label_id TEXT NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    PRIMARY KEY(topic_id, label_id)
);

CREATE TABLE task_labels (
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    label_id TEXT NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    PRIMARY KEY(task_id, label_id)
);

CREATE TABLE run_summaries (
    run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    summary TEXT NOT NULL CHECK (length(trim(summary)) BETWEEN 1 AND 4000),
    passed_tests INTEGER NOT NULL DEFAULT 0 CHECK (passed_tests >= 0),
    failed_tests INTEGER NOT NULL DEFAULT 0 CHECK (failed_tests >= 0),
    created_at TEXT NOT NULL
);

CREATE TABLE ci_check_snapshots (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (length(trim(provider)) BETWEEN 1 AND 100),
    commit_sha TEXT NOT NULL CHECK (length(trim(commit_sha)) BETWEEN 7 AND 64),
    state TEXT NOT NULL CHECK (state IN ('PENDING', 'PASSED', 'FAILED', 'CANCELLED', 'UNKNOWN')),
    checks_json TEXT NOT NULL CHECK (json_valid(checks_json)),
    source_url TEXT NOT NULL DEFAULT '' CHECK (length(source_url) <= 2000),
    observed_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE clarifications (
    id TEXT PRIMARY KEY,
    topic_id TEXT REFERENCES topics(id) ON DELETE CASCADE,
    task_id TEXT REFERENCES tasks(id) ON DELETE CASCADE,
    source_run_id TEXT NOT NULL UNIQUE REFERENCES runs(id) ON DELETE RESTRICT,
    continuation_run_id TEXT UNIQUE REFERENCES runs(id) ON DELETE RESTRICT,
    continuation_purpose TEXT NOT NULL CHECK (continuation_purpose IN ('PLANNING', 'TRIAGE', 'IMPLEMENTATION', 'REVISION')),
    category TEXT NOT NULL CHECK (category IN ('REQUIREMENT', 'DECISION', 'ENVIRONMENT', 'VALIDATION')),
    question TEXT NOT NULL CHECK (length(trim(question)) BETWEEN 1 AND 4000),
    options_json TEXT NOT NULL CHECK (json_valid(options_json)),
    recommended_option_id TEXT NOT NULL DEFAULT '',
    allow_custom_answer INTEGER NOT NULL CHECK (allow_custom_answer IN (0, 1)),
    status TEXT NOT NULL CHECK (status IN ('OPEN', 'ANSWERED')),
    selected_option_id TEXT NOT NULL DEFAULT '',
    custom_answer TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL CHECK (version >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    answered_at TEXT,
    CHECK ((topic_id IS NOT NULL AND task_id IS NULL) OR (topic_id IS NULL AND task_id IS NOT NULL)),
    CHECK (
        (status = 'OPEN' AND selected_option_id = '' AND custom_answer = '' AND answered_at IS NULL) OR
        (status = 'ANSWERED' AND ((selected_option_id != '' AND custom_answer = '') OR
                                  (selected_option_id = '' AND custom_answer != '')) AND answered_at IS NOT NULL)
    )
);

CREATE TABLE run_resource_leases (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    resource_kind TEXT NOT NULL CHECK (resource_kind IN ('BROWSER_SESSION')),
    provider_name TEXT NOT NULL CHECK (length(trim(provider_name)) BETWEEN 1 AND 200),
    state TEXT NOT NULL CHECK (state IN ('ACTIVE', 'RELEASED', 'ABANDONED')),
    opened_at TEXT NOT NULL,
    released_at TEXT,
    cleanup_reason TEXT NOT NULL DEFAULT '' CHECK (length(cleanup_reason) <= 1000),
    UNIQUE(run_id, resource_kind, provider_name),
    CHECK ((state = 'ACTIVE' AND released_at IS NULL) OR (state != 'ACTIVE' AND released_at IS NOT NULL))
);

CREATE TABLE experience_records (
    id TEXT PRIMARY KEY,
    experience_key TEXT NOT NULL UNIQUE,
    topic_id TEXT REFERENCES topics(id) ON DELETE CASCADE,
    task_id TEXT REFERENCES tasks(id) ON DELETE CASCADE,
    title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 200),
    summary TEXT NOT NULL CHECK (length(trim(summary)) BETWEEN 1 AND 1000),
    guidance TEXT NOT NULL CHECK (length(trim(guidance)) BETWEEN 1 AND 4000),
    applicability TEXT NOT NULL CHECK (length(trim(applicability)) BETWEEN 1 AND 1000),
    project_wide INTEGER NOT NULL CHECK (project_wide IN (0, 1)),
    status TEXT NOT NULL CHECK (status IN ('CANDIDATE', 'ACTIVE', 'CHALLENGED', 'SUPERSEDED')),
    pinned INTEGER NOT NULL CHECK (pinned IN (0, 1)),
    verification_count INTEGER NOT NULL CHECK (verification_count >= 0),
    successful_applications INTEGER NOT NULL CHECK (successful_applications >= 0),
    failed_applications INTEGER NOT NULL CHECK (failed_applications >= 0),
    source_run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
    supersedes_experience_id TEXT REFERENCES experience_records(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL, candidate_fingerprint TEXT NOT NULL DEFAULT '',
    CHECK ((topic_id IS NOT NULL AND task_id IS NULL) OR (topic_id IS NULL AND task_id IS NOT NULL))
);

CREATE TABLE experience_recalls (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    experience_id TEXT NOT NULL REFERENCES experience_records(id) ON DELETE CASCADE,
    rank INTEGER NOT NULL CHECK (rank >= 1),
    relevance_score REAL NOT NULL CHECK (relevance_score BETWEEN 0 AND 1),
    utility_score REAL NOT NULL CHECK (utility_score BETWEEN 0 AND 1),
    scope_score REAL NOT NULL CHECK (scope_score BETWEEN 0 AND 1),
    freshness_score REAL NOT NULL CHECK (freshness_score BETWEEN 0 AND 1),
    final_score REAL NOT NULL CHECK (final_score BETWEEN 0 AND 1),
    estimated_tokens INTEGER NOT NULL CHECK (estimated_tokens >= 0),
    outcome TEXT NOT NULL CHECK (outcome IN ('PENDING', 'HELPFUL', 'HARMFUL', 'IGNORED')),
    recalled_at TEXT NOT NULL,
    evaluated_at TEXT,
    UNIQUE(run_id, experience_id)
);

CREATE TABLE retrieval_eval_cases (
    id TEXT PRIMARY KEY,
    query TEXT NOT NULL CHECK (length(trim(query)) BETWEEN 1 AND 500),
    kinds_json TEXT NOT NULL CHECK (json_valid(kinds_json)),
    only_current INTEGER NOT NULL CHECK (only_current IN (0, 1)),
    note TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 1000),
    active INTEGER NOT NULL CHECK (active IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(query, kinds_json, only_current)
);

CREATE TABLE retrieval_eval_relevances (
    case_id TEXT NOT NULL REFERENCES retrieval_eval_cases(id) ON DELETE RESTRICT,
    document_id TEXT NOT NULL CHECK (length(trim(document_id)) BETWEEN 3 AND 300),
    created_at TEXT NOT NULL,
    PRIMARY KEY(case_id, document_id)
);

CREATE TABLE retrieval_eval_runs (
    id TEXT PRIMARY KEY,
    engine TEXT NOT NULL CHECK (length(trim(engine)) BETWEEN 1 AND 100),
    k INTEGER NOT NULL CHECK (k BETWEEN 1 AND 50),
    case_count INTEGER NOT NULL CHECK (case_count > 0),
    relevant_count INTEGER NOT NULL CHECK (relevant_count > 0),
    recalled_count INTEGER NOT NULL CHECK (recalled_count BETWEEN 0 AND relevant_count),
    hit_cases INTEGER NOT NULL CHECK (hit_cases BETWEEN 0 AND case_count),
    recall_at_k REAL NOT NULL CHECK (recall_at_k BETWEEN 0 AND 1),
    hit_at_k REAL NOT NULL CHECK (hit_at_k BETWEEN 0 AND 1),
    mrr REAL NOT NULL CHECK (mrr BETWEEN 0 AND 1),
    created_at TEXT NOT NULL
);

CREATE TABLE retrieval_eval_results (
    run_id TEXT NOT NULL REFERENCES retrieval_eval_runs(id) ON DELETE CASCADE,
    case_id TEXT NOT NULL REFERENCES retrieval_eval_cases(id) ON DELETE RESTRICT,
    document_id TEXT NOT NULL,
    rank INTEGER CHECK (rank IS NULL OR rank >= 1),
    PRIMARY KEY(run_id, case_id, document_id)
);

CREATE TABLE run_closures (
    run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,
    stop_reason TEXT NOT NULL CHECK (stop_reason IN (
        'GOAL_REACHED', 'DISCUSSION_REQUIRED', 'NEEDS_INPUT',
        'ENVIRONMENT_BLOCKED', 'POLICY_BLOCKED', 'LIMIT_REACHED',
        'PROCESS_FAILED', 'CANCELLED', 'TIMED_OUT', 'LOST'
    )),
    summary TEXT NOT NULL CHECK (length(trim(summary)) BETWEEN 1 AND 4000),
    completed_json TEXT NOT NULL CHECK (json_valid(completed_json)),
    verified_json TEXT NOT NULL CHECK (json_valid(verified_json)),
    unverified_json TEXT NOT NULL CHECK (json_valid(unverified_json)),
    remaining_risks_json TEXT NOT NULL CHECK (json_valid(remaining_risks_json)),
    next_action TEXT NOT NULL CHECK (length(next_action) <= 2000),
    created_at TEXT NOT NULL
);

CREATE TABLE objectives (
    id TEXT PRIMARY KEY,
    objective_key TEXT NOT NULL UNIQUE,
    root_topic_id TEXT NOT NULL REFERENCES topics(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('ACTIVE', 'PAUSED', 'ACHIEVED', 'CANCELLED')),
    current_revision_id TEXT REFERENCES objective_revisions(id) ON DELETE RESTRICT,
    max_continuations INTEGER NOT NULL CHECK (max_continuations BETWEEN 1 AND 100),
    continuation_count INTEGER NOT NULL DEFAULT 0 CHECK (continuation_count BETWEEN 0 AND max_continuations),
    version INTEGER NOT NULL CHECK (version >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    completed_at TEXT
);

CREATE TABLE objective_revisions (
    id TEXT PRIMARY KEY,
    objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE RESTRICT,
    revision_number INTEGER NOT NULL CHECK (revision_number >= 1),
    statement TEXT NOT NULL CHECK (length(trim(statement)) BETWEEN 1 AND 4000),
    scope TEXT NOT NULL CHECK (length(scope) <= 4000),
    constraints_json TEXT NOT NULL CHECK (json_valid(constraints_json)),
    completion_criteria_json TEXT NOT NULL CHECK (json_valid(completion_criteria_json)),
    previous_revision_id TEXT REFERENCES objective_revisions(id) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    UNIQUE(objective_id, revision_number)
);

CREATE TABLE objective_checkpoints (
    id TEXT PRIMARY KEY,
    objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    source_run_id TEXT REFERENCES runs(id) ON DELETE RESTRICT,
    summary TEXT NOT NULL CHECK (length(trim(summary)) BETWEEN 1 AND 4000),
    criteria_json TEXT NOT NULL CHECK (json_valid(criteria_json)),
    completed_json TEXT NOT NULL CHECK (json_valid(completed_json)),
    remaining_json TEXT NOT NULL CHECK (json_valid(remaining_json)),
    risks_json TEXT NOT NULL CHECK (json_valid(risks_json)),
    stop_reason TEXT NOT NULL CHECK (stop_reason IN (
        'PROGRESS', 'NEEDS_INPUT', 'REVIEW_REQUIRED',
        'LIMIT_REACHED', 'READY_TO_COMPLETE', 'NO_PROGRESS'
    )),
    next_action TEXT NOT NULL CHECK (length(next_action) <= 2000),
    created_at TEXT NOT NULL,
    UNIQUE(objective_id, sequence)
);

CREATE TABLE objective_events (
    id TEXT PRIMARY KEY,
    objective_id TEXT NOT NULL REFERENCES objectives(id) ON DELETE RESTRICT,
    sequence INTEGER NOT NULL CHECK (sequence >= 1),
    event_type TEXT NOT NULL CHECK (event_type IN ('OBJECTIVE_CREATED', 'OBJECTIVE_STATUS_CHANGED', 'OBJECTIVE_CHECKPOINTED')),
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json)),
    occurred_at TEXT NOT NULL,
    UNIQUE(objective_id, sequence)
);

CREATE TABLE "search_documents" (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN (
        'TOPIC', 'TASK', 'MESSAGE', 'PLAN_REVISION', 'CLARIFICATION',
        'DECISION', 'RUN_SUMMARY', 'CI_CHECK', 'LABEL', 'EXPERIENCE',
        'OBJECTIVE', 'CHECKPOINT'
    )),
    source_id TEXT NOT NULL,
    subject_kind TEXT NOT NULL CHECK (subject_kind IN ('TOPIC', 'TASK')),
    subject_id TEXT NOT NULL,
    stable_key TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    status TEXT NOT NULL,
    is_current INTEGER NOT NULL CHECK (is_current IN (0, 1)),
    updated_at TEXT NOT NULL,
    UNIQUE(kind, source_id)
);

CREATE VIRTUAL TABLE search_documents_fts USING fts5(
    title, body, stable_key,
    content = 'search_documents', content_rowid = 'rowid', tokenize = 'trigram'
);

CREATE INDEX topics_activity_idx
ON topics(status, updated_at DESC);

CREATE UNIQUE INDEX threads_topic_unique_idx
ON threads(topic_id) WHERE topic_id IS NOT NULL;

CREATE UNIQUE INDEX threads_task_unique_idx
ON threads(task_id) WHERE task_id IS NOT NULL;

CREATE INDEX messages_thread_timeline_idx
ON messages(thread_id, sequence);

CREATE INDEX artifacts_created_idx
ON artifacts(created_at DESC);

CREATE INDEX message_task_links_task_idx
ON message_task_links(task_id, created_at);

CREATE INDEX topic_task_links_task_idx
ON topic_task_links(task_id, created_at);

CREATE INDEX workspaces_state_idx ON workspaces(state, updated_at);

CREATE INDEX releases_created_idx ON releases(created_at DESC);

CREATE INDEX release_tasks_task_idx ON release_tasks(task_id, created_at);

CREATE INDEX task_reviews_task_idx ON task_reviews(task_id, created_at DESC);

CREATE INDEX tasks_board_order_idx
ON tasks(status, priority ASC, created_at ASC);

CREATE INDEX agent_profile_revisions_history_idx
ON agent_profile_revisions(profile_id, revision DESC);

CREATE INDEX task_estimates_history_idx ON task_estimates(task_id, revision DESC);

CREATE INDEX task_test_cases_order_idx ON task_test_cases(task_id, sort_order, created_at);

CREATE INDEX task_test_results_latest_idx
ON task_test_results(test_case_id, created_at DESC, id DESC);

CREATE INDEX run_artifacts_run_idx ON run_artifacts(run_id, kind);

CREATE INDEX task_assessments_current_idx
ON task_assessments(task_id, revision DESC);

CREATE UNIQUE INDEX runs_one_active_task_idx
ON runs(task_id) WHERE task_id IS NOT NULL AND status IN ('CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING');

CREATE UNIQUE INDEX runs_one_active_topic_idx
ON runs(topic_id) WHERE topic_id IS NOT NULL AND status IN ('CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING');

CREATE INDEX runs_status_queue_idx ON runs(status, queued_at);

CREATE INDEX runs_task_history_idx ON runs(task_id, created_at DESC);

CREATE INDEX plan_revisions_history_idx ON plan_revisions(plan_id, revision_number DESC);

CREATE INDEX plan_reviews_history_idx ON plan_reviews(plan_id, created_at DESC);

CREATE UNIQUE INDEX tasks_source_plan_draft_unique_idx
ON tasks(source_plan_task_draft_id) WHERE source_plan_task_draft_id IS NOT NULL;

CREATE INDEX run_usage_captured_idx ON run_usage(captured_at DESC);

CREATE INDEX run_events_timeline_idx ON run_events(run_id, sequence);

CREATE UNIQUE INDEX agent_sessions_active_task_idx
ON agent_sessions(task_id, profile_revision_id)
WHERE task_id IS NOT NULL AND status = 'ACTIVE';

CREATE UNIQUE INDEX agent_sessions_active_topic_idx
ON agent_sessions(topic_id, profile_revision_id)
WHERE topic_id IS NOT NULL AND status = 'ACTIVE';

CREATE UNIQUE INDEX agent_sessions_external_idx
ON agent_sessions(adapter, external_session_id);

CREATE UNIQUE INDEX approval_requests_one_open_run_idx
ON approval_requests(run_id) WHERE status = 'OPEN';

CREATE INDEX approval_requests_open_idx
ON approval_requests(status, created_at);

CREATE INDEX topic_events_timeline_idx ON topic_events(topic_id, sequence);

CREATE INDEX task_feedback_queue_idx ON task_feedback_turns(status, created_at);

CREATE INDEX task_feedback_task_idx ON task_feedback_turns(task_id, created_at DESC);

CREATE INDEX task_feedback_events_timeline_idx
ON task_feedback_events(task_id, sequence);

CREATE UNIQUE INDEX task_integration_one_running_idx
ON task_integration_attempts(task_id) WHERE status = 'RUNNING';

CREATE INDEX task_integration_task_history_idx
ON task_integration_attempts(task_id, created_at DESC, id DESC);

CREATE INDEX mcp_call_events_call_idx ON mcp_call_events(call_id, occurred_at, id);

CREATE INDEX mcp_call_events_recent_idx ON mcp_call_events(occurred_at DESC, id DESC);

CREATE INDEX task_relations_target_idx ON task_relations(target_task_id, created_at);

CREATE INDEX task_events_timeline_idx ON task_events(task_id, sequence);

CREATE INDEX tasks_active_board_idx ON tasks(archived_at, status, priority, created_at);

CREATE INDEX decisions_topic_idx ON decisions(topic_id, created_at DESC);

CREATE INDEX decisions_task_idx ON decisions(task_id, created_at DESC);

CREATE INDEX ci_check_snapshots_task_idx ON ci_check_snapshots(task_id, observed_at DESC, id DESC);

CREATE UNIQUE INDEX clarifications_one_open_topic_idx ON clarifications(topic_id) WHERE topic_id IS NOT NULL AND status = 'OPEN';

CREATE UNIQUE INDEX clarifications_one_open_task_idx ON clarifications(task_id) WHERE task_id IS NOT NULL AND status = 'OPEN';

CREATE INDEX clarifications_open_queue_idx ON clarifications(status, created_at);

CREATE INDEX mcp_call_events_run_idx ON mcp_call_events(run_id, occurred_at, id);

CREATE INDEX run_resource_leases_active_idx ON run_resource_leases(state, run_id);

CREATE INDEX experience_records_topic_idx ON experience_records(topic_id, status, updated_at DESC);

CREATE INDEX experience_records_task_idx ON experience_records(task_id, status, updated_at DESC);

CREATE INDEX experience_records_recall_idx ON experience_records(status, project_wide, pinned, updated_at DESC);

CREATE INDEX experience_recalls_run_idx ON experience_recalls(run_id, rank);

CREATE INDEX experience_recalls_experience_idx ON experience_recalls(experience_id, recalled_at DESC);

CREATE UNIQUE INDEX experience_records_run_candidate_idx
ON experience_records(source_run_id, candidate_fingerprint)
WHERE source_run_id IS NOT NULL AND candidate_fingerprint != '';

CREATE INDEX retrieval_eval_cases_active_idx ON retrieval_eval_cases(active, updated_at DESC, id DESC);

CREATE INDEX retrieval_eval_runs_recent_idx ON retrieval_eval_runs(created_at DESC, id DESC);

CREATE UNIQUE INDEX objectives_one_current_idx ON objectives((1)) WHERE status IN ('ACTIVE', 'PAUSED');

CREATE INDEX objectives_topic_idx ON objectives(root_topic_id, created_at DESC);

CREATE INDEX objective_revisions_history_idx ON objective_revisions(objective_id, revision_number DESC);

CREATE INDEX objective_checkpoints_history_idx ON objective_checkpoints(objective_id, sequence DESC);

CREATE INDEX search_documents_subject_idx ON search_documents(subject_kind, subject_id, updated_at DESC);

CREATE INDEX search_documents_filter_idx ON search_documents(kind, status, is_current, updated_at DESC);

CREATE TRIGGER search_topics_ai AFTER INSERT ON topics BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    VALUES ('TOPIC:' || new.id, 'TOPIC', new.id, 'TOPIC', new.id, new.topic_key, new.title, new.description, new.status, 1, new.updated_at);
END;

CREATE TRIGGER search_topics_ad AFTER DELETE ON topics BEGIN
    DELETE FROM search_documents WHERE id = 'TOPIC:' || old.id;
END;

CREATE TRIGGER search_tasks_ai AFTER INSERT ON tasks BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    VALUES ('TASK:' || new.id, 'TASK', new.id, 'TASK', new.id, new.task_key, new.title,
            new.description || char(10) || new.acceptance_criteria, new.status, 1, new.updated_at);
END;

CREATE TRIGGER search_tasks_ad AFTER DELETE ON tasks BEGIN
    DELETE FROM search_documents WHERE id = 'TASK:' || old.id;
END;

CREATE TRIGGER search_messages_ai AFTER INSERT ON messages BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    SELECT 'MESSAGE:' || new.id, 'MESSAGE', new.id,
           CASE WHEN threads.topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END,
           COALESCE(threads.topic_id, threads.task_id),
           COALESCE(topics.topic_key, tasks.task_key) || '#message-' || new.sequence,
           CASE new.author_kind WHEN 'HUMAN' THEN '你的消息' WHEN 'AGENT' THEN 'Agent 消息' ELSE '系统消息' END,
           new.content, new.author_kind, 1, new.created_at
    FROM threads LEFT JOIN topics ON topics.id = threads.topic_id
    LEFT JOIN tasks ON tasks.id = threads.task_id WHERE threads.id = new.thread_id;
END;

CREATE TRIGGER search_messages_ad AFTER DELETE ON messages BEGIN
    DELETE FROM search_documents WHERE id = 'MESSAGE:' || old.id;
END;

CREATE TRIGGER search_plan_revisions_ai AFTER INSERT ON plan_revisions BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    SELECT 'PLAN_REVISION:' || new.id, 'PLAN_REVISION', new.id, 'TOPIC', plans.topic_id,
           plans.plan_key || '-R' || new.revision_number,
           plans.plan_key || ' Revision ' || new.revision_number,
           new.summary || char(10) || new.rationale || char(10) || new.risks,
           plans.status, CASE WHEN plans.current_revision_id = new.id THEN 1 ELSE 0 END, plans.updated_at
    FROM plans WHERE plans.id = new.plan_id;
END;

CREATE TRIGGER search_plan_revisions_ad AFTER DELETE ON plan_revisions BEGIN
    DELETE FROM search_documents WHERE id = 'PLAN_REVISION:' || old.id;
END;

CREATE TRIGGER search_plans_au AFTER UPDATE OF status, current_revision_id, updated_at ON plans BEGIN
    UPDATE search_documents SET status = new.status,
        is_current = CASE WHEN source_id = new.current_revision_id THEN 1 ELSE 0 END,
        updated_at = new.updated_at
    WHERE kind = 'PLAN_REVISION' AND source_id IN (SELECT id FROM plan_revisions WHERE plan_id = new.id);
END;

CREATE TRIGGER search_plan_drafts_ai AFTER INSERT ON plan_task_drafts BEGIN
    UPDATE search_documents SET body = body || char(10) || new.title || char(10) || new.description || char(10) || new.acceptance_criteria
    WHERE id = 'PLAN_REVISION:' || new.plan_revision_id;
END;

CREATE TRIGGER search_plan_drafts_au AFTER UPDATE OF title, description, acceptance_criteria ON plan_task_drafts BEGIN
    UPDATE search_documents SET body = (
        SELECT revisions.summary || char(10) || revisions.rationale || char(10) || revisions.risks || char(10) ||
               COALESCE(group_concat(drafts.title || char(10) || drafts.description || char(10) || drafts.acceptance_criteria, char(10)), '')
        FROM plan_revisions AS revisions LEFT JOIN plan_task_drafts AS drafts ON drafts.plan_revision_id = revisions.id
        WHERE revisions.id = new.plan_revision_id GROUP BY revisions.id
    ) WHERE id = 'PLAN_REVISION:' || new.plan_revision_id;
END;

CREATE TRIGGER search_plan_drafts_ad AFTER DELETE ON plan_task_drafts BEGIN
    UPDATE search_documents SET body = (
        SELECT revisions.summary || char(10) || revisions.rationale || char(10) || revisions.risks || char(10) ||
               COALESCE(group_concat(drafts.title || char(10) || drafts.description || char(10) || drafts.acceptance_criteria, char(10)), '')
        FROM plan_revisions AS revisions LEFT JOIN plan_task_drafts AS drafts ON drafts.plan_revision_id = revisions.id
        WHERE revisions.id = old.plan_revision_id GROUP BY revisions.id
    ) WHERE id = 'PLAN_REVISION:' || old.plan_revision_id;
END;

CREATE TRIGGER search_topics_au_content AFTER UPDATE OF title, description ON topics BEGIN
    UPDATE search_documents SET title = new.title, body = new.description
    WHERE id = 'TOPIC:' || new.id;
END;

CREATE TRIGGER search_topics_au_metadata AFTER UPDATE OF status, updated_at ON topics BEGIN
    UPDATE search_documents SET status = new.status, updated_at = new.updated_at
    WHERE id = 'TOPIC:' || new.id;
END;

CREATE TRIGGER search_tasks_au_content AFTER UPDATE OF title, description, acceptance_criteria ON tasks BEGIN
    UPDATE search_documents SET title = new.title,
        body = new.description || char(10) || new.acceptance_criteria
    WHERE id = 'TASK:' || new.id;
END;

CREATE TRIGGER search_tasks_au_metadata AFTER UPDATE OF status, updated_at ON tasks BEGIN
    UPDATE search_documents SET status = new.status, updated_at = new.updated_at
    WHERE id = 'TASK:' || new.id;
END;

CREATE TRIGGER search_clarifications_ai AFTER INSERT ON clarifications BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    VALUES ('CLARIFICATION:' || new.id, 'CLARIFICATION', new.id,
            CASE WHEN new.topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END,
            COALESCE(new.topic_id, new.task_id),
            COALESCE((SELECT topic_key FROM topics WHERE id = new.topic_id),
                     (SELECT task_key FROM tasks WHERE id = new.task_id)) || '#clarification-' || new.id,
            COALESCE((SELECT topic_key FROM topics WHERE id = new.topic_id),
                     (SELECT task_key FROM tasks WHERE id = new.task_id)) || ' · ' || new.category,
            new.question || char(10) || new.options_json || char(10) || new.selected_option_id || char(10) || new.custom_answer,
            new.status, 1, new.updated_at);
END;

CREATE TRIGGER search_clarifications_au AFTER UPDATE OF question, options_json, selected_option_id, custom_answer, status, updated_at ON clarifications BEGIN
    UPDATE search_documents SET
        body = new.question || char(10) || new.options_json || char(10) || new.selected_option_id || char(10) || new.custom_answer,
        status = new.status, updated_at = new.updated_at
    WHERE id = 'CLARIFICATION:' || new.id;
END;

CREATE TRIGGER search_clarifications_ad AFTER DELETE ON clarifications BEGIN
    DELETE FROM search_documents WHERE id = 'CLARIFICATION:' || old.id;
END;

CREATE TRIGGER search_decisions_ai AFTER INSERT ON decisions BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    VALUES ('DECISION:' || new.id, 'DECISION', new.id,
            CASE WHEN new.topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END,
            COALESCE(new.topic_id, new.task_id), new.decision_key, new.title, new.content,
            new.status, CASE WHEN new.status = 'ACTIVE' THEN 1 ELSE 0 END, new.created_at);
END;

CREATE TRIGGER search_decisions_au AFTER UPDATE OF status ON decisions BEGIN
    UPDATE search_documents SET status = new.status,
        is_current = CASE WHEN new.status = 'ACTIVE' THEN 1 ELSE 0 END
    WHERE id = 'DECISION:' || new.id;
END;

CREATE TRIGGER search_decisions_ad AFTER DELETE ON decisions BEGIN
    DELETE FROM search_documents WHERE id = 'DECISION:' || old.id;
END;

CREATE TRIGGER search_run_summaries_ai AFTER INSERT ON run_summaries BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    SELECT 'RUN_SUMMARY:' || new.run_id, 'RUN_SUMMARY', new.run_id,
           CASE WHEN runs.topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END,
           COALESCE(runs.topic_id, runs.task_id), runs.id,
           runs.purpose || ' Run 摘要', new.summary, new.status, 1, new.created_at
    FROM runs WHERE runs.id = new.run_id;
END;

CREATE TRIGGER search_run_summaries_au AFTER UPDATE OF status, summary, created_at ON run_summaries BEGIN
    UPDATE search_documents SET body = new.summary, status = new.status, updated_at = new.created_at
    WHERE id = 'RUN_SUMMARY:' || new.run_id;
END;

CREATE TRIGGER search_run_summaries_ad AFTER DELETE ON run_summaries BEGIN
    DELETE FROM search_documents WHERE id = 'RUN_SUMMARY:' || old.run_id;
END;

CREATE TRIGGER search_ci_snapshots_ai AFTER INSERT ON ci_check_snapshots BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    VALUES ('CI_CHECK:' || new.id, 'CI_CHECK', new.id, 'TASK', new.task_id,
            new.commit_sha, new.provider || ' CI', new.checks_json, new.state, 1, new.observed_at);
END;

CREATE TRIGGER search_ci_snapshots_ad AFTER DELETE ON ci_check_snapshots BEGIN
    DELETE FROM search_documents WHERE id = 'CI_CHECK:' || old.id;
END;

CREATE TRIGGER search_topic_labels_ai AFTER INSERT ON topic_labels BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    SELECT 'LABEL:TOPIC:' || new.topic_id || ':' || labels.id, 'LABEL', new.topic_id || ':' || labels.id,
           'TOPIC', new.topic_id, labels.name, labels.name, labels.color, 'ATTACHED', 1, new.created_at
    FROM labels WHERE labels.id = new.label_id;
END;

CREATE TRIGGER search_topic_labels_ad AFTER DELETE ON topic_labels BEGIN
    DELETE FROM search_documents WHERE id = 'LABEL:TOPIC:' || old.topic_id || ':' || old.label_id;
END;

CREATE TRIGGER search_task_labels_ai AFTER INSERT ON task_labels BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    SELECT 'LABEL:TASK:' || new.task_id || ':' || labels.id, 'LABEL', new.task_id || ':' || labels.id,
           'TASK', new.task_id, labels.name, labels.name, labels.color, 'ATTACHED', 1, new.created_at
    FROM labels WHERE labels.id = new.label_id;
END;

CREATE TRIGGER search_task_labels_ad AFTER DELETE ON task_labels BEGIN
    DELETE FROM search_documents WHERE id = 'LABEL:TASK:' || old.task_id || ':' || old.label_id;
END;

CREATE TRIGGER search_experiences_ai AFTER INSERT ON experience_records BEGIN
    INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
    VALUES ('EXPERIENCE:' || new.id, 'EXPERIENCE', new.id,
            CASE WHEN new.topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END,
            COALESCE(new.topic_id, new.task_id), new.experience_key, new.title,
            new.summary || char(10) || new.guidance || char(10) || new.applicability,
            new.status, CASE WHEN new.status = 'ACTIVE' THEN 1 ELSE 0 END, new.updated_at);
END;

CREATE TRIGGER search_experiences_au AFTER UPDATE OF title, summary, guidance, applicability, status, updated_at ON experience_records BEGIN
    UPDATE search_documents SET title = new.title,
        body = new.summary || char(10) || new.guidance || char(10) || new.applicability,
        status = new.status, is_current = CASE WHEN new.status = 'ACTIVE' THEN 1 ELSE 0 END,
        updated_at = new.updated_at
    WHERE id = 'EXPERIENCE:' || new.id;
END;

CREATE TRIGGER search_experiences_ad AFTER DELETE ON experience_records BEGIN
    DELETE FROM search_documents WHERE id = 'EXPERIENCE:' || old.id;
END;

CREATE TRIGGER search_documents_ai AFTER INSERT ON search_documents BEGIN
    INSERT INTO search_documents_fts(rowid, title, body, stable_key)
    VALUES (new.rowid, new.title, new.body, new.stable_key);
END;

CREATE TRIGGER search_documents_ad AFTER DELETE ON search_documents BEGIN
    INSERT INTO search_documents_fts(search_documents_fts, rowid, title, body, stable_key)
    VALUES ('delete', old.rowid, old.title, old.body, old.stable_key);
END;

CREATE TRIGGER search_documents_au AFTER UPDATE OF title, body, stable_key ON search_documents BEGIN
    INSERT INTO search_documents_fts(search_documents_fts, rowid, title, body, stable_key)
    VALUES ('delete', old.rowid, old.title, old.body, old.stable_key);
    INSERT INTO search_documents_fts(rowid, title, body, stable_key)
    VALUES (new.rowid, new.title, new.body, new.stable_key);
END;

CREATE TRIGGER search_objectives_au AFTER UPDATE OF current_revision_id, status, updated_at ON objectives BEGIN
    INSERT OR REPLACE INTO search_documents(
        id, kind, source_id, subject_kind, subject_id, stable_key,
        title, body, status, is_current, updated_at
    ) SELECT 'OBJECTIVE:' || new.id, 'OBJECTIVE', new.id, 'TOPIC', new.root_topic_id,
             new.objective_key, revisions.statement,
             revisions.scope || char(10) || revisions.constraints_json || char(10) || revisions.completion_criteria_json,
             new.status, CASE WHEN new.status IN ('ACTIVE', 'PAUSED') THEN 1 ELSE 0 END, new.updated_at
      FROM objective_revisions AS revisions WHERE revisions.id = new.current_revision_id;
END;

CREATE TRIGGER search_objectives_ad AFTER DELETE ON objectives BEGIN
    DELETE FROM search_documents WHERE id = 'OBJECTIVE:' || old.id;
END;

CREATE TRIGGER search_objective_checkpoints_ai AFTER INSERT ON objective_checkpoints BEGIN
    UPDATE search_documents SET is_current = 0
    WHERE kind = 'CHECKPOINT' AND subject_id = (SELECT root_topic_id FROM objectives WHERE id = new.objective_id);
    INSERT INTO search_documents(
        id, kind, source_id, subject_kind, subject_id, stable_key,
        title, body, status, is_current, updated_at
    ) SELECT 'CHECKPOINT:' || new.id, 'CHECKPOINT', new.id, 'TOPIC', objectives.root_topic_id,
             objectives.objective_key || '#checkpoint-' || new.sequence,
             objectives.objective_key || ' Checkpoint ' || new.sequence,
             new.summary || char(10) || new.criteria_json || char(10) || new.completed_json || char(10) ||
             new.remaining_json || char(10) || new.risks_json || char(10) || new.next_action,
             new.stop_reason, 1, new.created_at
      FROM objectives WHERE objectives.id = new.objective_id;
END;

CREATE TRIGGER search_objective_checkpoints_ad AFTER DELETE ON objective_checkpoints BEGIN
    DELETE FROM search_documents WHERE id = 'CHECKPOINT:' || old.id;
END;`
