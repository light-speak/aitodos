import { useState } from 'react'
import type { FormEvent, KeyboardEvent } from 'react'
import { ListTodoIcon, MessageSquareTextIcon, XIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { CreateTaskInput, CreateTopicInput } from '../types'
import { Button } from './ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from './ui/dialog'
import { Label } from './ui/label'
import { MarkdownTextarea } from './MarkdownTextarea'

type ItemKind = 'topic' | 'task'

interface CreateItemDialogProps {
  onClose: () => void
  onCreateTopic: (input: CreateTopicInput) => Promise<void>
  onCreateTask: (input: CreateTaskInput) => Promise<void>
}

export function CreateItemDialog({ onClose, onCreateTopic, onCreateTask }: CreateItemDialogProps) {
  const [kind, setKind] = useState<ItemKind>('topic')
  const [content, setContent] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
	if (submitting) return
    const normalized = content.trim()
    if (!normalized) {
      setError('请输入你想做的内容')
      return
    }
    setSubmitting(true)
    setError('')
    try {
      if (kind === 'topic') {
        await onCreateTopic({ title: '', description: normalized })
      } else {
		await onCreateTask({ title: '', description: normalized, acceptance_criteria: '', priority: 2 })
      }
      onClose()
    } catch (submitError: unknown) {
      setError(errorMessage(submitError))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="gap-0 overflow-hidden p-0 sm:max-w-2xl" showCloseButton={false}>
        <DialogHeader className="border-b px-6 py-5">
          <div className="flex items-start justify-between gap-4">
            <div className="space-y-1.5">
              <DialogTitle className="text-lg">新建事项</DialogTitle>
              <DialogDescription>只需要描述你想做什么，标题会自动生成。</DialogDescription>
            </div>
            <Button variant="ghost" size="icon-sm" type="button" aria-label="关闭" onClick={onClose}>
              <XIcon />
            </Button>
          </div>
        </DialogHeader>
        <form
          noValidate
          onKeyDown={submitWithModifierEnter}
          onSubmit={(event) => { void handleSubmit(event) }}
        >
          <div className="grid gap-5 px-6 py-5">
            <KindSelector value={kind} onChange={setKind} />
            <div className="grid gap-2">
              <Label htmlFor="item-content">你想做什么？</Label>
              <MarkdownTextarea
                id="item-content"
                value={content}
                size="large"
                rows={7}
                autoFocus
                aria-invalid={error ? true : undefined}
                placeholder="描述需求、问题、想法或一项明确的工作…"
                onChange={setContent}
              />
            </div>
            {error ? (
              <p className="rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-2 text-sm text-destructive" role="alert">
                {error}
              </p>
            ) : null}
          </div>
          <DialogFooter className="mx-0 mb-0 rounded-none px-6 py-4">
            <Button variant="outline" type="button" onClick={onClose}>取消</Button>
            <Button type="submit" disabled={submitting}>{submitting ? '创建中…' : '创建事项'}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function submitWithModifierEnter(event: KeyboardEvent<HTMLFormElement>) {
  if (event.key !== 'Enter' || (!event.metaKey && !event.ctrlKey)) return
  event.preventDefault()
  event.currentTarget.requestSubmit()
}

function KindSelector({ value, onChange }: { value: ItemKind; onChange: (kind: ItemKind) => void }) {
  return (
    <div className="grid grid-cols-2 gap-2" role="radiogroup" aria-label="事项类型">
      <KindOption
        selected={value === 'topic'}
        icon={<MessageSquareTextIcon />}
        title="Topic"
        description="先讨论和规划"
        onClick={() => onChange('topic')}
      />
      <KindOption
        selected={value === 'task'}
        icon={<ListTodoIcon />}
        title="Task"
        description="明确后直接执行"
        onClick={() => onChange('task')}
      />
    </div>
  )
}

function KindOption(props: { selected: boolean; icon: React.ReactNode; title: string; description: string; onClick: () => void }) {
  return (
    <button
      className={`rounded-xl border p-3 text-left transition ${props.selected ? 'border-primary bg-primary/5 ring-1 ring-primary' : 'hover:bg-muted/50'}`}
      type="button"
      role="radio"
      aria-checked={props.selected}
      onClick={props.onClick}
    >
      <span className="mb-2 flex items-center gap-2 text-sm font-medium [&_svg]:size-4">{props.icon}{props.title}</span>
      <span className="block text-xs text-muted-foreground">{props.description}</span>
    </button>
  )
}
