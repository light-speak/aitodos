import { Clock3Icon, MessageSquareTextIcon } from 'lucide-react'

import type { Topic, TopicStatus } from '../types'
import { Badge } from './ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card'
import { Skeleton } from './ui/skeleton'

interface TopicListProps {
  topics: Topic[]
  loading: boolean
  onOpenTopic: (topicID: string) => void
}

const statusLabels: Record<TopicStatus, { phase: string; detail?: string }> = {
  OPEN: { phase: '讨论中' },
  NEEDS_CLARIFICATION: { phase: '讨论中', detail: '待澄清' },
  PLAN_REVIEW: { phase: '待确认', detail: '方案' },
  PLANNED: { phase: '已完成', detail: '已规划' },
  CLOSED: { phase: '已完成' },
}

export function TopicList({ topics, loading, onOpenTopic }: TopicListProps) {
  return (
    <section className="px-4 pb-8 sm:px-6 lg:px-8" aria-label="Topic 列表">
      {loading ? <LoadingTopics /> : null}
      {!loading && topics.length === 0 ? (
        <div className="flex min-h-48 items-center justify-center rounded-xl border border-dashed bg-muted/20 text-sm text-muted-foreground">
          暂无 Topic，创建一个议题开始讨论。
        </div>
      ) : null}
      {!loading && topics.length > 0 ? (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {topics.map((topic) => <TopicCard topic={topic} onOpenTopic={onOpenTopic} key={topic.id} />)}
        </div>
      ) : null}
    </section>
  )
}

function TopicCard({ topic, onOpenTopic }: { topic: Topic; onOpenTopic: (topicID: string) => void }) {
  function openTopic() {
    onOpenTopic(topic.id)
  }

  function handleKeyDown(event: React.KeyboardEvent) {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      openTopic()
    }
  }

  return (
    <Card
      className="cursor-pointer gap-4 shadow-xs transition hover:-translate-y-0.5 hover:ring-foreground/20 hover:shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      role="button"
      tabIndex={0}
      aria-label={`${topic.key} ${topic.title}`}
      onClick={openTopic}
      onKeyDown={handleKeyDown}
    >
      <CardHeader className="gap-3">
        <div className="flex items-center justify-between gap-3">
          <span className="font-mono text-[11px] font-medium text-muted-foreground">{topic.key}</span>
          <span className="flex items-center gap-1">
            <Badge variant="secondary">{statusLabels[topic.status].phase}</Badge>
            {statusLabels[topic.status].detail ? <Badge variant="outline">{statusLabels[topic.status].detail}</Badge> : null}
          </span>
        </div>
        <CardTitle className="text-base leading-6">{topic.title}</CardTitle>
        {topic.description ? (
          <CardDescription className="line-clamp-3 leading-6">{topic.description}</CardDescription>
        ) : null}
      </CardHeader>
      <CardContent className="flex items-center justify-between text-[11px] text-muted-foreground">
        <span className="flex items-center gap-1.5"><MessageSquareTextIcon className="size-3.5" />查看讨论</span>
        <span className="flex items-center gap-1.5"><Clock3Icon className="size-3.5" />{formatDate(topic.updated_at)}</span>
      </CardContent>
    </Card>
  )
}

function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '更新时间未知' : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium' }).format(date)
}

function LoadingTopics() {
  return (
    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3" aria-label="正在加载 Topic">
      {[0, 1, 2].map((item) => (
        <Card className="gap-4" key={item}>
          <CardHeader className="gap-3">
            <Skeleton className="h-3 w-24" />
            <Skeleton className="h-5 w-4/5" />
            <Skeleton className="h-3 w-full" />
          </CardHeader>
        </Card>
      ))}
    </div>
  )
}
