import { useMemo, useState } from 'react'
import { LinkIcon, XIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { Task, TaskAssociation } from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { TaskPicker } from './TaskPicker'

interface RelatedTasksProps {
  tasks: Task[]
  associations: TaskAssociation[]
  excludedTaskID?: string
  loading: boolean
  error: unknown
  pendingTaskIDs: Set<string>
  onAdd: (taskID: string) => Promise<void>
  onRemove: (taskID: string) => Promise<void>
  onOpenTask: (taskID: string) => void
}

export function RelatedTasks(props: RelatedTasksProps) {
  const { tasks, associations, excludedTaskID, loading, error, pendingTaskIDs, onAdd, onRemove, onOpenTask } = props
  const [mutationError, setMutationError] = useState('')
  const linkedTaskIDs = useMemo(() => new Set(associations.map((item) => item.task.id)), [associations])
  const candidates = tasks.filter((task) => task.id !== excludedTaskID && !linkedTaskIDs.has(task.id))

  async function add(task: Task) {
    setMutationError('')
    try {
      await onAdd(task.id)
    } catch (addError: unknown) {
      setMutationError(errorMessage(addError))
    }
  }

  async function remove(taskID: string) {
    setMutationError('')
    try {
      await onRemove(taskID)
    } catch (removeError: unknown) {
      setMutationError(errorMessage(removeError))
    }
  }

  return (
    <section className="py-5" aria-label="关联 Task">
      <div className="mb-3 flex items-center gap-2">
        <h3 className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
          <LinkIcon className="size-4" />关联 Task
        </h3>
        <Badge variant="outline">{associations.length}</Badge>
        <div className="ml-auto">
          <TaskPicker
            tasks={candidates}
            selectedTaskIDs={[]}
            triggerLabel="添加关联"
            closeOnSelect
            align="right"
            disabled={loading || pendingTaskIDs.size > 0}
            onSelect={(task) => { void add(task) }}
          />
        </div>
      </div>
      {loading ? <p className="text-sm text-muted-foreground">正在加载关联…</p> : null}
      {!loading && error ? <p className="text-sm text-destructive">{errorMessage(error)}</p> : null}
      {!loading && !error && associations.length === 0 ? (
        <p className="text-sm text-muted-foreground">还没有关联 Task。</p>
      ) : null}
      {!loading && !error && associations.length > 0 ? (
        <div className="grid gap-2 sm:grid-cols-2">
          {associations.map((association) => (
            <div className="flex min-w-0 items-center rounded-lg border bg-card" key={association.task.id}>
              <button
                className="min-w-0 flex-1 px-3 py-2 text-left hover:bg-muted/60"
                type="button"
                aria-label={`打开关联 Task ${association.task.key}`}
                onClick={() => onOpenTask(association.task.id)}
              >
                <span className="block font-mono text-[11px] text-muted-foreground">{association.task.key}</span>
                <span className="block truncate text-sm font-medium">{association.task.title}</span>
              </button>
              <Button
                className="mr-1"
                variant="ghost"
                size="icon-sm"
                type="button"
                aria-label={`移除关联 ${association.task.key}`}
                disabled={pendingTaskIDs.has(association.task.id)}
                onClick={() => { void remove(association.task.id) }}
              >
                <XIcon />
              </Button>
            </div>
          ))}
        </div>
      ) : null}
      {mutationError ? <p className="mt-2 text-xs text-destructive" role="alert">{mutationError}</p> : null}
    </section>
  )
}
