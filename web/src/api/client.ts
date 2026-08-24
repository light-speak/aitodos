import { messageAuthorKinds, releaseStatuses, runPurposes, runStatuses, taskStatuses, topicStatuses, workspaceStates } from '../types'
import type {
  CreateTaskInput,
  CreateTopicInput,
  CreateMessageInput,
  DiscussionMessage,
  DiscussionSubjectKind,
  ProjectInfo,
  Task,
  TaskAssociation,
  TaskStatus,
  Topic,
  TopicAssociation,
  MessageAuthorKind,
  TopicStatus,
  UploadedImage,
  CreateReleaseInput,
  Release,
  ReleaseStatus,
  RepositoryInfo,
  Workspace,
  WorkspaceState,
	TaskChanges,
	FileDiff,
  TaskReview,
	AgentProfile,
	AgentProfileRevisionInput,
	ProjectProgress,
	TaskQuality,
	TaskEstimate,
	TaskTestCase,
	TaskTestResult,
	CreateTaskEstimateInput,
	CreateTaskTestCaseInput,
	CreateTaskTestResultInput,
	TaskAssessment,
	TaskAssessmentState,
	Clarification,
  ClarificationAnswerInput,
	PlanView,
	CreatePlanRevisionInput,
	ProjectCapabilityCatalog,
	ProjectSkill,
	ProjectMCPServer,
	RunUsageSummary,
	AgentRun,
	RunArtifact,
	RunDetail,
	RunLog,
	RunUsage,
	RunWorkspaceSnapshot,
	RunPage,
	RunQueryInput,
	ApprovalRequest,
	ApprovalDecision,
} from '../types'

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  body?: unknown
  signal?: AbortSignal
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(message: string, status: number, code: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

export async function getProject(signal?: AbortSignal): Promise<ProjectInfo> {
  return parseProject(await requestJSON('/api/project', { signal }))
}

export async function updateWorkerSettings(enabled: boolean, maxWorkers: number): Promise<ProjectInfo> {
	return parseProject(await requestJSON('/api/project/workers', {
		method: 'POST', body: { enabled, max_workers: maxWorkers },
	}))
}

export async function getProjectProgress(signal?: AbortSignal): Promise<ProjectProgress> {
	return parseProjectProgress(await requestJSON('/api/progress', { signal }))
}

export async function getRunUsageSummary(signal?: AbortSignal): Promise<RunUsageSummary> {
	return parseRunUsageSummary(await requestJSON('/api/runs/usage', { signal }))
}

export async function getTaskRuns(taskID: string, signal?: AbortSignal): Promise<AgentRun[]> {
	const value = await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/runs`, { signal })
	if (!Array.isArray(value)) throw new ApiError('Run 历史格式无效', 502, 'INVALID_RESPONSE')
	return value.map(parseAgentRun)
}

export async function getRuns(query: RunQueryInput, signal?: AbortSignal): Promise<RunPage> {
	const parameters = new URLSearchParams()
	if (query.active) parameters.set('active', 'true')
	if (query.status) parameters.set('status', query.status)
	if (query.purpose) parameters.set('purpose', query.purpose)
	if (query.task_id) parameters.set('task_id', query.task_id)
	if (query.topic_id) parameters.set('topic_id', query.topic_id)
	if (query.limit) parameters.set('limit', String(query.limit))
	if (query.cursor) parameters.set('cursor', query.cursor)
	const suffix = parameters.size > 0 ? `?${parameters.toString()}` : ''
	return parseRunPage(await requestJSON(`/api/runs${suffix}`, { signal }))
}

export async function requestRunCancellation(runID: string, reason: string): Promise<AgentRun> {
	return parseAgentRun(await requestJSON(`/api/runs/${encodeURIComponent(runID)}/cancel`, {
		method: 'POST', body: { reason },
	}))
}

export async function retryTask(taskID: string, expectedVersion: number): Promise<Task> {
	return parseTask(await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/retry`, {
		method: 'POST', body: { expected_version: expectedVersion },
	}))
}

export async function getOpenApprovalRequests(signal?: AbortSignal): Promise<ApprovalRequest[]> {
	const value = await requestJSON('/api/approvals', { signal })
	if (!Array.isArray(value)) throw new ApiError('权限请求格式无效', 502, 'INVALID_RESPONSE')
	return value.map(parseApprovalRequest)
}

export async function decideApprovalRequest(
	approvalID: string,
	decision: ApprovalDecision,
	expectedVersion: number,
): Promise<ApprovalRequest> {
	return parseApprovalRequest(await requestJSON(`/api/approvals/${encodeURIComponent(approvalID)}/decision`, {
		method: 'POST', body: { decision, expected_version: expectedVersion },
	}))
}

export async function getRunDetail(runID: string, signal?: AbortSignal): Promise<RunDetail> {
	return parseRunDetail(await requestJSON(`/api/runs/${encodeURIComponent(runID)}`, { signal }))
}

export async function getRunLog(runID: string, stream: RunLog['stream'], signal?: AbortSignal): Promise<RunLog> {
	return parseRunLog(await requestJSON(`/api/runs/${encodeURIComponent(runID)}/logs?stream=${stream}`, { signal }))
}

export async function getAgentProfiles(signal?: AbortSignal): Promise<AgentProfile[]> {
	const value = await requestJSON('/api/agent-profiles', { signal })
	if (!Array.isArray(value)) throw new ApiError('Agent 配置格式无效', 502, 'INVALID_RESPONSE')
	return value.map(parseAgentProfile)
}

export async function createAgentProfileRevision(profileID: string, input: AgentProfileRevisionInput): Promise<AgentProfile> {
	return parseAgentProfile(await requestJSON(`/api/agent-profiles/${encodeURIComponent(profileID)}/revisions`, {
		method: 'POST', body: input,
	}))
}

export async function getProjectCapabilities(signal?: AbortSignal): Promise<ProjectCapabilityCatalog> {
	return parseProjectCapabilities(await requestJSON('/api/project/capabilities', { signal }))
}

export async function addProjectSkill(input: { name: string; source_path: string }): Promise<ProjectSkill> {
	return parseProjectSkill(await requestJSON('/api/project/capabilities/skills', { method: 'POST', body: input }))
}

export async function refreshProjectSkill(skillID: string, version: number): Promise<ProjectSkill> {
	return parseProjectSkill(await requestJSON(`/api/project/capabilities/skills/${encodeURIComponent(skillID)}/refresh`, {
		method: 'POST', body: { version },
	}))
}

export async function addProjectMCPServer(input: { name: string; config_name: string }): Promise<ProjectMCPServer> {
	return parseProjectMCPServer(await requestJSON('/api/project/capabilities/mcp-servers', { method: 'POST', body: input }))
}

export async function getOpenClarifications(signal?: AbortSignal): Promise<Clarification[]> {
	return parseClarifications(await requestJSON('/api/clarifications', { signal }))
}

export async function getTaskClarifications(taskID: string, signal?: AbortSignal): Promise<Clarification[]> {
	return parseClarifications(await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/clarifications`, { signal }))
}

export async function answerClarification(id: string, input: ClarificationAnswerInput): Promise<{ clarification: Clarification; task: Task }> {
	const value = await requestJSON(`/api/clarifications/${encodeURIComponent(id)}/answer`, { method: 'POST', body: input })
	if (!isRecord(value)) throw new ApiError('问题回答结果格式无效', 502, 'INVALID_RESPONSE')
	return { clarification: parseClarification(value.clarification), task: parseTask(value.task) }
}

export async function getTaskQuality(taskID: string, signal?: AbortSignal): Promise<TaskQuality> {
	return parseTaskQuality(await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/quality`, { signal }))
}

