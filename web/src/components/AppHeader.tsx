import { AlertCircleIcon, BellIcon, BellOffIcon, BotIcon, CircleHelpIcon, GitBranchIcon, ListTodoIcon, LoaderCircleIcon, MessageSquareIcon, PlusIcon, SearchIcon, Settings2Icon, SquareKanbanIcon, TagIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import { shortGitSHA } from '../lib/utils'
import type { AgentRun, ProjectInfo, Release, RepositoryInfo } from '../types'
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
	onOpenRepository: () => void
	onToggleWorkers: () => void
	onConfigureWorkers: () => void
	workersPending: boolean
	activeRuns: AgentRun[]
	agentActivityError: unknown
	onOpenRuns: () => void
	onReloadAgentActivity: () => void
	attentionCount: number
	onOpenClarifications: () => void
	notificationLabel: string
	notificationsEnabled: boolean
	notificationsSupported: boolean
	notificationsBlocked: boolean
	onToggleNotifications: () => void
	onOpenSearch: () => void
  onCreate: () => void
}

export function AppHeader(props: AppHeaderProps) {
	const { project, topicCount, taskCount, repository, latestRelease, onOpenReleases, onOpenRepository, onToggleWorkers, onConfigureWorkers, workersPending, activeRuns, agentActivityError, onOpenRuns, onReloadAgentActivity, attentionCount, onOpenClarifications, notificationLabel, notificationsEnabled, notificationsSupported, notificationsBlocked, onToggleNotifications, onOpenSearch, onCreate } = props
  const branchSummary = repository === null
    ? '正在读取…'
		: repository.has_head
			? `${repository.current_branch || 'detached'} @ ${shortGitSHA(repository.head_sha)}`
			: `${repository.current_branch || '未命名分支'} · 尚无 Commit`
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
			<Metric icon={<GitBranchIcon />} label={repository?.dirty ? 'Git · 有修改' : 'Git'} value={branchSummary} onClick={onOpenRepository} ariaLabel="Git 仓库信息" />
          <Separator orientation="vertical" className="h-8" />
          <Metric icon={<TagIcon />} label="Release" value={latestRelease?.tag_name ?? '尚未发布'} />
        </div>
		{activeRuns.length > 0 ? (
			<Button variant="secondary" size="lg" type="button" aria-label={`${activeRuns.length} 个 Agent 运行中`} onClick={onOpenRuns}>
				<LoaderCircleIcon className="animate-spin" />{activeRuns.length} 个 Agent 运行中
			</Button>
		) : agentActivityError ? (
			<Button variant="outline" size="lg" type="button" aria-label="Agent 状态未知" title={errorMessage(agentActivityError)} onClick={onReloadAgentActivity}>
				<AlertCircleIcon />Agent 状态未知
			</Button>
		) : null}
		{project && project.scheduler_failures > 0 ? (
			<Button variant="destructive" size="lg" type="button" aria-label="Worker 调度异常" title={project.scheduler_error} onClick={onOpenRuns}>
				<AlertCircleIcon />Worker 调度异常
			</Button>
		) : null}
		<Button variant="outline" size="lg" type="button" aria-label="搜索项目" onClick={onOpenSearch}>
			<SearchIcon />搜索<kbd className="ml-1 rounded border px-1 py-0.5 font-mono text-[10px] leading-none text-muted-foreground" aria-hidden="true">/</kbd>
		</Button>
		<Button
			variant={project?.workers_enabled ? 'default' : 'outline'}
			size="lg"
			type="button"
			aria-label={project?.workers_enabled ? '暂停 Workers' : '启动 Workers'}
			disabled={project === null || workersPending}
			onClick={onToggleWorkers}
		>
			<BotIcon />
			{workersPending ? '保存中…' : project?.workers_enabled ? `${project.max_workers} Workers` : 'Workers 已暂停'}
		</Button>
		<Button variant="ghost" size="icon-lg" type="button" aria-label="配置 Workers" disabled={project === null} onClick={onConfigureWorkers}>
			<Settings2Icon />
		</Button>
		<Button aria-label={attentionCount > 0 ? `待处理 ${attentionCount}` : '待处理'} variant={attentionCount > 0 ? 'default' : 'outline'} size="lg" type="button" onClick={onOpenClarifications}>
			<CircleHelpIcon />待处理
			{attentionCount > 0 ? <Badge variant="secondary">{attentionCount}</Badge> : null}
		</Button>
		{notificationsSupported ? <Button variant={notificationsEnabled ? 'secondary' : 'ghost'} size="icon-lg" type="button" aria-label={notificationLabel} title={notificationLabel} disabled={notificationsBlocked} onClick={onToggleNotifications}>{notificationsBlocked ? <BellOffIcon /> : <BellIcon />}</Button> : null}
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

function Metric(props: { icon: React.ReactNode; label: string; value: string; onClick?: () => void; ariaLabel?: string }) {
	const content = (
		<>
		<span className="text-muted-foreground [&_svg]:size-4">{props.icon}</span>
      <span className="min-w-0">
			<span className="block truncate text-sm font-medium leading-4">{props.value}</span>
			<span className="block text-[11px] leading-4 text-muted-foreground">{props.label}</span>
      </span>
		</>
	)
	if (props.onClick) {
		return <button className="flex min-w-20 max-w-44 items-center gap-2.5 rounded-lg text-left outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50" type="button" aria-label={props.ariaLabel} onClick={props.onClick}>{content}</button>
	}
	return (
		<div className="flex min-w-20 max-w-44 items-center gap-2.5">
			{content}
    </div>
  )
}
