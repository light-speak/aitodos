import { useMemo, useState } from 'react'
import { MessageSquareTextIcon, XIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { Topic, TopicAssociation } from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { TopicPicker } from './TopicPicker'

interface RelatedTopicsProps {
  topics: Topic[]
  associations: TopicAssociation[]
  loading: boolean
  error: unknown
  pendingTopicIDs: Set<string>
  onAdd: (topicID: string) => Promise<void>
  onRemove: (topicID: string) => Promise<void>
  onOpenTopic: (topicID: string) => void
}

export function RelatedTopics(props: RelatedTopicsProps) {
  const { topics, associations, loading, error, pendingTopicIDs, onAdd, onRemove, onOpenTopic } = props
  const [mutationError, setMutationError] = useState('')
  const linkedIDs = useMemo(() => new Set(associations.map((item) => item.topic.id)), [associations])
  const candidates = topics.filter((topic) => !linkedIDs.has(topic.id))

  async function mutate(action: () => Promise<void>) {
    setMutationError('')
    try {
      await action()
    } catch (mutation: unknown) {
      setMutationError(errorMessage(mutation))
    }
  }

  return (
    <section className="py-5" aria-label="关联 Topic">
      <div className="mb-3 flex items-center gap-2">
        <h3 className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
          <MessageSquareTextIcon className="size-4" />关联 Topic
        </h3>
        <Badge variant="outline">{associations.length}</Badge>
        <div className="ml-auto">
          <TopicPicker
            topics={candidates}
            disabled={loading || pendingTopicIDs.size > 0}
            onSelect={(topic) => { void mutate(() => onAdd(topic.id)) }}
          />
        </div>
      </div>
      {loading ? <p className="text-sm text-muted-foreground">正在加载关联…</p> : null}
      {!loading && error ? <p className="text-sm text-destructive">{errorMessage(error)}</p> : null}
      {!loading && !error && associations.length === 0 ? <p className="text-sm text-muted-foreground">还没有关联 Topic。</p> : null}
      {!loading && !error && associations.length > 0 ? (
        <div className="grid gap-2 sm:grid-cols-2">
          {associations.map((association) => (
            <div className="flex min-w-0 items-center rounded-lg border bg-card" key={association.topic.id}>
              <button
                className="min-w-0 flex-1 px-3 py-2 text-left hover:bg-muted/60"
                type="button"
                aria-label={`打开关联 Topic ${association.topic.key}`}
                onClick={() => onOpenTopic(association.topic.id)}
              >
                <span className="block font-mono text-[11px] text-muted-foreground">{association.topic.key}</span>
                <span className="block truncate text-sm font-medium">{association.topic.title}</span>
              </button>
              <Button
                className="mr-1"
                variant="ghost"
                size="icon-sm"
                type="button"
                aria-label={`移除关联 ${association.topic.key}`}
                disabled={pendingTopicIDs.has(association.topic.id)}
                onClick={() => { void mutate(() => onRemove(association.topic.id)) }}
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