export async function getTaskAssessment(taskID: string, signal?: AbortSignal): Promise<TaskAssessmentState> {
	return parseTaskAssessmentState(await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/assessment`, { signal }))
}

export async function updateTaskTitle(taskID: string, title: string, expectedVersion: number): Promise<Task> {
	return parseTask(await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/title`, {
		method: 'PUT', body: { title, expected_version: expectedVersion },
	}))
}

export async function updateTaskTargetBranch(taskID: string, targetBranch: string, expectedVersion: number): Promise<Task> {
	return parseTask(await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/target-branch`, {
		method: 'PUT', body: { target_branch: targetBranch, expected_version: expectedVersion },
	}))
}

export async function createTaskEstimate(taskID: string, input: CreateTaskEstimateInput): Promise<TaskEstimate> {
	return parseTaskEstimate(await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/estimates`, { method: 'POST', body: input }))
}

export async function createTaskTestCase(taskID: string, input: CreateTaskTestCaseInput): Promise<TaskTestCase> {
	return parseTaskTestCase(await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/test-cases`, { method: 'POST', body: input }))
}

export async function createTaskTestResult(taskID: string, testCaseID: string, input: CreateTaskTestResultInput): Promise<TaskTestResult> {
	return parseTaskTestResult(await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/test-cases/${encodeURIComponent(testCaseID)}/results`, { method: 'POST', body: input }))
}

export async function getTasks(signal?: AbortSignal): Promise<Task[]> {
  const payload = await requestJSON('/api/tasks', { signal })
  if (!Array.isArray(payload)) {
    throw new ApiError('Task 列表格式无效', 502, 'INVALID_RESPONSE')
  }
  return payload.map(parseTask)
}

export async function getTask(taskID: string, signal?: AbortSignal): Promise<Task> {
	return parseTask(await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}`, { signal }))
}

export async function createTask(input: CreateTaskInput): Promise<Task> {
  return parseTask(await requestJSON('/api/tasks', { method: 'POST', body: input }))
}

export async function getTopics(signal?: AbortSignal): Promise<Topic[]> {
  const payload = await requestJSON('/api/topics', { signal })
  if (!Array.isArray(payload)) {
    throw new ApiError('Topic 列表格式无效', 502, 'INVALID_RESPONSE')
  }
  return payload.map(parseTopic)
}

export async function createTopic(input: CreateTopicInput): Promise<Topic> {
  return parseTopic(await requestJSON('/api/topics', { method: 'POST', body: input }))
}

export async function requestTopicPlanning(topicID: string, expectedVersion: number): Promise<Topic> {
	return parseTopic(await requestJSON(`/api/topics/${encodeURIComponent(topicID)}/planning`, {
		method: 'POST', body: { expected_version: expectedVersion },
	}))
}

export async function getTopicPlan(topicID: string, signal?: AbortSignal): Promise<PlanView | null> {
	const value = await requestJSON(`/api/topics/${encodeURIComponent(topicID)}/plans/current`, { signal })
	return value === null ? null : parsePlanView(value)
}

export async function createTopicPlan(topic: Topic, input: CreatePlanRevisionInput): Promise<PlanView> {
	return parsePlanView(await requestJSON(`/api/topics/${encodeURIComponent(topic.id)}/plans`, {
		method: 'POST', body: { ...input, expected_topic_version: topic.version },
	}))
}

export async function requestPlanChanges(topic: Topic, current: PlanView, comment: string): Promise<PlanView> {
	return parsePlanView(await requestJSON(`/api/plans/${encodeURIComponent(current.plan.id)}/request-changes`, {
		method: 'POST', body: {
			expected_topic_version: topic.version, revision_id: current.revision.id, comment,
		},
	}))
}

export async function approvePlan(topic: Topic, current: PlanView, comment: string): Promise<{ plan: PlanView; tasks: Task[] }> {
	const value = await requestJSON(`/api/plans/${encodeURIComponent(current.plan.id)}/approve`, {
		method: 'POST', body: {
			expected_topic_version: topic.version, revision_id: current.revision.id, comment,
		},
	})
	if (!isRecord(value) || !Array.isArray(value.tasks)) {
		throw new ApiError('Plan 批准结果格式无效', 502, 'INVALID_RESPONSE')
	}
	return { plan: parsePlanView(value.plan), tasks: value.tasks.map(parseTask) }
}

export async function getMessages(
  subjectKind: DiscussionSubjectKind,
  subjectID: string,
  signal?: AbortSignal,
): Promise<DiscussionMessage[]> {
  const payload = await requestJSON(`/api/${subjectKind}/${encodeURIComponent(subjectID)}/messages`, { signal })
  if (!Array.isArray(payload)) {
    throw new ApiError('讨论消息列表格式无效', 502, 'INVALID_RESPONSE')
  }
  return payload.map(parseDiscussionMessage)
}

export async function createMessage(
  subjectKind: DiscussionSubjectKind,
  subjectID: string,
  input: CreateMessageInput,
): Promise<DiscussionMessage> {
  return parseDiscussionMessage(await requestJSON(`/api/${subjectKind}/${encodeURIComponent(subjectID)}/messages`, {
    method: 'POST',
    body: input,
  }))
}

export async function getTaskAssociations(
  subjectKind: DiscussionSubjectKind,
  subjectID: string,
  signal?: AbortSignal,
): Promise<TaskAssociation[]> {
  const payload = await requestJSON(`/api/${subjectKind}/${encodeURIComponent(subjectID)}/relations`, { signal })
  if (!Array.isArray(payload)) {
    throw new ApiError('Task 关联列表格式无效', 502, 'INVALID_RESPONSE')
  }
  return payload.map(parseTaskAssociation)
}

export async function linkTask(subjectKind: DiscussionSubjectKind, subjectID: string, taskID: string): Promise<void> {
  await requestJSON(`/api/${subjectKind}/${encodeURIComponent(subjectID)}/relations`, {
    method: 'POST',
    body: { task_id: taskID },
  })
}

export async function unlinkTask(subjectKind: DiscussionSubjectKind, subjectID: string, taskID: string): Promise<void> {
  await requestJSON(
    `/api/${subjectKind}/${encodeURIComponent(subjectID)}/relations/${encodeURIComponent(taskID)}`,
    { method: 'DELETE' },
  )
}

export async function getTaskTopics(taskID: string, signal?: AbortSignal): Promise<TopicAssociation[]> {
  const payload = await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/topics`, { signal })
  if (!Array.isArray(payload)) {
    throw new ApiError('Topic 关联列表格式无效', 502, 'INVALID_RESPONSE')
  }
  return payload.map(parseTopicAssociation)
}

export async function linkTopic(taskID: string, topicID: string): Promise<void> {
  await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/topics`, {
    method: 'POST',
    body: { topic_id: topicID },
  })
}

export async function unlinkTopic(taskID: string, topicID: string): Promise<void> {
  await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/topics/${encodeURIComponent(topicID)}`, {
    method: 'DELETE',
  })
}

export async function uploadImage(file: File): Promise<UploadedImage> {
  const optimized = await optimizeImage(file)
  const body = new FormData()
  body.append('original', file, file.name || 'pasted-image')
  body.append('optimized', optimized, optimized.name)
  const response = await fetch('/api/artifacts/images', { method: 'POST', body })
  const payload: unknown = await response.json().catch(() => undefined)
  if (!response.ok) {
    throw parseAPIError(payload, response.status)
  }
  return parseUploadedImage(payload)
}

export async function getRepositoryInfo(signal?: AbortSignal): Promise<RepositoryInfo> {
  return parseRepositoryInfo(await requestJSON('/api/git', { signal }))
}

export async function getReleases(signal?: AbortSignal): Promise<Release[]> {
  const payload = await requestJSON('/api/releases', { signal })
  if (!Array.isArray(payload)) {
    throw new ApiError('Release 历史格式无效', 502, 'INVALID_RESPONSE')
  }
  return payload.map(parseRelease)
}

