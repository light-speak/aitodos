import { useMemo, useState } from 'react'
import type { FormEvent, KeyboardEvent } from 'react'
import { Clock3Icon, LinkIcon, MessageSquareTextIcon, SendIcon, XIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { DiscussionMessage, MessageAuthorKind, Task } from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { MarkdownContent } from './MarkdownContent'
import { MarkdownTextarea } from './MarkdownTextarea'
import { TaskPicker } from './TaskPicker'

interface DiscussionPanelProps {
  messages: DiscussionMessage[]
  tasks: Task[]
  excludedTaskID?: string
  loading: boolean
  submitting: boolean
  error: unknown
  onReload: () => void
  onSendMessage: (content: string, linkedTaskIDs: string[]) => Promise<void>
  onOpenTask: (taskID: string) => void
}

const authorLabels: Record<MessageAuthorKind, string> = {
  HUMAN: '你',
  AGENT: 'Agent',
  SYSTEM: '系统',
}

export function DiscussionMessages(props: Omit<DiscussionPanelProps, 'submitting' | 'onSendMessage'>) {
  const { messages, tasks, loading, error, onReload, onOpenTask } = props
  const tasksByID = useMemo(() => new Map(tasks.map((task) => [task.id, task])), [tasks])
  return (
    <section className="py-5" aria-label="讨论消息">
      <h3 className="mb-4 flex items-center gap-2 text-xs font-medium text-muted-foreground">
        <MessageSquareTextIcon className="size-4" />讨论记录
        <Badge variant="outline" className="ml-auto">{messages.length}</Badge>
      </h3>
      {loading ? <p className="py-8 text-center text-sm text-muted-foreground">正在加载讨论…</p> : null}
      {!loading && error ? (
        <div className="rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive" role="alert">
          <p>{errorMessage(error)}</p>
          <Button className="mt-2" variant="outline" size="sm" type="button" onClick={onReload}>重新加载</Button>
        </div>
      ) : null}
      {!loading && !error && messages.length === 0 ? (
        <p className="rounded-lg border border-dashed py-8 text-center text-sm text-muted-foreground">还没有讨论消息。</p>
      ) : null}
      {!loading && !error ? (
        <ol className="grid gap-3">
          {messages.map((message) => (
            <li className="rounded-xl border bg-card p-4" key={message.id}>
              <div className="mb-2 flex items-center justify-between gap-3 text-xs text-muted-foreground">
                <span className="font-medium text-foreground">{authorLabels[message.author_kind]}</span>
                <time className="flex items-center gap-1" dateTime={message.created_at}>
                  <Clock3Icon className="size-3.5" />{formatDate(message.created_at)}
                </time>
              </div>
              <MarkdownContent content={message.content} />
              {message.linked_task_ids.length > 0 ? (
                <div className="mt-3 flex flex-wrap gap-1.5 border-t pt-3">
                  {message.linked_task_ids.map((taskID) => {
                    const task = tasksByID.get(taskID)
                    return (
                      <Button
                        variant="secondary"
                        size="xs"
                        type="button"
                        aria-label={`打开 ${task?.key ?? taskID}`}
                        key={taskID}
                        onClick={() => onOpenTask(taskID)}
                      >
                        <LinkIcon />{task?.key ?? taskID}{task ? ` · ${task.title}` : ''}
                      </Button>
                    )
                  })}
                </div>
              ) : null}
            </li>
          ))}
        </ol>
      ) : null}
    </section>
  )
}

export function DiscussionComposer(props: Pick<DiscussionPanelProps, 'tasks' | 'excludedTaskID' | 'submitting' | 'onSendMessage'>) {
  const { tasks, excludedTaskID, submitting, onSendMessage } = props
  const [content, setContent] = useState('')
  const [linkedTaskIDs, setLinkedTaskIDs] = useState<string[]>([])
  const [submitError, setSubmitError] = useState('')
  const candidates = tasks.filter((task) => task.id !== excludedTaskID)

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (submitting) return
    const normalized = content.trim()
    if (!normalized) {
      setSubmitError('请输入讨论内容')
      return
    }
    setSubmitError('')
    try {
      await onSendMessage(normalized, linkedTaskIDs)
      setContent('')
      setLinkedTaskIDs([])
    } catch (sendError: unknown) {
      setSubmitError(errorMessage(sendError))
    }
  }

  function toggleTask(task: Task) {
    setLinkedTaskIDs((current) => current.includes(task.id)
      ? current.filter((taskID) => taskID !== task.id)
      : [...current, task.id])
  }

  return (
    <form
      className="border-t bg-muted/20 p-4 sm:px-6"
      noValidate
      onKeyDown={submitWithModifierEnter}
      onSubmit={(event) => { void handleSubmit(event) }}
    >
      <label className="mb-2 block text-xs font-medium text-muted-foreground" htmlFor="discussion-message-content">
        发表消息
      </label>
      <MarkdownTextarea
        id="discussion-message-content"
        value={content}
        maxLength={20000}
        rows={3}
        aria-invalid={submitError ? true : undefined}
        placeholder="补充背景、约束、问题或结论…"
        onChange={setContent}
      />
      {linkedTaskIDs.length > 0 ? (
        <div className="mt-2 flex flex-wrap gap-1.5">
          {linkedTaskIDs.map((taskID) => {
            const task = tasks.find((item) => item.id === taskID)
            if (task === undefined) return null
            return (
              <Badge variant="secondary" key={taskID}>
                {task.key}
                <button type="button" aria-label={`取消引用 ${task.key}`} onClick={() => toggleTask(task)}><XIcon /></button>
              </Badge>
            )
          })}
        </div>
      ) : null}
      <div className="mt-3 flex items-center gap-3">
        <TaskPicker
          tasks={candidates}
          selectedTaskIDs={linkedTaskIDs}
          triggerLabel="引用 Task"
          placement="top"
          closeOnSelect
          disabled={submitting}
          onSelect={toggleTask}
        />
        <span className="min-w-0 flex-1 text-xs text-destructive" role={submitError ? 'alert' : undefined}>{submitError}</span>
        <Button type="submit" disabled={submitting}>
          <SendIcon />{submitting ? '发送中…' : '发送消息'}
        </Button>
      </div>
    </form>
  )
}

function submitWithModifierEnter(event: KeyboardEvent<HTMLFormElement>) {
  if (event.key !== 'Enter' || (!event.metaKey && !event.ctrlKey)) return
  event.preventDefault()
  event.currentTarget.requestSubmit()
}

function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? '未知时间'
    : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}
