import type { KeyboardEvent } from 'react'
import { ArrowUpRightIcon } from 'lucide-react'

import type { Task, TaskStatus } from '../types'
import { Badge } from './ui/badge'
import { Card, CardDescription, CardFooter, CardHeader, CardTitle } from './ui/card'
import { Skeleton } from './ui/skeleton'

interface KanbanBoardProps {
  tasks: Task[]
  loading: boolean
  onOpenTask: (taskID: string) => void
}

const columns: ReadonlyArray<{ id: string; statuses: readonly TaskStatus[]; label: string; dot: string }> = [
  { id: 'todo', statuses: ['BACKLOG', 'READY', 'CHANGES_REQUESTED', 'BLOCKED'], label: '待办', dot: 'bg-zinc-400' },
  { id: 'doing', statuses: ['RUNNING'], label: '进行中', dot: 'bg-violet-500' },
  { id: 'review', statuses: ['REVIEW'], label: '待验收', dot: 'bg-amber-500' },
  { id: 'done', statuses: ['ACCEPTED'], label: '已完成', dot: 'bg-emerald-500' },
]

const statusLabels: Record<TaskStatus, string> = {
  BACKLOG: '待完善',
  READY: '可执行',
  RUNNING: '执行中',
  REVIEW: '待验收',
  ACCEPTED: '已完成',
  CHANGES_REQUESTED: '需修改',
  BLOCKED: '已阻塞',
  CANCELLED: '已取消',
}

export function KanbanBoard({ tasks, loading, onOpenTask }: KanbanBoardProps) {
  return (
    <div className="kanban-scrollbar overflow-x-auto px-4 pb-8 sm:px-6 lg:px-8">
      <div className="flex min-w-max items-start gap-3" aria-label="Task Kanban">
        {columns.map((column) => {
          const columnTasks = tasks.filter((task) => column.statuses.includes(task.status))
          return (
            <section
              className="w-[300px] rounded-xl border bg-muted/30 p-2.5"
              aria-label={column.label}
              key={column.id}
            >
              <header className="flex h-10 items-center gap-2 px-1.5">
                <span className={`size-2 rounded-full ${column.dot}`} aria-hidden="true" />
                <h3 className="text-sm font-medium">{column.label}</h3>
                <Badge variant="secondary" className="ml-auto min-w-6 justify-center px-1.5 font-mono text-[11px]">
                  {columnTasks.length}
                </Badge>
              </header>
              <div className="grid gap-2">
                {loading ? <LoadingCard /> : null}
                {!loading && columnTasks.length === 0 ? (
                  <div className="flex min-h-24 items-center justify-center rounded-lg border border-dashed bg-background/60 text-xs text-muted-foreground">
                    暂无任务
                  </div>
                ) : null}
                {columnTasks.map((task) => (
                  <TaskCard task={task} onOpenTask={onOpenTask} key={task.id} />
                ))}
              </div>
            </section>
          )
        })}
      </div>
    </div>
  )
}

function TaskCard({ task, onOpenTask }: { task: Task; onOpenTask: (taskID: string) => void }) {
  function openTask() {
    onOpenTask(task.id)
  }

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'Enter' && event.key !== ' ') return
    event.preventDefault()
    openTask()
  }

  return (
    <Card
      size="sm"
      role="button"
      tabIndex={0}
      aria-label={`${task.key} ${task.title}`}
      className="cursor-pointer gap-3 bg-card shadow-xs transition hover:-translate-y-0.5 hover:ring-foreground/20 hover:shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      onClick={openTask}
      onKeyDown={handleKeyDown}
    >
      <CardHeader className="gap-2">
        <div className="flex items-center justify-between gap-2">
          <span className="font-mono text-[11px] font-medium text-muted-foreground">{task.key}</span>
          <span className="flex items-center gap-1">
            <Badge variant="outline" className="h-5 px-1.5 text-[10px]">{statusLabels[task.status]}</Badge>
            <Badge variant={task.priority > 0 ? 'default' : 'outline'} className="h-5 px-1.5 font-mono text-[10px]">
              P{task.priority}
            </Badge>
          </span>
        </div>
        <CardTitle className="text-sm leading-5">{task.title}</CardTitle>
        {task.description ? (
          <CardDescription className="line-clamp-2 text-xs leading-5">{task.description}</CardDescription>
        ) : null}
      </CardHeader>
      <CardFooter className="justify-between border-0 bg-transparent pt-0 text-[11px] text-muted-foreground">
        <span>{task.current_workspace_id ? 'Workspace 已创建' : 'Workspace 未创建'}</span>
        <span className="flex items-center gap-1">查看详情 <ArrowUpRightIcon className="size-3" /></span>
      </CardFooter>
    </Card>
  )
}

function LoadingCard() {
  return (
    <Card size="sm" aria-label="正在加载任务">
      <CardHeader className="gap-3">
        <Skeleton className="h-3 w-20" />
        <Skeleton className="h-4 w-4/5" />
        <Skeleton className="h-3 w-full" />
      </CardHeader>
    </Card>
  )
}
