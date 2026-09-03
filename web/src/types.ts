export const taskStatuses = [
  'BACKLOG',
  'READY',
  'RUNNING',
  'REVIEW',
  'ACCEPTED',
  'CHANGES_REQUESTED',
  'BLOCKED',
  'CANCELLED',
] as const

export type TaskStatus = (typeof taskStatuses)[number]

export type TaskTitleSource = 'PROVISIONAL' | 'AI' | 'HUMAN'

export const topicStatuses = [
  'OPEN',
  'NEEDS_CLARIFICATION',
  'PLAN_REVIEW',
  'PLANNED',
  'CLOSED',
] as const

export type TopicStatus = (typeof topicStatuses)[number]

export const searchKinds = [
	'TOPIC', 'TASK', 'MESSAGE', 'PLAN_REVISION', 'CLARIFICATION',
	'DECISION', 'RUN_SUMMARY', 'CI_CHECK', 'LABEL',
	'EXPERIENCE',
] as const

export type SearchKind = (typeof searchKinds)[number]

export interface SearchItem {
	document_id: string
	kind: SearchKind
	source_id: string
	subject_kind: 'TOPIC' | 'TASK'
	subject_id: string
	stable_key: string
	title: string
	snippet: string
	status: string
	current: boolean
	updated_at: string
}

export interface SearchPage {
	items: SearchItem[]
	next_cursor?: string
}

export interface SearchQueryInput {
	query: string
	kinds?: SearchKind[]
	statuses?: string[]
	only_current?: boolean
	updated_after?: string
	updated_before?: string
	limit?: number
	cursor?: string
}

export const messageAuthorKinds = ['HUMAN', 'AGENT', 'SYSTEM'] as const

export type MessageAuthorKind = (typeof messageAuthorKinds)[number]

export interface Task {
  id: string
  key: string
  title: string
  title_source: TaskTitleSource
  title_locked: boolean
  description: string
  acceptance_criteria: string
  status: TaskStatus
  priority: number
  target_branch?: string
  base_commit_sha?: string
  current_workspace_id?: string
  latest_run_id?: string
	source_plan_revision_id?: string
	source_plan_task_draft_id?: string
  assessment_input_version: number
  assessment?: TaskAssessment
  assessment_stale?: boolean
  version: number
  created_at: string
  updated_at: string
  archived_at?: string
}

export interface Topic {
  id: string
  key: string
  title: string
  description: string
  status: TopicStatus
  current_plan_id?: string
  current_summary_id?: string
  version: number
  created_at: string
  updated_at: string
}

export type PlanStatus = 'IN_REVIEW' | 'CHANGES_REQUESTED' | 'APPROVED'

export interface PlanTestCaseDraft {
	id: string
	task_draft_id: string
	title: string
	description: string
	required: boolean
	sort_order: number
}

export interface PlanTaskDraft {
	id: string
	plan_revision_id: string
	key: string
	title: string
	description: string
	acceptance_criteria: string
	priority: number
	proposed_order: number
	test_cases: PlanTestCaseDraft[]
}

export interface PlanRevision {
	id: string
	plan_id: string
	revision: number
	summary: string
	rationale: string
	risks: string
	source_run_id?: string
	previous_revision_id?: string
	drafts: PlanTaskDraft[]
	created_at: string
}

export interface Plan {
	id: string
	key: string
	topic_id: string
	status: PlanStatus
	current_revision_id: string
	approved_revision_id?: string
	version: number
	created_at: string
	updated_at: string
}

export interface PlanReview {
	id: string
	plan_id: string
	plan_revision_id: string
	decision: 'APPROVED' | 'CHANGES_REQUESTED'
	comment: string
	created_at: string
}

export interface PlanView {
	plan: Plan
	revision: PlanRevision
	reviews: PlanReview[]
}

export interface PlanTestCaseInput {
	title: string
	description: string
	required: boolean
}

export interface PlanTaskDraftInput {
	title: string
	description: string
	acceptance_criteria: string
	priority: number
	test_cases: PlanTestCaseInput[]
}

export interface CreatePlanRevisionInput {
	summary: string
	rationale: string
	risks: string
	drafts: PlanTaskDraftInput[]
}

export interface DiscussionMessage {
  id: string
  thread_id: string
  sequence: number
  author_kind: MessageAuthorKind
  content: string
  linked_task_ids: string[]
  created_at: string
}

