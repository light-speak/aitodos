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

export const topicStatuses = [
  'OPEN',
  'NEEDS_CLARIFICATION',
  'PLAN_REVIEW',
  'PLANNED',
  'CLOSED',
] as const

export type TopicStatus = (typeof topicStatuses)[number]

export const messageAuthorKinds = ['HUMAN', 'AGENT', 'SYSTEM'] as const

export type MessageAuthorKind = (typeof messageAuthorKinds)[number]

export interface Task {
  id: string
  key: string
  title: string
  description: string
  acceptance_criteria: string
  status: TaskStatus
  priority: number
  target_branch?: string
  base_commit_sha?: string
  current_workspace_id?: string
  latest_run_id?: string
  version: number
  created_at: string
  updated_at: string
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

export interface DiscussionMessage {
  id: string
  thread_id: string
  sequence: number
  author_kind: MessageAuthorKind
  content: string
  linked_task_ids: string[]
  created_at: string
}

export interface TaskAssociation {
  task: Task
  source_message_id?: string
  created_at: string
}

export interface TopicAssociation {
  topic: Topic
  source_message_id?: string
  created_at: string
}

export interface ProjectInfo {
  name: string
  root: string
  agent: string
  max_workers: number
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

export interface GitBranch {
  name: string
  head_sha: string
}

export interface RepositoryInfo {
  current_branch: string
  head_sha: string
  dirty: boolean
  exact_tag?: string
  branches: GitBranch[]
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
