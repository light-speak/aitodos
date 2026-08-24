import { useState } from 'react'
import { AlertCircleIcon, FolderGit2Icon, GitBranchIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import { shortGitSHA } from '../lib/utils'
import type { RepositoryInfo, Task, Workspace } from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Label } from './ui/label'

interface TaskWorkspacePanelProps {
  workspace: Workspace | null
  loading: boolean
  error: unknown
	task: Task
	repository: RepositoryInfo | null
	onUpdateTargetBranch: (targetBranch: string) => Promise<void>
}

export function TaskWorkspacePanel(props: TaskWorkspacePanelProps) {
	const initialBranch = taskTargetBranch(props.task, props.repository)
	const selectionKey = `${props.task.id}:${props.task.version}:${initialBranch}`
	const [branchDraft, setBranchDraft] = useState({ key: selectionKey, value: initialBranch })
	const targetBranch = branchDraft.key === selectionKey ? branchDraft.value : initialBranch
	const [saving, setSaving] = useState(false)
	const [branchError, setBranchError] = useState<unknown>(null)

  if (props.loading) {
    return <section className="py-5 text-sm text-muted-foreground">正在校验 Git Workspace…</section>
  }
  if (props.workspace === null) {
		const locked = Boolean(props.task.current_workspace_id)
    return (
      <section className="py-5">
        <div className="rounded-xl border border-dashed bg-muted/20 p-4">
          <div>
            <h3 className="flex items-center gap-2 text-sm font-medium"><FolderGit2Icon className="size-4" />Git Workspace</h3>
			<p className="mt-1 text-xs leading-5 text-muted-foreground">{props.repository?.has_head !== false ? 'Worker 领取 Task 后，系统会自动准备独立 Workspace。' : '仓库尚无 Commit；创建首个 Commit 后，Worker 才能准备 Workspace。'}</p>
          </div>
			<div className="mt-4 grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end">
				<div className="grid gap-1.5">
					<Label htmlFor={`task-target-branch-${props.task.id}`}>目标分支</Label>
					<select
						id={`task-target-branch-${props.task.id}`}
						className="h-9 w-full rounded-lg border bg-background px-3 font-mono text-sm outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
						value={targetBranch}
						disabled={locked || saving || props.repository === null || props.repository.branches.length === 0}
						onChange={(event) => setBranchDraft({ key: selectionKey, value: event.target.value })}
					>
						{branchOptions(props.task, props.repository).map((branch) => <option value={branch} key={branch}>{branch}</option>)}
					</select>
				</div>
				<Button
					variant="outline"
					type="button"
					disabled={locked || saving || targetBranch === initialBranch || targetBranch === ''}
					onClick={() => { void saveTargetBranch(props, targetBranch, setSaving, setBranchError) }}
				>
					{saving ? '保存中…' : '保存目标分支'}
				</Button>
			</div>
			<p className="mt-2 text-xs leading-5 text-muted-foreground">Workspace 创建后目标分支将被锁定。</p>
		</div>
		<WorkspaceError error={branchError} />
        <WorkspaceError error={props.error} />
      </section>
    )
  }

  const item = props.workspace
	const status = workspaceStatus(item)
  return (
    <section className="py-5">
      <div className="mb-3 flex items-center justify-between gap-3">
        <h3 className="flex items-center gap-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">
          <FolderGit2Icon className="size-4" />Git Workspace
        </h3>
				<div className="flex items-center gap-2">
					<Badge variant="outline" className={status.tone}>{status.label}</Badge>
				</div>
      </div>
      <div className="grid gap-3 rounded-xl border bg-muted/20 p-4 text-sm">
        <WorkspaceFact icon={<GitBranchIcon />} label="工作分支" value={item.branch_name} />
        <WorkspaceFact label="HEAD" value={shortGitSHA(item.head_sha)} />
        <WorkspaceFact label="基线" value={`${item.target_branch} @ ${shortGitSHA(item.base_commit_sha)}`} />
        <WorkspaceFact label="路径" value={item.path} />
      </div>
      {item.failure_message ? <WorkspaceError error={new Error(item.failure_message)} /> : null}
      <WorkspaceError error={props.error} />
    </section>
  )
}

async function saveTargetBranch(
	props: TaskWorkspacePanelProps,
	targetBranch: string,
	setSaving: (saving: boolean) => void,
	setError: (error: unknown) => void,
) {
	setSaving(true)
	setError(null)
	try {
		await props.onUpdateTargetBranch(targetBranch)
	} catch (error: unknown) {
		setError(error)
	} finally {
		setSaving(false)
	}
}

function taskTargetBranch(task: Task, repository: RepositoryInfo | null): string {
	return task.target_branch || repository?.default_branch || repository?.branches[0]?.name || ''
}

function branchOptions(task: Task, repository: RepositoryInfo | null): string[] {
	const names = repository?.branches.map((branch) => branch.name) ?? []
	if (task.target_branch && !names.includes(task.target_branch)) return [task.target_branch, ...names]
	return names
}

function workspaceStatus(item: Workspace): { label: string; tone: string } {
	if (item.state === 'QUARANTINED') return { label: '已隔离', tone: 'border-rose-300 bg-rose-50 text-rose-700' }
	if (item.state === 'ERROR') return { label: '创建失败', tone: 'border-rose-300 bg-rose-50 text-rose-700' }
	if (item.state === 'PROVISIONING') return { label: '创建中', tone: 'border-blue-300 bg-blue-50 text-blue-700' }
	if (item.dirty) return { label: '有未提交修改', tone: 'border-amber-300 bg-amber-50 text-amber-700' }
	return { label: '工作区干净', tone: 'border-emerald-300 bg-emerald-50 text-emerald-700' }
}

function WorkspaceFact(props: { icon?: React.ReactNode; label: string; value: string }) {
  return (
    <div className="grid grid-cols-[88px_minmax(0,1fr)] gap-3">
      <span className="flex items-center gap-1.5 text-muted-foreground">{props.icon}{props.label}</span>
      <span className="break-all font-mono text-xs leading-5">{props.value}</span>
    </div>
  )
}

function WorkspaceError({ error }: { error: unknown }) {
  if (error === null) return null
  return <p className="mt-3 flex items-center gap-2 text-sm text-destructive" role="alert"><AlertCircleIcon className="size-4" />{errorMessage(error)}</p>
}
