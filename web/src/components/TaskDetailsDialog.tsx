import { useState } from 'react'
import { Clock3Icon, ListChecksIcon, TextIcon, XIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { DiscussionMessage, Task, TaskAssociation, TaskStatus, Topic, TopicAssociation, Workspace } from '../types'
import { DiscussionComposer, DiscussionMessages } from './DiscussionPanel'
import { MarkdownContent } from './MarkdownContent'
import { RelatedTasks } from './RelatedTasks'
import { RelatedTopics } from './RelatedTopics'
import { TaskWorkspacePanel } from './TaskWorkspacePanel'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Separator } from './ui/separator'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from './ui/sheet'

interface TaskDetailsDialogProps {
  task: Task
  tasks: Task[]
  messages: DiscussionMessage[]
  associations: TaskAssociation[]
  topics: Topic[]
  topicAssociations: TopicAssociation[]
  pending: boolean
  discussionLoading: boolean
  relationLoading: boolean
  submitting: boolean
  discussionError: unknown
  relationError: unknown
  pendingRelationTaskIDs: Set<string>
  topicRelationLoading: boolean
  topicRelationError: unknown
  pendingRelationTopicIDs: Set<string>
  workspace: Workspace | null
  workspaceLoading: boolean
  workspaceCreating: boolean
  workspaceError: unknown
  onClose: () => void
  onQueue: (task: Task) => Promise<void>
  onReloadDiscussion: () => void
  onSendMessage: (content: string, linkedTaskIDs: string[]) => Promise<void>
  onAddRelation: (taskID: string) => Promise<void>
  onRemoveRelation: (taskID: string) => Promise<void>
  onOpenTask: (taskID: string) => void
  onAddTopicRelation: (topicID: string) => Promise<void>
  onRemoveTopicRelation: (topicID: string) => Promise<void>
  onOpenTopic: (topicID: string) => void
  onCreateWorkspace: () => Promise<void>
}

const statusLabels: Record<TaskStatus, string> = {
  BACKLOG: '待完善', READY: '可执行', RUNNING: '执行中', REVIEW: '待验收',
  ACCEPTED: '已验收', CHANGES_REQUESTED: '需修改', BLOCKED: '已阻塞', CANCELLED: '已取消',
}

const statusTone: Record<TaskStatus, string> = {
  BACKLOG: 'border-zinc-300 bg-zinc-50 text-zinc-700',
  READY: 'border-blue-200 bg-blue-50 text-blue-700',
  RUNNING: 'border-violet-200 bg-violet-50 text-violet-700',
  REVIEW: 'border-amber-200 bg-amber-50 text-amber-700',
  ACCEPTED: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  CHANGES_REQUESTED: 'border-orange-200 bg-orange-50 text-orange-700',
  BLOCKED: 'border-rose-200 bg-rose-50 text-rose-700',
  CANCELLED: 'border-zinc-300 bg-zinc-50 text-zinc-500',
}

export function TaskDetailsDialog(props: TaskDetailsDialogProps) {
  const { task, tasks, messages, associations, pending, onClose, onQueue, onOpenTask } = props
  const [commandError, setCommandError] = useState('')
  const canQueue = task.status === 'BACKLOG' || task.status === 'CHANGES_REQUESTED'

  async function handleQueue() {
    setCommandError('')
    try {
      await onQueue(task)
      onClose()
    } catch (queueError: unknown) {
      setCommandError(errorMessage(queueError))
    }
  }

  return (
    <Sheet open onOpenChange={(open) => { if (!open) onClose() }}>
      <SheetContent
        className="gap-0 data-[side=right]:w-full data-[side=right]:sm:w-[min(52rem,calc(100vw-3rem))] data-[side=right]:sm:max-w-none"
        showCloseButton={false}
      >
        <SheetHeader className="gap-3 border-b px-5 py-5 sm:px-6">
          <div className="flex items-start justify-between gap-4">
            <div className="min-w-0 space-y-1.5">
              <SheetDescription className="font-mono text-xs">{task.key}</SheetDescription>
              <SheetTitle className="text-xl leading-7">{task.title}</SheetTitle>
            </div>
            <Button variant="ghost" size="icon-sm" type="button" aria-label="关闭" onClick={onClose}><XIcon /></Button>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="outline" className={statusTone[task.status]}>{statusLabels[task.status]}</Badge>
            <Badge variant="secondary" className="font-mono">P{task.priority}</Badge>
            {canQueue ? (
              <Button className="ml-auto" type="button" disabled={pending} onClick={() => { void handleQueue() }}>
                {pending ? '正在加入队列…' : '加入执行队列'}
              </Button>
            ) : null}
          </div>
          {commandError ? <p className="text-sm text-destructive" role="alert">{commandError}</p> : null}
        </SheetHeader>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 sm:px-6">
          <DetailSection icon={<TextIcon />} title="Description" value={task.description} />
          <Separator />
          <DetailSection icon={<ListChecksIcon />} title="Acceptance Criteria" value={task.acceptance_criteria} />
          <Separator />
          <TaskWorkspacePanel
            workspace={props.workspace}
            loading={props.workspaceLoading}
            creating={props.workspaceCreating}
            error={props.workspaceError}
            onCreate={props.onCreateWorkspace}
          />
          <Separator />
          <dl className="grid gap-4 py-5 text-sm">
            <DetailItem icon={<Clock3Icon />} label="创建时间" value={formatDate(task.created_at)} />
          </dl>
          <Separator />
          <RelatedTopics
            topics={props.topics}
            associations={props.topicAssociations}
            loading={props.topicRelationLoading}
            error={props.topicRelationError}
            pendingTopicIDs={props.pendingRelationTopicIDs}
            onAdd={props.onAddTopicRelation}
            onRemove={props.onRemoveTopicRelation}
            onOpenTopic={props.onOpenTopic}
          />
          <Separator />
          <RelatedTasks
            tasks={tasks}
            associations={associations}
            excludedTaskID={task.id}
            loading={props.relationLoading}
            error={props.relationError}
            pendingTaskIDs={props.pendingRelationTaskIDs}
            onAdd={props.onAddRelation}
            onRemove={props.onRemoveRelation}
            onOpenTask={onOpenTask}
          />
          <Separator />
          <DiscussionMessages
            messages={messages}
            tasks={tasks}
            excludedTaskID={task.id}
            loading={props.discussionLoading}
            error={props.discussionError}
            onReload={props.onReloadDiscussion}
            onOpenTask={onOpenTask}
          />
        </div>

        <DiscussionComposer
          tasks={tasks}
          excludedTaskID={task.id}
          submitting={props.submitting}
          onSendMessage={props.onSendMessage}
        />
      </SheetContent>
    </Sheet>
  )
}

function DetailSection({ icon, title, value }: { icon: React.ReactNode; title: string; value: string }) {
  return (
    <section className="py-5">
      <h3 className="mb-3 flex items-center gap-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">
        <span className="[&_svg]:size-4">{icon}</span>{title}
      </h3>
      <MarkdownContent content={value} />
    </section>
  )
}

function DetailItem(props: { icon: React.ReactNode; label: string; value: string; mono?: boolean }) {
  return (
    <div className="grid grid-cols-[20px_88px_minmax(0,1fr)] items-start gap-2">
      <span className="text-muted-foreground [&_svg]:size-4">{props.icon}</span>
      <dt className="text-muted-foreground">{props.label}</dt>
      <dd className={`m-0 break-words ${props.mono ? 'font-mono text-xs leading-5' : ''}`}>{props.value}</dd>
    </div>
  )
}

function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? '未知'
    : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}
