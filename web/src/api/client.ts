import { messageAuthorKinds, releaseStatuses, taskStatuses, topicStatuses, workspaceStates } from '../types'
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
} from '../types'

interface RequestOptions {
  method?: 'GET' | 'POST' | 'DELETE'
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

export async function getTasks(signal?: AbortSignal): Promise<Task[]> {
  const payload = await requestJSON('/api/tasks', { signal })
  if (!Array.isArray(payload)) {
    throw new ApiError('Task 列表格式无效', 502, 'INVALID_RESPONSE')
  }
  return payload.map(parseTask)
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

export async function queueTask(task: Task): Promise<Task> {
  return parseTask(
    await requestJSON(`/api/tasks/${encodeURIComponent(task.id)}/queue`, {
      method: 'POST',
      body: { version: task.version },
    }),
  )
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
    typeof value.max_workers !== 'number'
  ) {
    throw new ApiError('Project 信息格式无效', 502, 'INVALID_RESPONSE')
  }
  return { name: value.name, root: value.root, agent: value.agent, max_workers: value.max_workers }
}

function parseTask(value: unknown): Task {
  if (!isTaskRecord(value)) {
    throw new ApiError('Task 信息格式无效', 502, 'INVALID_RESPONSE')
  }
  return {
    id: value.id,
    key: value.key,
    title: value.title,
    description: value.description,
    acceptance_criteria: value.acceptance_criteria,
    status: value.status,
    priority: value.priority,
    version: value.version,
    created_at: value.created_at,
    updated_at: value.updated_at,
    ...optionalTaskFields(value),
  }
}

function isTaskRecord(value: unknown): value is Record<string, unknown> & {
  id: string
  key: string
  title: string
  description: string
  acceptance_criteria: string
  status: TaskStatus
  priority: number
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
    typeof value.acceptance_criteria === 'string' &&
    isTaskStatus(value.status) &&
    typeof value.priority === 'number' &&
    typeof value.version === 'number' &&
    typeof value.created_at === 'string' &&
    typeof value.updated_at === 'string'
  )
}

function optionalTaskFields(value: Record<string, unknown>): Partial<Task> {
  const result: Partial<Task> = {}
  for (const field of ['target_branch', 'base_commit_sha', 'current_workspace_id', 'latest_run_id'] as const) {
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
    typeof value.current_branch !== 'string' ||
    typeof value.head_sha !== 'string' ||
    typeof value.dirty !== 'boolean' ||
    !Array.isArray(value.branches)
  ) {
    throw new ApiError('Git 仓库信息格式无效', 502, 'INVALID_RESPONSE')
  }
  const branches = value.branches.map((branch) => {
    if (!isRecord(branch) || typeof branch.name !== 'string' || typeof branch.head_sha !== 'string') {
      throw new ApiError('Git 分支信息格式无效', 502, 'INVALID_RESPONSE')
    }
    return { name: branch.name, head_sha: branch.head_sha }
  })
  return {
    current_branch: value.current_branch,
    head_sha: value.head_sha,
    dirty: value.dirty,
    branches,
    ...(typeof value.exact_tag === 'string' ? { exact_tag: value.exact_tag } : {}),
  }
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