export type TaskFeedbackIntent = 'NOTE' | 'DISCUSS' | 'REQUEST_CHANGES'

export interface TaskFeedback {
	id: string
	task_id: string
	source_message_id: string
	retry_of_feedback_id?: string
	intent: Exclude<TaskFeedbackIntent, 'NOTE'>
	status: 'QUEUED' | 'RUNNING' | 'ANSWERED' | 'APPLIED' | 'FAILED'
	run_id?: string
	response_message_id?: string
	failure_message: string
	created_at: string
	updated_at: string
}

export interface TaskFeedbackEvent {
	id: string
	task_id: string
	feedback_id: string
	sequence: number
	status: TaskFeedback['status']
	run_id?: string
	response_message_id?: string
	failure_message: string
	occurred_at: string
}

export interface TaskFeedbackResponse {
	message: DiscussionMessage
	feedback?: TaskFeedback
	task?: Task
	follow_up_task?: Task
}

export interface TaskAssociation {
  task: Task
  type?: TaskRelationType
  direction?: TaskRelationDirection
  source_message_id?: string
  created_at: string
}

export type TaskRelationType = 'RELATES_TO' | 'BLOCKS' | 'PARENT_OF' | 'SUPERSEDES' | 'DERIVED_FROM'
export type TaskRelationDirection = 'BIDIRECTIONAL' | 'OUTGOING' | 'INCOMING'

export interface UpdateTaskDetailsInput {
	description: string
	acceptance_criteria: string
	priority: number
}

export interface TopicAssociation {
  topic: Topic
  source_message_id?: string
  created_at: string
}

export interface KnowledgeLabel {
	id: string
	name: string
	color: string
	created_at: string
}

export interface Decision {
	id: string
	key: string
	topic_id?: string
	task_id?: string
	title: string
	content: string
	status: 'ACTIVE' | 'SUPERSEDED'
	supersedes_decision_id?: string
	created_at: string
}

export type ExperienceStatus = 'CANDIDATE' | 'ACTIVE' | 'CHALLENGED' | 'SUPERSEDED'

export interface ExperienceRecord {
	id: string
	key: string
	topic_id?: string
	task_id?: string
	title: string
	summary: string
	guidance: string
	applicability: string
	project_wide: boolean
	status: ExperienceStatus
	pinned: boolean
	verification_count: number
	successful_applications: number
	failed_applications: number
	recall_count: number
	source_run_id?: string
	supersedes_experience_id?: string
	created_at: string
	updated_at: string
}

export interface CreateExperienceInput {
	title: string
	summary: string
	guidance: string
	applicability: string
	project_wide: boolean
	pinned: boolean
	supersedes_experience_id?: string
}

export type ExperienceOutcome = 'PENDING' | 'HELPFUL' | 'HARMFUL' | 'IGNORED'

export interface ExperienceRecall {
	recall_id: string
	rank: number
	experience: ExperienceRecord
	score: {
		relevance_score: number
		utility_score: number
		scope_score: number
		freshness_score: number
		final_score: number
	}
	outcome: ExperienceOutcome
	recalled_at: string
}

export type CIState = 'PENDING' | 'PASSED' | 'FAILED' | 'CANCELLED' | 'UNKNOWN'

export interface CICheck {
	name: string
	state: CIState
	details_url?: string
}

export interface CISnapshot {
	id: string
	task_id: string
	provider: string
	commit_sha: string
	state: CIState
	checks: CICheck[]
	source_url?: string
	observed_at: string
	created_at: string
}

export interface RunSummary {
	run_id: string
	status: string
	summary: string
	passed_tests: number
	failed_tests: number
	created_at: string
}

export interface MCPAuditEvent {
	id: string
	call_id: string
	direction: 'INBOUND' | 'OUTBOUND'
	run_id?: string
	client_name: string
	server_name?: string
	tool_name: string
	phase: 'STARTED' | 'COMPLETED' | 'FAILED'
	argument_keys: string[]
	arguments_sha256: string
	result_bytes?: number
	error_message?: string
	occurred_at: string
}

export interface RunResourceLease {
	id: string
	run_id: string
	resource_kind: 'BROWSER_SESSION'
	provider_name: string
	state: 'ACTIVE' | 'RELEASED' | 'ABANDONED'
	opened_at: string
	released_at?: string
	cleanup_reason?: string
}