export async function createRelease(input: CreateReleaseInput): Promise<Release> {
  return parseRelease(await requestJSON('/api/releases', { method: 'POST', body: input }))
}

export async function getTaskWorkspace(taskID: string, signal?: AbortSignal): Promise<Workspace | null> {
  const payload = await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/workspace`, { signal })
  return payload === null ? null : parseWorkspace(payload)
}

export async function createTaskWorkspace(taskID: string): Promise<Workspace> {
  return parseWorkspace(await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/workspace`, { method: 'POST' }))
}

export async function getTaskChanges(taskID: string, signal?: AbortSignal): Promise<TaskChanges> {
	return parseTaskChanges(await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/changes`, { signal }))
}

export async function getTaskFileDiff(taskID: string, path: string): Promise<FileDiff> {
	return parseFileDiff(await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/changes/file?path=${encodeURIComponent(path)}`))
}

export async function submitTaskReview(task: Task): Promise<Task> {
	return parseTask(await requestJSON(`/api/tasks/${encodeURIComponent(task.id)}/submit-review`, {
		method: 'POST', body: { version: task.version },
	}))
}

export async function commitTaskWorkspace(taskID: string, message: string): Promise<Workspace> {
	return parseWorkspace(await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/workspace/commit`, {
		method: 'POST', body: { message },
	}))
}

export async function reviewTask(task: Task, decision: 'ACCEPTED' | 'REJECTED', comment: string): Promise<{ task: Task; review: TaskReview }> {
	const value = await requestJSON(`/api/tasks/${encodeURIComponent(task.id)}/reviews`, {
		method: 'POST', body: { version: task.version, decision, comment },
	})
	if (!isRecord(value)) throw new ApiError('Review 结果格式无效', 502, 'INVALID_RESPONSE')
	return { task: parseTask(value.task), review: parseTaskReview(value.review) }
}

export async function getTaskReviews(taskID: string, signal?: AbortSignal): Promise<TaskReview[]> {
	const value = await requestJSON(`/api/tasks/${encodeURIComponent(taskID)}/reviews`, { signal })
	if (!Array.isArray(value)) throw new ApiError('Review 历史格式无效', 502, 'INVALID_RESPONSE')
	return value.map(parseTaskReview)
}

export function errorMessage(error: unknown): string {
  if (error instanceof ApiError || error instanceof Error) {
    return error.message
  }
  return '发生未知错误，请重试'
}

async function requestJSON(path: string, options: RequestOptions = {}): Promise<unknown> {
  const headers = options.body === undefined ? undefined : { 'Content-Type': 'application/json' }
  const response = await fetch(path, {
    method: options.method ?? 'GET',
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    signal: options.signal,
  })
  const payload: unknown = await response.json().catch(() => undefined)
  if (!response.ok) {
    throw parseAPIError(payload, response.status)
  }
  return payload
}

function parseAPIError(payload: unknown, status: number): ApiError {
  if (isRecord(payload) && isRecord(payload.error)) {
    const code = typeof payload.error.code === 'string' ? payload.error.code : 'REQUEST_FAILED'
    const message = typeof payload.error.message === 'string' ? payload.error.message : '请求失败'
    return new ApiError(message, status, code)
  }
  return new ApiError(`请求失败（HTTP ${status}）`, status, 'REQUEST_FAILED')
}

function parseProject(value: unknown): ProjectInfo {
  if (
    !isRecord(value) ||
    typeof value.name !== 'string' ||
    typeof value.root !== 'string' ||
    typeof value.agent !== 'string' ||
		typeof value.workers_enabled !== 'boolean' ||
    typeof value.max_workers !== 'number'
  ) {
    throw new ApiError('Project 信息格式无效', 502, 'INVALID_RESPONSE')
  }
  return {
		name: value.name, root: value.root, agent: value.agent,
		workers_enabled: value.workers_enabled, max_workers: value.max_workers,
	}
}

function parseProjectProgress(value: unknown): ProjectProgress {
	if (!isRecord(value)) throw new ApiError('项目进度格式无效', 502, 'INVALID_RESPONSE')
	const fields = [
		'total_tasks', 'accepted_tasks', 'strict_percent', 'estimated_tasks', 'estimate_coverage',
		'total_points', 'remaining_points', 'required_tests', 'verified_passed_tests', 'agent_reported_passed_tests',
	] as const
	if (fields.some((field) => typeof value[field] !== 'number') ||
		(value.forecast_percent !== undefined && value.forecast_percent !== null && typeof value.forecast_percent !== 'number')) {
		throw new ApiError('项目进度格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		total_tasks: value.total_tasks as number, accepted_tasks: value.accepted_tasks as number,
		strict_percent: value.strict_percent as number, estimated_tasks: value.estimated_tasks as number,
		estimate_coverage: value.estimate_coverage as number, total_points: value.total_points as number,
		remaining_points: value.remaining_points as number, required_tests: value.required_tests as number,
		verified_passed_tests: value.verified_passed_tests as number,
		agent_reported_passed_tests: value.agent_reported_passed_tests as number,
		...(typeof value.forecast_percent === 'number' ? { forecast_percent: value.forecast_percent } : {}),
	}
}

function parseRunUsageSummary(value: unknown): RunUsageSummary {
	if (!isRecord(value) || typeof value.total_runs !== 'number' || typeof value.runs_with_usage !== 'number' || !Array.isArray(value.by_purpose)) {
		throw new ApiError('Run 用量格式无效', 502, 'INVALID_RESPONSE')
	}
	return { ...parseUsageMetrics(value), by_purpose: value.by_purpose.map(parsePurposeUsage) }
}

function parseAgentRun(value: unknown): AgentRun {
	if (!isRecord(value) || !hasStringFields(value, [
		'id', 'profile_revision_id', 'lease_expires_at', 'queued_at', 'claimed_at', 'created_at', 'updated_at',
	]) || !runPurposes.includes(value.purpose as (typeof runPurposes)[number]) ||
		!runStatuses.includes(value.status as (typeof runStatuses)[number]) ||
		typeof value.subject_version !== 'number' || typeof value.lease_generation !== 'number') {
		throw new ApiError('Run 信息格式无效', 502, 'INVALID_RESPONSE')
	}
	const result: AgentRun = {
		id: value.id as string, purpose: value.purpose as AgentRun['purpose'], status: value.status as AgentRun['status'],
		profile_revision_id: value.profile_revision_id as string, subject_version: value.subject_version,
		session_resumed: value.session_resumed === true,
		lease_generation: value.lease_generation, lease_expires_at: value.lease_expires_at as string,
		queued_at: value.queued_at as string, claimed_at: value.claimed_at as string,
		created_at: value.created_at as string, updated_at: value.updated_at as string,
	}
	for (const field of ['topic_id', 'task_id', 'retry_of_run_id', 'continuation_of_run_id', 'agent_session_id', 'started_at', 'finished_at', 'failure_kind', 'failure_code', 'failure_message', 'cancel_requested_at', 'cancel_reason'] as const) {
		if (typeof value[field] === 'string') result[field] = value[field]
	}
	if (typeof value.exit_code === 'number') result.exit_code = value.exit_code
	if (typeof value.failure_retryable === 'boolean') result.failure_retryable = value.failure_retryable
	return result
}

function parseRunPage(value: unknown): RunPage {
	if (!isRecord(value) || !Array.isArray(value.items) || !isOptionalString(value.next_cursor)) {
		throw new ApiError('Run 查询结果格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		items: value.items.map(parseAgentRun),
		...(typeof value.next_cursor === 'string' ? { next_cursor: value.next_cursor } : {}),
	}
}

function parseApprovalRequest(value: unknown): ApprovalRequest {
	if (!isRecord(value) || !hasStringFields(value, ['id', 'run_id', 'kind', 'status', 'created_at', 'updated_at']) ||
		typeof value.version !== 'number' || !Array.isArray(value.available_decisions) ||
		!value.available_decisions.every((item) => typeof item === 'string')) {
		throw new ApiError('权限请求格式无效', 502, 'INVALID_RESPONSE')
	}
	const result: ApprovalRequest = {
		id: value.id as string, run_id: value.run_id as string,
		kind: value.kind as ApprovalRequest['kind'], status: value.status as ApprovalRequest['status'],
		available_decisions: value.available_decisions as ApprovalDecision[], version: value.version,
		created_at: value.created_at as string, updated_at: value.updated_at as string,
	}
	for (const field of ['task_id', 'item_id', 'reason', 'command', 'cwd', 'host', 'protocol', 'grant_root', 'decision', 'resolved_at'] as const) {
		if (typeof value[field] === 'string') result[field] = value[field] as never
	}
	return result
}

function parseRunDetail(value: unknown): RunDetail {
	if (!isRecord(value) || !Array.isArray(value.artifacts)) {
		throw new ApiError('Run 详情格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		run: parseAgentRun(value.run),
		artifacts: value.artifacts.map(parseRunArtifact),
		...(isRecord(value.usage) ? { usage: parseRunUsage(value.usage) } : {}),
		...(isRecord(value.workspace_snapshot) ? { workspace_snapshot: parseRunWorkspaceSnapshot(value.workspace_snapshot) } : {}),
	}
}

function parseRunArtifact(value: unknown): RunArtifact {
	if (!isRecord(value) || !hasStringFields(value, ['id', 'run_id', 'kind', 'relative_path', 'sha256', 'created_at']) ||
		typeof value.size !== 'number' || typeof value.truncated !== 'boolean') {
		throw new ApiError('Run Artifact 格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		id: value.id as string, run_id: value.run_id as string, kind: value.kind as string,
		relative_path: value.relative_path as string, sha256: value.sha256 as string,
		size: value.size, truncated: value.truncated, created_at: value.created_at as string,
	}
}

function parseRunUsage(value: unknown): RunUsage {
	if (!isRecord(value) || !hasStringFields(value, ['run_id', 'source', 'captured_at'])) {
		throw new ApiError('Run 用量格式无效', 502, 'INVALID_RESPONSE')
	}
	const result: RunUsage = { run_id: value.run_id as string, source: value.source as string, captured_at: value.captured_at as string }
	for (const field of ['input_tokens', 'cached_input_tokens', 'cache_write_input_tokens', 'output_tokens', 'reasoning_output_tokens', 'model_requests', 'peak_input_tokens'] as const) {
		if (value[field] !== null && value[field] !== undefined && typeof value[field] !== 'number') {
			throw new ApiError('Run 用量格式无效', 502, 'INVALID_RESPONSE')
		}
		if (typeof value[field] === 'number') result[field] = value[field]
	}
	return result
}

function parseRunWorkspaceSnapshot(value: unknown): RunWorkspaceSnapshot {
	if (!isRecord(value) || !hasStringFields(value, [
		'run_id', 'workspace_id', 'branch_name', 'target_branch', 'base_commit_sha', 'head_before', 'head_after', 'captured_at',
	]) || typeof value.dirty_before !== 'boolean' || typeof value.dirty_after !== 'boolean' || !isWorkspaceState(value.state_after)) {
		throw new ApiError('Run Workspace 快照格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		run_id: value.run_id as string, workspace_id: value.workspace_id as string,
		branch_name: value.branch_name as string, target_branch: value.target_branch as string,
		base_commit_sha: value.base_commit_sha as string, head_before: value.head_before as string,
		head_after: value.head_after as string, dirty_before: value.dirty_before,
		dirty_after: value.dirty_after, state_after: value.state_after, captured_at: value.captured_at as string,
	}
}

function parseRunLog(value: unknown): RunLog {
	if (!isRecord(value) || (value.stream !== 'stdout' && value.stream !== 'stderr') ||
		typeof value.content !== 'string' || typeof value.size !== 'number' || typeof value.truncated !== 'boolean') {
		throw new ApiError('Run 日志格式无效', 502, 'INVALID_RESPONSE')
	}
	return { stream: value.stream, content: value.content, size: value.size, truncated: value.truncated }
}

function parsePurposeUsage(value: unknown) {
	if (!isRecord(value) || !runPurposes.includes(value.purpose as (typeof runPurposes)[number])) {
		throw new ApiError('Run 用量格式无效', 502, 'INVALID_RESPONSE')
	}
	return { purpose: value.purpose as (typeof runPurposes)[number], ...parseUsageMetrics(value) }
}

function parseUsageMetrics(value: Record<string, unknown>) {
	if (typeof value.total_runs !== 'number' || typeof value.runs_with_usage !== 'number') {
		throw new ApiError('Run 用量格式无效', 502, 'INVALID_RESPONSE')
	}
	const result: Omit<RunUsageSummary, 'by_purpose'> = {
		total_runs: value.total_runs, runs_with_usage: value.runs_with_usage,
	}
	for (const field of ['input_tokens', 'cached_input_tokens', 'uncached_input_tokens', 'cache_write_input_tokens', 'output_tokens', 'reasoning_output_tokens', 'model_requests', 'peak_input_tokens'] as const) {
		const metric = value[field]
		if (metric !== null && metric !== undefined && typeof metric !== 'number') {
			throw new ApiError('Run 用量格式无效', 502, 'INVALID_RESPONSE')
		}
		if (typeof metric === 'number') result[field] = metric
	}
	return result
}

function parseAgentProfile(value: unknown): AgentProfile {
	if (!isRecord(value) || typeof value.id !== 'string' || typeof value.name !== 'string' ||
		!isAgentRole(value.role) || typeof value.created_at !== 'string' || typeof value.updated_at !== 'string') {
		throw new ApiError('Agent Profile 格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		id: value.id, name: value.name, role: value.role,
		current_revision: parseAgentProfileRevision(value.current_revision),
		created_at: value.created_at, updated_at: value.updated_at,
	}
}

function parseAgentProfileRevision(value: unknown): AgentProfile['current_revision'] {
	if (!isRecord(value) || typeof value.id !== 'string' || typeof value.profile_id !== 'string' ||
		typeof value.revision !== 'number' || typeof value.instructions !== 'string' ||
		typeof value.adapter !== 'string' || typeof value.command !== 'string' || !isStringArray(value.args) ||
		typeof value.model !== 'string' || !hasNumberFields(value, ['max_input_tokens', 'reserved_output_tokens', 'recent_message_limit', 'retrieval_limit', 'timeout_seconds']) ||
		!isWorkspacePolicy(value.workspace_policy) || !isApprovalPolicy(value.approval_policy) || typeof value.created_at !== 'string') {
		throw new ApiError('Agent Profile Revision 格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		id: value.id, profile_id: value.profile_id, revision: value.revision,
		instructions: value.instructions, adapter: value.adapter, command: value.command,
		args: value.args, model: value.model, max_input_tokens: value.max_input_tokens as number,
		reserved_output_tokens: value.reserved_output_tokens as number,
		recent_message_limit: value.recent_message_limit as number, retrieval_limit: value.retrieval_limit as number,
		workspace_policy: value.workspace_policy, approval_policy: value.approval_policy,
		timeout_seconds: value.timeout_seconds as number, tool_policy: parseAgentToolPolicy(value.tool_policy),
		created_at: value.created_at,
	}
}

function parseProjectCapabilities(value: unknown): ProjectCapabilityCatalog {
	if (!isRecord(value) || !Array.isArray(value.skills) || !Array.isArray(value.mcp_servers)) {
		throw new ApiError('项目能力目录格式无效', 502, 'INVALID_RESPONSE')
	}
	return { skills: value.skills.map(parseProjectSkill), mcp_servers: value.mcp_servers.map(parseProjectMCPServer) }
}

function parseProjectSkill(value: unknown): ProjectSkill {
	if (!isRecord(value) || !hasStringFields(value, ['id', 'name', 'source_path', 'content_sha256', 'created_at', 'updated_at']) ||
		typeof value.enabled !== 'boolean' || typeof value.version !== 'number') {
		throw new ApiError('项目 Skill 格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		id: value.id as string, name: value.name as string, source_path: value.source_path as string,
		content_sha256: value.content_sha256 as string, enabled: value.enabled, version: value.version,
		created_at: value.created_at as string, updated_at: value.updated_at as string,
	}
}

function parseProjectMCPServer(value: unknown): ProjectMCPServer {
	if (!isRecord(value) || !hasStringFields(value, ['id', 'name', 'config_name', 'created_at', 'updated_at']) ||
		typeof value.enabled !== 'boolean' || typeof value.version !== 'number') {
		throw new ApiError('项目 MCP 格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		id: value.id as string, name: value.name as string, config_name: value.config_name as string,
		enabled: value.enabled, version: value.version,
		created_at: value.created_at as string, updated_at: value.updated_at as string,
	}
}

function parseAgentToolPolicy(value: unknown): AgentProfile['current_revision']['tool_policy'] {
	if (!isRecord(value) || !Array.isArray(value.skills) || !Array.isArray(value.mcp_servers)) {
		throw new ApiError('Agent Tool Policy 格式无效', 502, 'INVALID_RESPONSE')
	}
	const skills = value.skills.map((binding) => {
		if (!isRecord(binding) || typeof binding.skill_id !== 'string' || typeof binding.required !== 'boolean') {
			throw new ApiError('Agent Skill Policy 格式无效', 502, 'INVALID_RESPONSE')
		}
		return { skill_id: binding.skill_id, required: binding.required }
	})
	const mcpServers = value.mcp_servers.map((binding) => {
		if (!isRecord(binding) || typeof binding.server_id !== 'string' || typeof binding.required !== 'boolean' ||
			!isStringArray(binding.enabled_tools)) {
			throw new ApiError('Agent MCP Policy 格式无效', 502, 'INVALID_RESPONSE')
		}
		return { server_id: binding.server_id, required: binding.required, enabled_tools: binding.enabled_tools }
	})
	return { skills, mcp_servers: mcpServers }
}

function parseClarifications(value: unknown): Clarification[] {
	if (!Array.isArray(value)) throw new ApiError('待回答问题列表格式无效', 502, 'INVALID_RESPONSE')
	return value.map(parseClarification)
}

function parseClarification(value: unknown): Clarification {
	if (!isRecord(value) || !hasStringFields(value, ['id', 'task_id', 'source_run_id', 'question', 'created_at', 'updated_at']) ||
		(value.continuation_purpose !== 'IMPLEMENTATION' && value.continuation_purpose !== 'REVISION') ||
		!isClarificationCategory(value.category) || (value.status !== 'OPEN' && value.status !== 'ANSWERED') ||
		!Array.isArray(value.options) || typeof value.allow_custom_answer !== 'boolean' || typeof value.version !== 'number') {
		throw new ApiError('待回答问题格式无效', 502, 'INVALID_RESPONSE')
	}
	const options = value.options.map((option) => {
		if (!isRecord(option) || !hasStringFields(option, ['id', 'label', 'description'])) {
			throw new ApiError('待回答问题选项格式无效', 502, 'INVALID_RESPONSE')
		}
		return { id: option.id as string, label: option.label as string, description: option.description as string }
	})
	return {
		id: value.id as string, task_id: value.task_id as string, source_run_id: value.source_run_id as string,
		continuation_purpose: value.continuation_purpose, category: value.category, question: value.question as string,
		options, allow_custom_answer: value.allow_custom_answer, status: value.status, version: value.version,
		created_at: value.created_at as string, updated_at: value.updated_at as string,
		...(typeof value.continuation_run_id === 'string' ? { continuation_run_id: value.continuation_run_id } : {}),
		...(typeof value.recommended_option_id === 'string' ? { recommended_option_id: value.recommended_option_id } : {}),
		...(typeof value.selected_option_id === 'string' ? { selected_option_id: value.selected_option_id } : {}),
		...(typeof value.custom_answer === 'string' ? { custom_answer: value.custom_answer } : {}),
		...(typeof value.answered_at === 'string' ? { answered_at: value.answered_at } : {}),
	}
}

function isClarificationCategory(value: unknown): value is Clarification['category'] {
	return value === 'REQUIREMENT' || value === 'DECISION' || value === 'ENVIRONMENT' || value === 'VALIDATION'
}

function hasNumberFields(value: Record<string, unknown>, fields: readonly string[]): boolean {
	return fields.every((field) => typeof value[field] === 'number')
}

function hasStringFields(value: Record<string, unknown>, fields: readonly string[]): boolean {
	return fields.every((field) => typeof value[field] === 'string')
}

function isAgentRole(value: unknown): value is AgentProfile['role'] {
	return value === 'PLANNER' || value === 'TRIAGER' || value === 'IMPLEMENTER' || value === 'REVISION' || value === 'REVIEWER'
}

function isWorkspacePolicy(value: unknown): value is AgentProfile['current_revision']['workspace_policy'] {
	return value === 'NONE' || value === 'READ_ONLY' || value === 'WRITE_TASK'
}

function isApprovalPolicy(value: unknown): value is AgentProfile['current_revision']['approval_policy'] {
	return value === 'READ_ONLY' || value === 'WORKSPACE_WRITE'
}

function parseTaskQuality(value: unknown): TaskQuality {
	if (!isRecord(value) || !Array.isArray(value.estimate_history) || !Array.isArray(value.test_cases)) {
		throw new ApiError('Task 质量信息格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		...(value.estimate === undefined ? {} : { estimate: parseTaskEstimate(value.estimate) }),
		estimate_history: value.estimate_history.map(parseTaskEstimate),
		test_cases: value.test_cases.map(parseTaskTestCase),
	}
}

function parseTaskEstimate(value: unknown): TaskEstimate {
	if (!isRecord(value) || typeof value.id !== 'string' || typeof value.task_id !== 'string' ||
		!hasNumberFields(value, ['revision', 'points', 'remaining_points', 'confidence']) ||
		typeof value.rationale !== 'string' || (value.source !== 'AI' && value.source !== 'HUMAN') ||
		typeof value.created_at !== 'string') {
		throw new ApiError('Task 估算格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		id: value.id, task_id: value.task_id, revision: value.revision as number,
		points: value.points as number, remaining_points: value.remaining_points as number,
		confidence: value.confidence as number, rationale: value.rationale, source: value.source,
		created_at: value.created_at,
		...(typeof value.source_run_id === 'string' ? { source_run_id: value.source_run_id } : {}),
	}
}

function parseTaskTestCase(value: unknown): TaskTestCase {
	if (!isRecord(value) || typeof value.id !== 'string' || typeof value.task_id !== 'string' ||
		typeof value.title !== 'string' || typeof value.description !== 'string' || typeof value.required !== 'boolean' ||
		typeof value.sort_order !== 'number' || (value.created_by !== 'HUMAN' && value.created_by !== 'AGENT') ||
		typeof value.created_at !== 'string' || typeof value.updated_at !== 'string') {
		throw new ApiError('Task 测试项格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		id: value.id, task_id: value.task_id, title: value.title, description: value.description,
		required: value.required, sort_order: value.sort_order, created_by: value.created_by,
		created_at: value.created_at, updated_at: value.updated_at,
		...(typeof value.source_run_id === 'string' ? { source_run_id: value.source_run_id } : {}),
		...(value.latest_result === undefined ? {} : { latest_result: parseTaskTestResult(value.latest_result) }),
	}
}

function parseTaskTestResult(value: unknown): TaskTestResult {
	if (!isRecord(value) || typeof value.id !== 'string' || typeof value.test_case_id !== 'string' ||
		typeof value.task_id !== 'string' || !isTestOutcome(value.outcome) || !isTestEvidence(value.evidence_kind) ||
		typeof value.summary !== 'string' || typeof value.created_at !== 'string') {
		throw new ApiError('Task 测试结果格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		id: value.id, test_case_id: value.test_case_id, task_id: value.task_id,
		outcome: value.outcome, evidence_kind: value.evidence_kind, summary: value.summary,
		created_at: value.created_at,
		...(typeof value.command === 'string' ? { command: value.command } : {}),
		...(typeof value.artifact_ref === 'string' ? { artifact_ref: value.artifact_ref } : {}),
		...(typeof value.source_run_id === 'string' ? { source_run_id: value.source_run_id } : {}),
	}
}

function isTestOutcome(value: unknown): value is TaskTestResult['outcome'] {
	return value === 'PASSED' || value === 'FAILED' || value === 'BLOCKED'
}

function isTestEvidence(value: unknown): value is TaskTestResult['evidence_kind'] {
	return value === 'COMMAND' || value === 'HUMAN' || value === 'AGENT_REPORT'
}

function parseTaskAssessmentState(value: unknown): TaskAssessmentState {
	if (!isRecord(value) || !Array.isArray(value.history) || typeof value.stale !== 'boolean' ||
		(value.current !== null && value.current !== undefined && !isRecord(value.current))) {
		throw new ApiError('Task 评估格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		...(isRecord(value.current) ? { current: parseTaskAssessment(value.current) } : {}),
		history: value.history.map(parseTaskAssessment),
		stale: value.stale,
	}
}

function parseTaskAssessment(value: unknown): TaskAssessment {
	if (!isRecord(value) || !isRecord(value.scores) ||
		!hasStringFields(value, ['id', 'task_id', 'suggested_title', 'applied_title', 'rationale', 'split_rationale', 'source_run_id', 'created_at']) ||
		!hasNumberFields(value, ['task_assessment_version', 'revision', 'weighted_score', 'confidence']) ||
		!isComplexity(value.complexity) || !isAutonomy(value.autonomy) ||
		typeof value.split_recommended !== 'boolean' || !isStringArray(value.assumptions) ||
		!hasNumberFields(value.scores, ['technical_complexity', 'requirement_uncertainty', 'change_scope', 'validation_burden', 'human_dependency', 'risk_and_reversibility'])) {
		throw new ApiError('Task 评估格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		id: value.id as string, task_id: value.task_id as string,
		task_assessment_version: value.task_assessment_version as number,
		revision: value.revision as number, suggested_title: value.suggested_title as string,
		applied_title: value.applied_title as string,
		scores: {
			technical_complexity: value.scores.technical_complexity as number,
			requirement_uncertainty: value.scores.requirement_uncertainty as number,
			change_scope: value.scores.change_scope as number,
			validation_burden: value.scores.validation_burden as number,
			human_dependency: value.scores.human_dependency as number,
			risk_and_reversibility: value.scores.risk_and_reversibility as number,
		},
		weighted_score: value.weighted_score as number, complexity: value.complexity,
		autonomy: value.autonomy, confidence: value.confidence as number,
		rationale: value.rationale as string, assumptions: value.assumptions,
		split_recommended: value.split_recommended, split_rationale: value.split_rationale as string,
		source_run_id: value.source_run_id as string, created_at: value.created_at as string,
	}
}

function isComplexity(value: unknown): value is TaskAssessment['complexity'] {
	return value === 'C1' || value === 'C2' || value === 'C3' || value === 'C4' || value === 'C5'
}

function isAutonomy(value: unknown): value is TaskAssessment['autonomy'] {
	return value === 'A0' || value === 'A1' || value === 'A2' || value === 'A3'
}

function parseTask(value: unknown): Task {
  if (!isTaskRecord(value)) {
    throw new ApiError('Task 信息格式无效', 502, 'INVALID_RESPONSE')
  }
  return {
    id: value.id,
    key: value.key,
    title: value.title,
		title_source: value.title_source,
		title_locked: value.title_locked,
    description: value.description,
    acceptance_criteria: value.acceptance_criteria,
    status: value.status,
    priority: value.priority,
		assessment_input_version: value.assessment_input_version,
    version: value.version,
    created_at: value.created_at,
    updated_at: value.updated_at,
    ...optionalTaskFields(value),
		...(isRecord(value.assessment) ? { assessment: parseTaskAssessment(value.assessment) } : {}),
		...(typeof value.assessment_stale === 'boolean' ? { assessment_stale: value.assessment_stale } : {}),
  }
}

function isTaskRecord(value: unknown): value is Record<string, unknown> & {
  id: string
  key: string
  title: string
	title_source: Task['title_source']
	title_locked: boolean
  description: string
  acceptance_criteria: string
  status: TaskStatus
  priority: number
	assessment_input_version: number
  version: number
  created_at: string
  updated_at: string
} {
  return (
    isRecord(value) &&
    typeof value.id === 'string' &&
    typeof value.key === 'string' &&
    typeof value.title === 'string' &&
		(value.title_source === 'PROVISIONAL' || value.title_source === 'AI' || value.title_source === 'HUMAN') &&
		typeof value.title_locked === 'boolean' &&
    typeof value.description === 'string' &&
    typeof value.acceptance_criteria === 'string' &&
    isTaskStatus(value.status) &&
    typeof value.priority === 'number' &&
		typeof value.assessment_input_version === 'number' &&
    typeof value.version === 'number' &&
    typeof value.created_at === 'string' &&
    typeof value.updated_at === 'string'
  )
}

function optionalTaskFields(value: Record<string, unknown>): Partial<Task> {
  const result: Partial<Task> = {}
  for (const field of ['target_branch', 'base_commit_sha', 'current_workspace_id', 'latest_run_id', 'source_plan_revision_id', 'source_plan_task_draft_id'] as const) {
    if (typeof value[field] === 'string') {
      result[field] = value[field]
    }
  }
  return result
}

function isTaskStatus(value: unknown): value is TaskStatus {
  return typeof value === 'string' && taskStatuses.some((status) => status === value)
}

function parseTopic(value: unknown): Topic {
  if (!isTopicRecord(value)) {
    throw new ApiError('Topic 信息格式无效', 502, 'INVALID_RESPONSE')
  }
  return {
    id: value.id,
    key: value.key,
    title: value.title,
    description: value.description,
    status: value.status,
    version: value.version,
    created_at: value.created_at,
    updated_at: value.updated_at,
    ...optionalTopicFields(value),
  }
}

function isTopicRecord(value: unknown): value is Record<string, unknown> & {
  id: string
  key: string
  title: string
  description: string
  status: TopicStatus
  version: number
  created_at: string
  updated_at: string
} {
  return (
    isRecord(value) &&
    typeof value.id === 'string' &&
    typeof value.key === 'string' &&
    typeof value.title === 'string' &&
    typeof value.description === 'string' &&
    isTopicStatus(value.status) &&
    typeof value.version === 'number' &&
    typeof value.created_at === 'string' &&
    typeof value.updated_at === 'string'
  )
}

function optionalTopicFields(value: Record<string, unknown>): Partial<Topic> {
  const result: Partial<Topic> = {}
  for (const field of ['current_plan_id', 'current_summary_id'] as const) {
    if (typeof value[field] === 'string') {
      result[field] = value[field]
    }
  }
  return result
}

function isTopicStatus(value: unknown): value is TopicStatus {
  return typeof value === 'string' && topicStatuses.some((status) => status === value)
}

function parsePlanView(value: unknown): PlanView {
	if (!isRecord(value) || !isRecord(value.plan) || !isRecord(value.revision) || !Array.isArray(value.reviews)) {
		throw new ApiError('Plan 信息格式无效', 502, 'INVALID_RESPONSE')
	}
	const current = value.plan
	const revision = value.revision
	if (!hasStringFields(current, ['id', 'key', 'topic_id', 'current_revision_id', 'created_at', 'updated_at']) ||
		(current.status !== 'IN_REVIEW' && current.status !== 'CHANGES_REQUESTED' && current.status !== 'APPROVED') ||
		typeof current.version !== 'number' ||
		!hasStringFields(revision, ['id', 'plan_id', 'summary', 'rationale', 'risks', 'created_at']) ||
		typeof revision.revision !== 'number' || !Array.isArray(revision.drafts)) {
		throw new ApiError('Plan 信息格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		plan: {
			id: current.id as string, key: current.key as string, topic_id: current.topic_id as string,
			status: current.status, current_revision_id: current.current_revision_id as string,
			version: current.version, created_at: current.created_at as string, updated_at: current.updated_at as string,
			...(typeof current.approved_revision_id === 'string' ? { approved_revision_id: current.approved_revision_id } : {}),
		},
		revision: {
			id: revision.id as string, plan_id: revision.plan_id as string, revision: revision.revision,
			summary: revision.summary as string, rationale: revision.rationale as string, risks: revision.risks as string,
			drafts: revision.drafts.map(parsePlanTaskDraft), created_at: revision.created_at as string,
			...(typeof revision.source_run_id === 'string' ? { source_run_id: revision.source_run_id } : {}),
			...(typeof revision.previous_revision_id === 'string' ? { previous_revision_id: revision.previous_revision_id } : {}),
		},
		reviews: value.reviews.map((review) => {
			if (!isRecord(review) || !hasStringFields(review, ['id', 'plan_id', 'plan_revision_id', 'comment', 'created_at']) ||
				(review.decision !== 'APPROVED' && review.decision !== 'CHANGES_REQUESTED')) {
				throw new ApiError('Plan Review 格式无效', 502, 'INVALID_RESPONSE')
			}
			return {
				id: review.id as string, plan_id: review.plan_id as string,
				plan_revision_id: review.plan_revision_id as string, decision: review.decision,
				comment: review.comment as string, created_at: review.created_at as string,
			}
		}),
	}
}

function parsePlanTaskDraft(value: unknown): PlanView['revision']['drafts'][number] {
	if (!isRecord(value) || !hasStringFields(value, ['id', 'plan_revision_id', 'key', 'title', 'description', 'acceptance_criteria']) ||
		typeof value.priority !== 'number' || typeof value.proposed_order !== 'number' || !Array.isArray(value.test_cases)) {
		throw new ApiError('Plan Task 草案格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		id: value.id as string, plan_revision_id: value.plan_revision_id as string, key: value.key as string,
		title: value.title as string, description: value.description as string,
		acceptance_criteria: value.acceptance_criteria as string, priority: value.priority,
		proposed_order: value.proposed_order,
		test_cases: value.test_cases.map((testCase) => {
			if (!isRecord(testCase) || !hasStringFields(testCase, ['id', 'task_draft_id', 'title', 'description']) ||
				typeof testCase.required !== 'boolean' || typeof testCase.sort_order !== 'number') {
				throw new ApiError('Plan 测试项格式无效', 502, 'INVALID_RESPONSE')
			}
			return {
				id: testCase.id as string, task_draft_id: testCase.task_draft_id as string,
				title: testCase.title as string, description: testCase.description as string,
				required: testCase.required, sort_order: testCase.sort_order,
			}
		}),
	}
}

function parseDiscussionMessage(value: unknown): DiscussionMessage {
  if (
    !isRecord(value) ||
    typeof value.id !== 'string' ||
    typeof value.thread_id !== 'string' ||
    typeof value.sequence !== 'number' ||
    !isMessageAuthorKind(value.author_kind) ||
    typeof value.content !== 'string' ||
    !isStringArray(value.linked_task_ids) ||
    typeof value.created_at !== 'string'
  ) {
    throw new ApiError('讨论消息格式无效', 502, 'INVALID_RESPONSE')
  }
  return {
    id: value.id,
    thread_id: value.thread_id,
    sequence: value.sequence,
    author_kind: value.author_kind,
    content: value.content,
    linked_task_ids: value.linked_task_ids,
    created_at: value.created_at,
  }
}

function parseTaskAssociation(value: unknown): TaskAssociation {
  if (
    !isRecord(value) ||
    typeof value.created_at !== 'string' ||
    (value.source_message_id !== undefined && typeof value.source_message_id !== 'string')
  ) {
    throw new ApiError('Task 关联格式无效', 502, 'INVALID_RESPONSE')
  }
  return {
    task: parseTask(value.task),
    created_at: value.created_at,
    ...(typeof value.source_message_id === 'string' ? { source_message_id: value.source_message_id } : {}),
  }
}

function parseTopicAssociation(value: unknown): TopicAssociation {
  if (
    !isRecord(value) ||
    typeof value.created_at !== 'string' ||
    (value.source_message_id !== undefined && typeof value.source_message_id !== 'string')
  ) {
    throw new ApiError('Topic 关联格式无效', 502, 'INVALID_RESPONSE')
  }
  return {
    topic: parseTopic(value.topic),
    created_at: value.created_at,
    ...(typeof value.source_message_id === 'string' ? { source_message_id: value.source_message_id } : {}),
  }
}

function parseUploadedImage(value: unknown): UploadedImage {
  if (
    !isRecord(value) ||
    typeof value.id !== 'string' ||
    typeof value.markdown !== 'string' ||
    typeof value.original_url !== 'string' ||
    typeof value.optimized_url !== 'string'
  ) {
    throw new ApiError('图片上传结果格式无效', 502, 'INVALID_RESPONSE')
  }
  return {
    id: value.id,
    markdown: value.markdown,
    original_url: value.original_url,
    optimized_url: value.optimized_url,
  }
}

function parseRepositoryInfo(value: unknown): RepositoryInfo {
  if (
    !isRecord(value) ||
		typeof value.root !== 'string' ||
		typeof value.git_common_dir !== 'string' ||
		typeof value.git_version !== 'string' ||
		typeof value.default_branch !== 'string' ||
    typeof value.current_branch !== 'string' ||
    typeof value.head_sha !== 'string' ||
		typeof value.has_head !== 'boolean' ||
    typeof value.dirty !== 'boolean' ||
		typeof value.identity_configured !== 'boolean' ||
		!isOptionalString(value.remote_default_branch) ||
		!isOptionalString(value.exact_tag) ||
		!isOptionalString(value.upstream) ||
		!isOptionalNumber(value.ahead) ||
		!isOptionalNumber(value.behind) ||
		!isOptionalString(value.user_name) ||
		!isOptionalString(value.user_email) ||
    !Array.isArray(value.branches) ||
		!Array.isArray(value.remotes)
  ) {
    throw new ApiError('Git 仓库信息格式无效', 502, 'INVALID_RESPONSE')
  }
  const branches = value.branches.map((branch) => {
    if (!isRecord(branch) || typeof branch.name !== 'string' || typeof branch.head_sha !== 'string') {
      throw new ApiError('Git 分支信息格式无效', 502, 'INVALID_RESPONSE')
    }
    return { name: branch.name, head_sha: branch.head_sha }
  })
	const remotes = value.remotes.map((remote) => {
		if (!isRecord(remote) || typeof remote.name !== 'string' || typeof remote.fetch_url !== 'string' || typeof remote.push_url !== 'string') {
			throw new ApiError('Git Remote 信息格式无效', 502, 'INVALID_RESPONSE')
		}
		return { name: remote.name, fetch_url: remote.fetch_url, push_url: remote.push_url }
	})
  return {
		root: value.root,
		git_common_dir: value.git_common_dir,
		git_version: value.git_version,
		default_branch: value.default_branch,
    current_branch: value.current_branch,
    head_sha: value.head_sha,
		has_head: value.has_head,
    dirty: value.dirty,
    branches,
		remotes,
		identity_configured: value.identity_configured,
		...(typeof value.remote_default_branch === 'string' ? { remote_default_branch: value.remote_default_branch } : {}),
    ...(typeof value.exact_tag === 'string' ? { exact_tag: value.exact_tag } : {}),
		...(typeof value.upstream === 'string' ? { upstream: value.upstream } : {}),
		...(typeof value.ahead === 'number' ? { ahead: value.ahead } : {}),
		...(typeof value.behind === 'number' ? { behind: value.behind } : {}),
		...(typeof value.user_name === 'string' ? { user_name: value.user_name } : {}),
		...(typeof value.user_email === 'string' ? { user_email: value.user_email } : {}),
  }
}

function isOptionalString(value: unknown): value is string | undefined {
	return value === undefined || typeof value === 'string'
}

function isOptionalNumber(value: unknown): value is number | undefined {
	return value === undefined || typeof value === 'number'
}

function parseTaskChanges(value: unknown): TaskChanges {
	if (!isRecord(value) || typeof value.base_commit_sha !== 'string' || typeof value.head_sha !== 'string' ||
		typeof value.dirty !== 'boolean' || typeof value.file_count !== 'number' || typeof value.additions !== 'number' ||
		typeof value.deletions !== 'number' || !Array.isArray(value.files)) {
		throw new ApiError('Task 变更格式无效', 502, 'INVALID_RESPONSE')
	}
	return {
		base_commit_sha: value.base_commit_sha, head_sha: value.head_sha, dirty: value.dirty,
		file_count: value.file_count, additions: value.additions, deletions: value.deletions,
		files: value.files.map((file) => {
			if (!isRecord(file) || typeof file.path !== 'string' || typeof file.status !== 'string' ||
				typeof file.additions !== 'number' || typeof file.deletions !== 'number' || typeof file.binary !== 'boolean') {
				throw new ApiError('变更文件格式无效', 502, 'INVALID_RESPONSE')
			}
			return { path: file.path, status: file.status, additions: file.additions, deletions: file.deletions, binary: file.binary }
		}),
	}
}

function parseFileDiff(value: unknown): FileDiff {
	if (!isRecord(value) || typeof value.path !== 'string' || typeof value.patch !== 'string' ||
		typeof value.truncated !== 'boolean' || typeof value.binary !== 'boolean') {
		throw new ApiError('文件 Diff 格式无效', 502, 'INVALID_RESPONSE')
	}
	return { path: value.path, patch: value.patch, truncated: value.truncated, binary: value.binary }
}

function parseTaskReview(value: unknown): TaskReview {
	if (!isRecord(value) || typeof value.id !== 'string' || typeof value.task_id !== 'string' ||
		(value.decision !== 'ACCEPTED' && value.decision !== 'REJECTED') || typeof value.comment !== 'string' ||
		typeof value.commit_sha !== 'string' || typeof value.created_at !== 'string') {
		throw new ApiError('Review 格式无效', 502, 'INVALID_RESPONSE')
	}
	return { id: value.id, task_id: value.task_id, decision: value.decision, comment: value.comment, commit_sha: value.commit_sha, created_at: value.created_at }
}

function parseRelease(value: unknown): Release {
  if (
    !isRecord(value) ||
    typeof value.id !== 'string' ||
    typeof value.version !== 'string' ||
    typeof value.tag_name !== 'string' ||
    typeof value.source_branch !== 'string' ||
    typeof value.commit_sha !== 'string' ||
    !isReleaseStatus(value.status) ||
    !isStringArray(value.task_ids) ||
    typeof value.created_at !== 'string' ||
    typeof value.updated_at !== 'string'
  ) {
    throw new ApiError('Release 信息格式无效', 502, 'INVALID_RESPONSE')
  }
  return {
    id: value.id,
    version: value.version,
    tag_name: value.tag_name,
    source_branch: value.source_branch,
    commit_sha: value.commit_sha,
    status: value.status,
    task_ids: value.task_ids,
    created_at: value.created_at,
    updated_at: value.updated_at,
    ...(typeof value.failure_message === 'string' ? { failure_message: value.failure_message } : {}),
    ...(typeof value.tagged_at === 'string' ? { tagged_at: value.tagged_at } : {}),
  }
}

function parseWorkspace(value: unknown): Workspace {
  if (
    !isRecord(value) ||
    typeof value.id !== 'string' ||
    typeof value.task_id !== 'string' ||
    typeof value.path !== 'string' ||
    typeof value.branch_name !== 'string' ||
    typeof value.target_branch !== 'string' ||
    typeof value.base_commit_sha !== 'string' ||
    typeof value.head_sha !== 'string' ||
    !isWorkspaceState(value.state) ||
    typeof value.dirty !== 'boolean' ||
    typeof value.created_at !== 'string' ||
    typeof value.updated_at !== 'string'
  ) {
    throw new ApiError('Workspace 信息格式无效', 502, 'INVALID_RESPONSE')
  }
  return {
    id: value.id,
    task_id: value.task_id,
    path: value.path,
    branch_name: value.branch_name,
    target_branch: value.target_branch,
    base_commit_sha: value.base_commit_sha,
    head_sha: value.head_sha,
    state: value.state,
    dirty: value.dirty,
    created_at: value.created_at,
    updated_at: value.updated_at,
    ...(typeof value.failure_message === 'string' ? { failure_message: value.failure_message } : {}),
    ...(typeof value.last_verified_at === 'string' ? { last_verified_at: value.last_verified_at } : {}),
  }
}

function isReleaseStatus(value: unknown): value is ReleaseStatus {
  return typeof value === 'string' && releaseStatuses.some((status) => status === value)
}

function isWorkspaceState(value: unknown): value is WorkspaceState {
  return typeof value === 'string' && workspaceStates.some((state) => state === value)
}

function isMessageAuthorKind(value: unknown): value is MessageAuthorKind {
  return typeof value === 'string' && messageAuthorKinds.some((kind) => kind === value)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === 'string')
}

const maxImageEdge = 2000

async function optimizeImage(file: File): Promise<File> {
  if (!file.type.startsWith('image/')) {
    throw new Error('只能粘贴图片文件')
  }
  const bitmap = await createImageBitmap(file)
  try {
    const scale = Math.min(1, maxImageEdge / Math.max(bitmap.width, bitmap.height))
    const width = Math.max(1, Math.round(bitmap.width * scale))
    const height = Math.max(1, Math.round(bitmap.height * scale))
    if (scale === 1 && file.type === 'image/png') {
      return new File([file], optimizedName(file.name, 'png'), { type: 'image/png' })
    }
    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
    const context = canvas.getContext('2d')
    if (context === null) {
      throw new Error('浏览器无法处理图片')
    }
    context.drawImage(bitmap, 0, 0, width, height)
    const mediaType = file.type === 'image/png' ? 'image/png' : 'image/jpeg'
    const blob = await canvasBlob(canvas, mediaType, mediaType === 'image/jpeg' ? 0.82 : undefined)
    return new File([blob], optimizedName(file.name, mediaType === 'image/png' ? 'png' : 'jpg'), { type: mediaType })
  } finally {
    bitmap.close()
  }
}

function canvasBlob(canvas: HTMLCanvasElement, mediaType: string, quality?: number): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (blob === null) {
        reject(new Error('图片压缩失败'))
        return
      }
      resolve(blob)
    }, mediaType, quality)
  })
}

function optimizedName(original: string, extension: string): string {
  const base = original.replace(/\.[^.]+$/, '') || 'pasted-image'
  return `${base}-optimized.${extension}`
}
