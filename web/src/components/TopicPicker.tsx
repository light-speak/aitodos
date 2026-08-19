import { useMemo, useState } from 'react'
import { CheckIcon, LinkIcon, SearchIcon } from 'lucide-react'

import type { Topic } from '../types'
import { Button } from './ui/button'
import { Input } from './ui/input'

interface TopicPickerProps {
  topics: Topic[]
  disabled?: boolean
  onSelect: (topic: Topic) => void
}

export function TopicPicker({ topics, disabled, onSelect }: TopicPickerProps) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const filteredTopics = useMemo(() => filterTopics(topics, query), [query, topics])

  function select(topic: Topic) {
    onSelect(topic)
    setOpen(false)
    setQuery('')
  }

  return (
    <div className="relative">
      <Button
        variant="outline"
        size="sm"
        type="button"
        aria-expanded={open}
        disabled={disabled || topics.length === 0}
        onClick={() => setOpen((current) => !current)}
      >
        <LinkIcon />添加 Topic
      </Button>
      {open ? (
        <div className="absolute top-full right-0 z-20 mt-2 w-[min(24rem,calc(100vw-2rem))] rounded-xl border bg-popover p-2 shadow-lg">
          <label className="flex items-center gap-2 rounded-lg border bg-background px-2.5">
            <SearchIcon className="size-4 text-muted-foreground" />
            <Input
              className="border-0 px-0 shadow-none focus-visible:ring-0"
              aria-label="搜索 Topic"
              value={query}
              placeholder="按编号或标题搜索"
              onChange={(event) => setQuery(event.target.value)}
            />
          </label>
          <div className="mt-2 max-h-52 overflow-y-auto" role="list">
            {filteredTopics.length === 0 ? (
              <p className="px-3 py-5 text-center text-xs text-muted-foreground">没有可选 Topic</p>
            ) : filteredTopics.map((topic) => (
              <button
                className="flex w-full items-start gap-2 rounded-lg px-3 py-2 text-left hover:bg-muted"
                type="button"
                key={topic.id}
                onClick={() => select(topic)}
              >
                <CheckIcon className="mt-0.5 size-4 shrink-0 opacity-0" />
                <span className="min-w-0">
                  <span className="block font-mono text-[11px] text-muted-foreground">{topic.key}</span>
                  <span className="block truncate text-sm">{topic.title}</span>
                </span>
              </button>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  )
}

function filterTopics(topics: Topic[], query: string): Topic[] {
  const normalized = query.trim().toLocaleLowerCase()
  if (!normalized) return topics
  return topics.filter((topic) => `${topic.key} ${topic.title}`.toLocaleLowerCase().includes(normalized))
}