export interface ProjectInfo {
  name: string
  root: string
  agent: string
	workers_enabled: boolean
  max_workers: number
	scheduler_failures: number
	scheduler_error: string
}

export const runPurposes = ['PLANNING', 'TRIAGE', 'IMPLEMENTATION', 'REVISION', 'REVIEW'] as const
export type RunPurpose = (typeof runPurposes)[number]

export interface PurposeUsage {
	purpose: RunPurpose
	total_runs: number
	runs_with_usage: number
	input_tokens?: number
	cached_input_tokens?: number
	uncached_input_tokens?: number
	cache_write_input_tokens?: number
	output_tokens?: number
	reasoning_output_tokens?: number
	model_requests?: number
	peak_input_tokens?: number
}

export interface RunUsageSummary extends Omit<PurposeUsage, 'purpose'> {
	by_purpose: PurposeUsage[]
}

export const runStatuses = [
	'CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING', 'NEEDS_INPUT',
	'SUCCEEDED', 'FAILED', 'CANCELLED', 'TIMED_OUT', 'LOST',
] as const

export type RunStatus = (typeof runStatuses)[number]

export interface AgentRun {
	id: string
	purpose: RunPurpose
	topic_id?: string
	task_id?: string
	status: RunStatus
	profile_revision_id: string
	subject_version: number
	retry_of_run_id?: string
	continuation_of_run_id?: string
	agent_session_id?: string
	session_resumed: boolean
	lease_generation: number
	lease_expires_at: string
	queued_at: string
	claimed_at: string
	started_at?: string
	finished_at?: string
	exit_code?: number
	failure_kind?: string
	failure_code?: string
	failure_message?: string
	failure_retryable?: boolean
	cancel_requested_at?: string
	cancel_reason?: string
	created_at: string
	updated_at: string
}

export interface RunPage {
	items: AgentRun[]
	next_cursor?: string
}

export interface RunQueryInput {
	active?: boolean
	status?: RunStatus
	purpose?: RunPurpose
	task_id?: string
	topic_id?: string
	limit?: number
	cursor?: string
}

export interface RunEvent {
	id: string
	run_id: string
	sequence: number
	type: string
	payload: unknown
	occurred_at: string
}

export interface RunUsage {
	run_id: string
	input_tokens?: number
	cached_input_tokens?: number
	cache_write_input_tokens?: number
	output_tokens?: number
	reasoning_output_tokens?: number
	model_requests?: number
	peak_input_tokens?: number
	source: string
	captured_at: string
}

export interface RunArtifact {
	id: string
	run_id: string
	kind: string
	relative_path: string
	sha256: string
	size: number
	truncated: boolean
	created_at: string
}

export interface RunWorkspaceSnapshot {
	run_id: string
	workspace_id: string
	branch_name: string
	target_branch: string
	base_commit_sha: string
	head_before: string
	head_after: string
	dirty_before: boolean
	dirty_after: boolean
	state_after: WorkspaceState
	captured_at: string
}

export interface RunDetail {
	run: AgentRun
	usage?: RunUsage
	artifacts: RunArtifact[]
	workspace_snapshot?: RunWorkspaceSnapshot
}

export interface RunLog {
	stream: 'stdout' | 'stderr'
	content: string
	size: number
	truncated: boolean
}

export const agentRoles = ['PLANNER', 'TRIAGER', 'IMPLEMENTER', 'REVISION', 'REVIEWER'] as const
export type AgentRole = (typeof agentRoles)[number]

export const workspacePolicies = ['NONE', 'READ_ONLY', 'WRITE_TASK'] as const
export type WorkspacePolicy = (typeof workspacePolicies)[number]

export const approvalPolicies = ['READ_ONLY', 'WORKSPACE_WRITE'] as const
export type ApprovalPolicy = (typeof approvalPolicies)[number]

export interface ProjectSkill {
	id: string
	name: string
	source_path: string
	content_sha256: string
	enabled: boolean
	version: number
	created_at: string
	updated_at: string
}

export interface ProjectMCPServer {
	id: string
	name: string
	config_name: string
	enabled: boolean
	version: number
	created_at: string
	updated_at: string
}

export interface ProjectCapabilityCatalog {
	skills: ProjectSkill[]
	mcp_servers: ProjectMCPServer[]
}

export interface SkillBinding {
	skill_id: string
	required: boolean
}

export interface MCPServerBinding {
	server_id: string
	required: boolean
	enabled_tools: string[]
}

