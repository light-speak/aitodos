import { GitBranchIcon, ListTodoIcon, MessageSquareIcon, PlusIcon, SquareKanbanIcon, TagIcon } from 'lucide-react'

import { shortGitSHA } from '../lib/utils'
import type { ProjectInfo, Release, RepositoryInfo } from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Separator } from './ui/separator'

interface AppHeaderProps {
  project: ProjectInfo | null
  topicCount: number
  taskCount: number
  repository: RepositoryInfo | null
  latestRelease: Release | null
  onOpenReleases: () => void
  onCreate: () => void
}

export function AppHeader(props: AppHeaderProps) {
  const { project, topicCount, taskCount, repository, latestRelease, onOpenReleases, onCreate } = props
  const branchSummary = repository === null
    ? '正在读取…'
    : `${repository.current_branch || 'detached'} @ ${shortGitSHA(repository.head_sha)}`
  return (
    <header className="border-b bg-card">
      <div className="flex min-h-20 items-center gap-4 px-4 sm:px-6 lg:px-8">
        <div className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-primary text-primary-foreground shadow-xs" aria-hidden="true">
          <SquareKanbanIcon className="size-5" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h1 className="truncate font-heading text-lg font-semibold tracking-tight">{project?.name ?? 'AiTodos'}</h1>
            <Badge variant="secondary" className="hidden sm:inline-flex">本地项目</Badge>
          </div>
          <p className="truncate font-mono text-xs text-muted-foreground">{project?.root ?? '正在读取项目…'}</p>
        </div>
        <div className="hidden items-center gap-4 lg:flex" aria-label="项目状态">
          <Metric icon={<MessageSquareIcon />} label="Topics" value={String(topicCount)} />
          <Separator orientation="vertical" className="h-8" />
          <Metric icon={<ListTodoIcon />} label="Tasks" value={String(taskCount)} />
          <Separator orientation="vertical" className="h-8" />
          <Metric icon={<GitBranchIcon />} label={repository?.dirty ? 'Git · 有修改' : 'Git'} value={branchSummary} />
          <Separator orientation="vertical" className="h-8" />
          <Metric icon={<TagIcon />} label="Release" value={latestRelease?.tag_name ?? '尚未发布'} />
        </div>
        <Button variant="outline" size="lg" type="button" aria-label="Releases" onClick={onOpenReleases}>
          <TagIcon />Releases
        </Button>
        <Button size="lg" type="button" aria-label="新建事项" onClick={onCreate}>
          <PlusIcon />
          新建事项
          <kbd className="ml-1 hidden min-w-5 rounded border border-white/20 bg-white/10 px-1 py-0.5 font-mono text-[10px] leading-none sm:inline-block" aria-hidden="true">N</kbd>
        </Button>
      </div>
    </header>
  )
}

function Metric({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="flex min-w-20 max-w-44 items-center gap-2.5">
      <span className="text-muted-foreground [&_svg]:size-4">{icon}</span>
      <span className="min-w-0">
        <span className="block truncate text-sm font-medium leading-4">{value}</span>
        <span className="block text-[11px] leading-4 text-muted-foreground">{label}</span>
      </span>
    </div>
  )
}
