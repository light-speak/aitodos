import { AlertCircleIcon, FolderGit2Icon, GitBranchIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import { shortGitSHA } from '../lib/utils'
import type { Workspace } from '../types'
import { Badge } from './ui/badge'

interface TaskWorkspacePanelProps {
  workspace: Workspace | null
  loading: boolean
  error: unknown
	repositoryHasHead: boolean
}

export function TaskWorkspacePanel(props: TaskWorkspacePanelProps) {
  if (props.loading) {
    return <section className="py-5 text-sm text-muted-foreground">正在校验 Git Workspace…</section>
  }
  if (props.workspace === null) {
    return (
      <section className="py-5">
        <div className="rounded-xl border border-dashed bg-muted/20 p-4">
          <div>
            <h3 className="flex items-center gap-2 text-sm font-medium"><FolderGit2Icon className="size-4" />Git Workspace</h3>
			<p className="mt-1 text-xs leading-5 text-muted-foreground">{props.repositoryHasHead ? 'Worker 领取 Task 后，系统会自动准备独立 Workspace。' : '仓库尚无 Commit；创建首个 Commit 后，Worker 才能准备 Workspace。'}</p>
          </div>
		</div>
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