export interface AgentToolPolicy {
	skills: SkillBinding[]
	mcp_servers: MCPServerBinding[]
}

export interface AgentProfileRevision {
	id: string
	profile_id: string
	revision: number
	instructions: string
	adapter: string
	command: string
	args: string[]
	model: string
	max_input_tokens: number
	reserved_output_tokens: number
	recent_message_limit: number
	retrieval_limit: number
	workspace_policy: WorkspacePolicy
	approval_policy: ApprovalPolicy
	timeout_seconds: number
	tool_policy: AgentToolPolicy
	created_at: string
}

export interface AgentProfile {
	id: string
	name: string
	role: AgentRole
	current_revision: AgentProfileRevision
	created_at: string
	updated_at: string
}

export type ClarificationCategory = 'REQUIREMENT' | 'DECISION' | 'ENVIRONMENT' | 'VALIDATION'
export type ClarificationStatus = 'OPEN' | 'ANSWERED'

export interface ClarificationOption {
	id: string
	label: string
	description: string
}

export interface Clarification {
	id: string
	topic_id?: string
	task_id?: string
	source_run_id: string
	continuation_run_id?: string
	continuation_purpose: 'PLANNING' | 'TRIAGE' | 'IMPLEMENTATION' | 'REVISION'
	category: ClarificationCategory
	question: string
	options: ClarificationOption[]
	recommended_option_id?: string
	allow_custom_answer: boolean
	status: ClarificationStatus
	selected_option_id?: string
	custom_answer?: string
	version: number
	created_at: string
	updated_at: string
	answered_at?: string
}

export interface ClarificationAnswerInput {
	selected_option_id: string
	custom_answer: string
	expected_version: number
}

export type ApprovalKind = 'COMMAND' | 'FILE_CHANGE' | 'NETWORK' | 'PERMISSIONS'
export type ApprovalDecision = 'ACCEPT_ONCE' | 'ACCEPT_SESSION' | 'DECLINE' | 'CANCEL_RUN'

export interface ApprovalRequest {
	id: string
	run_id: string
	task_id?: string
	item_id?: string
	kind: ApprovalKind
	reason?: string
	command?: string
	cwd?: string
	host?: string
	protocol?: string
	grant_root?: string
	available_decisions: ApprovalDecision[]
	status: 'OPEN' | 'RESOLVED' | 'CLEARED'
	decision?: ApprovalDecision
	version: number
	created_at: string
	updated_at: string
	resolved_at?: string
}

export type AgentProfileRevisionInput = Omit<
	AgentProfileRevision,
	'id' | 'profile_id' | 'revision' | 'workspace_policy' | 'approval_policy' | 'created_at'
>

export interface ProjectProgress {
	total_tasks: number
	accepted_tasks: number
	strict_percent: number
	estimated_tasks: number
	estimate_coverage: number
	total_points: number
	remaining_points: number
	forecast_percent?: number
	required_tests: number
	verified_passed_tests: number
	agent_reported_passed_tests: number
}

export type ComplexityLevel = 'C1' | 'C2' | 'C3' | 'C4' | 'C5'
export type AutonomyLevel = 'A0' | 'A1' | 'A2' | 'A3'

export interface AssessmentScores {
	technical_complexity: number
	requirement_uncertainty: number
	change_scope: number
	validation_burden: number
	human_dependency: number
	risk_and_reversibility: number
}

export interface TaskAssessment {
	id: string
	task_id: string
	task_assessment_version: number
	revision: number
	suggested_title: string
	applied_title: string
	scores: AssessmentScores
	weighted_score: number
	complexity: ComplexityLevel
	autonomy: AutonomyLevel
	confidence: number
	rationale: string
	assumptions: string[]
	split_recommended: boolean
	split_rationale: string
	source_run_id: string
	created_at: string
}

export interface TaskAssessmentState {
	current?: TaskAssessment
	history: TaskAssessment[]
	stale: boolean
}

export type EstimateSource = 'AI' | 'HUMAN'

export interface TaskEstimate {
	id: string
	task_id: string
	revision: number
	points: number
	remaining_points: number
	confidence: number
	rationale: string
	source: EstimateSource
	source_run_id?: string
	created_at: string
}

export type TestOutcome = 'PASSED' | 'FAILED' | 'BLOCKED'
export type TestEvidenceKind = 'COMMAND' | 'HUMAN' | 'AGENT_REPORT'

