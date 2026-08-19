import { useMemo, useState } from 'react'
import { CheckIcon, LinkIcon, SearchIcon } from 'lucide-react'

import type { Task } from '../types'
import { cn } from '../lib/utils'
import { Button } from './ui/button'
import { Input } from './ui/input'

interface TaskPickerProps {
  tasks: Task[]
  selectedTaskIDs: string[]
  triggerLabel: string
  disabled?: boolean
  closeOnSelect?: boolean
  placement?: 'top' | 'bottom'
  align?: 'left' | 'right'
  onSelect: (task: Task) => void
}

export function TaskPicker(props: TaskPickerProps) {
  const {
    tasks, selectedTaskIDs, triggerLabel, disabled, closeOnSelect = false,
    placement = 'bottom', align = 'left', onSelect,
  } = props
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const filteredTasks = useMemo(() => filterTasks(tasks, query), [query, tasks])

  function select(task: Task) {
    onSelect(task)
    if (closeOnSelect) {
      setOpen(false)
      setQuery('')
    }
  }

  return (
    <div className="relative">
      <Button
        variant="outline"
        size="sm"
        type="button"
        aria-expanded={open}
        disabled={disabled || tasks.length === 0}
        onClick={() => setOpen((current) => !current)}
      >
        <LinkIcon />{triggerLabel}
      </Button>
      {open ? (
        <div className={cn(
          'absolute z-20 w-[min(24rem,calc(100vw-2rem))] rounded-xl border bg-popover p-2 shadow-lg',
          placement === 'top' ? 'bottom-full mb-2' : 'top-full mt-2',
          align === 'left' ? 'left-0' : 'right-0',
        )}>
          <label className="flex items-center gap-2 rounded-lg border bg-background px-2.5">
            <SearchIcon className="size-4 text-muted-foreground" />
            <Input
              className="border-0 px-0 shadow-none focus-visible:ring-0"
              aria-label="搜索 Task"
              value={query}
              placeholder="按编号或标题搜索"
              onChange={(event) => setQuery(event.target.value)}
            />
          </label>
          <div className="mt-2 max-h-52 overflow-y-auto" role="list">
            {filteredTasks.length === 0 ? (
              <p className="px-3 py-5 text-center text-xs text-muted-foreground">没有可选 Task</p>
            ) : filteredTasks.map((task) => {
              const selected = selectedTaskIDs.includes(task.id)
              return (
                <button
                  className="flex w-full items-start gap-2 rounded-lg px-3 py-2 text-left hover:bg-muted"
                  type="button"
                  aria-pressed={selected}
                  key={task.id}
                  onClick={() => select(task)}
                >
                  <CheckIcon className={`mt-0.5 size-4 shrink-0 ${selected ? 'opacity-100' : 'opacity-0'}`} />
                  <span className="min-w-0">
                    <span className="block font-mono text-[11px] text-muted-foreground">{task.key}</span>
                    <span className="block truncate text-sm">{task.title}</span>
                  </span>
                </button>
              )
            })}
          </div>
        </div>
      ) : null}
    </div>
  )
}

function filterTasks(tasks: Task[], query: string): Task[] {
  const normalized = query.trim().toLocaleLowerCase()
  if (!normalized) return tasks
  return tasks.filter((task) => `${task.key} ${task.title}`.toLocaleLowerCase().includes(normalized))
}