export interface TaskTestResult {
	id: string
	test_case_id: string
	task_id: string
	outcome: TestOutcome
	evidence_kind: TestEvidenceKind
	summary: string
	command?: string
	exit_code?: number
	artifact_ref?: string
	source_run_id?: string
	stale?: boolean
	created_at: string
}

export interface TaskTestCase {
	id: string
	task_id: string
	title: string
	description: string
	required: boolean
	sort_order: number
	created_by: 'HUMAN' | 'AGENT'
	source_run_id?: string
	latest_result?: TaskTestResult
	created_at: string
	updated_at: string
}

export interface TaskQuality {
	estimate?: TaskEstimate
	estimate_history: TaskEstimate[]
	test_cases: TaskTestCase[]
}

export interface CreateTaskEstimateInput {
	points: number
	remaining_points: number
	confidence: number
	rationale: string
}

export interface CreateTaskTestCaseInput {
	title: string
	description: string
	required: boolean
	sort_order: number
}

export interface CreateTaskTestResultInput {
	outcome: TestOutcome
	summary: string
}

export const workspaceStates = ['PROVISIONING', 'READY', 'DIRTY', 'QUARANTINED', 'ERROR'] as const

export type WorkspaceState = (typeof workspaceStates)[number]

export interface Workspace {
  id: string
  task_id: string
  path: string
  branch_name: string
  target_branch: string
  base_commit_sha: string
  head_sha: string
  state: WorkspaceState
  dirty: boolean
  failure_message?: string
  last_verified_at?: string
  created_at: string
  updated_at: string
}

export type TaskIntegrationOperation = 'INTEGRATE' | 'SYNC'
export type TaskIntegrationStatus = 'RUNNING' | 'SUCCEEDED' | 'NEEDS_SYNC' | 'SYNCED' | 'CONFLICT' | 'FAILED'

export interface TaskIntegration {
	id: string
	task_id: string
	review_id: string
	operation: TaskIntegrationOperation
	status: TaskIntegrationStatus
	target_branch: string
	source_commit_sha: string
	target_before_sha: string
	target_after_sha?: string
	workspace_after_sha?: string
	failure_kind?: string
	failure_message?: string
	created_at: string
	updated_at: string
}

export interface GitBranch {
  name: string
  head_sha: string
}

export interface GitRemote {
	name: string
	fetch_url: string
	push_url: string
}

export interface RepositoryInfo {
	root: string
	git_common_dir: string
	git_version: string
	default_branch: string
	remote_default_branch?: string
  current_branch: string
  head_sha: string
	has_head: boolean
  dirty: boolean
  exact_tag?: string
	upstream?: string
	ahead?: number
	behind?: number
	user_name?: string
	user_email?: string
	identity_configured: boolean
  branches: GitBranch[]
	remotes: GitRemote[]
}

export interface ChangedFile {
  path: string
  status: string
  additions: number
  deletions: number
  binary: boolean
}

export interface TaskChanges {
  base_commit_sha: string
  head_sha: string
  dirty: boolean
  file_count: number
  additions: number
  deletions: number
  files: ChangedFile[]
}

export interface FileDiff {
  path: string
  patch: string
  truncated: boolean
  binary: boolean
}

export interface TaskReview {
  id: string
  task_id: string
  decision: 'ACCEPTED' | 'REJECTED'
  comment: string
  commit_sha?: string
  created_at: string
}

export const releaseStatuses = ['CREATING', 'TAGGED', 'FAILED'] as const

export type ReleaseStatus = (typeof releaseStatuses)[number]

export interface Release {
  id: string
  version: string
  tag_name: string
  source_branch: string
  commit_sha: string
  status: ReleaseStatus
  failure_message?: string
  task_ids: string[]
  created_at: string
  updated_at: string
  tagged_at?: string
}

export interface CreateReleaseInput {
  version: string
  source_branch: string
  task_ids: string[]
}

export interface CreateTaskInput {
  title: string
  description: string
  acceptance_criteria: string
  priority: number
	target_branch?: string
}

export interface CreateTopicInput {
  title: string
  description: string
}

export interface CreateMessageInput {
  content: string
  linked_task_ids: string[]
}

export type DiscussionSubjectKind = 'topics' | 'tasks'

export interface UploadedImage {
  id: string
  markdown: string
  original_url: string
  optimized_url: string
}
