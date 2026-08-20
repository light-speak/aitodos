import { useState } from 'react'
import { CircleHelpIcon, RefreshCwIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { Clarification, ClarificationAnswerInput, Task } from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from './ui/dialog'
import { Textarea } from './ui/textarea'

type AnswerValue = Omit<ClarificationAnswerInput, 'expected_version'>

interface AnswerCardProps {
	item: Clarification
	answering: boolean
	onAnswer: (input: AnswerValue) => Promise<void>
	compact?: boolean
}

export function ClarificationAnswerCard({ item, answering, onAnswer, compact = false }: AnswerCardProps) {
	const [selectedOptionID, setSelectedOptionID] = useState('')
	const [customAnswer, setCustomAnswer] = useState('')
	const [error, setError] = useState('')
	const answered = item.status === 'ANSWERED'

	async function submit() {
		if (selectedOptionID === '' && customAnswer.trim() === '') {
			setError('请选择一个选项或填写自定义回答')
			return
		}
		setError('')
		try {
			await onAnswer({ selected_option_id: selectedOptionID, custom_answer: customAnswer.trim() })
		} catch (submitError: unknown) {
			setError(errorMessage(submitError))
		}
	}

	return (
		<section className={`rounded-xl border bg-card ${compact ? 'p-3' : 'p-4'}`}>
			<div className="mb-3 flex flex-wrap items-center gap-2">
				<Badge variant="outline">{categoryLabel(item.category)}</Badge>
				{answered ? <Badge variant="secondary">已回答</Badge> : <Badge>等待回答</Badge>}
			</div>
			<p className="text-sm font-medium leading-6">{item.question}</p>
			{answered ? (
				<div className="mt-3 grid gap-2">
					<p className="rounded-lg bg-muted/50 px-3 py-2 text-sm">{answeredText(item)}</p>
					<p className="break-all font-mono text-[11px] leading-5 text-muted-foreground">
						来源 Run {item.source_run_id}{item.continuation_run_id ? ` · 续跑 ${item.continuation_run_id}` : ''}
					</p>
				</div>
			) : (
				<div className="mt-3 grid gap-3">
					<div className="grid gap-2">
						{item.options.map((option) => (
							<label className="flex cursor-pointer items-start gap-3 rounded-lg border px-3 py-2.5 has-checked:border-primary has-checked:bg-primary/5" key={option.id}>
								<input
									type="radio"
									name={`clarification-${item.id}`}
									value={option.id}
									checked={selectedOptionID === option.id}
									onChange={() => { setSelectedOptionID(option.id); setCustomAnswer('') }}
									className="mt-1 accent-primary"
								/>
								<span className="min-w-0 text-sm">
									<span className="flex flex-wrap items-center gap-2 font-medium">
										{option.label}
										{item.recommended_option_id === option.id ? <Badge variant="secondary">推荐</Badge> : null}
									</span>
									{option.description ? <span className="mt-0.5 block text-xs leading-5 text-muted-foreground">{option.description}</span> : null}
								</span>
							</label>
						))}
					</div>
					{item.allow_custom_answer ? (
						<div className="grid gap-1.5">
							<label className="text-xs font-medium text-muted-foreground" htmlFor={`custom-answer-${item.id}`}>自定义回答</label>
							<Textarea
								id={`custom-answer-${item.id}`}
								value={customAnswer}
								onChange={(event) => { setCustomAnswer(event.currentTarget.value); setSelectedOptionID('') }}
								placeholder="需要补充约束时直接填写"
								className="min-h-20 resize-y"
							/>
						</div>
					) : null}
					{error ? <p className="text-sm text-destructive" role="alert">{error}</p> : null}
					<div className="flex justify-end">
						<Button type="button" disabled={answering} onClick={() => { void submit() }}>
							{answering ? '提交中…' : '回答并继续'}
						</Button>
					</div>
				</div>
			)}
		</section>
	)
}

export function TaskClarificationsPanel(props: {
	items: Clarification[]
	error: unknown
	answeringID: string | null
	onReload: () => void
	onAnswer: (item: Clarification, input: AnswerValue) => Promise<void>
}) {
	if (props.items.length === 0 && !props.error) return null
	return (
		<section className="py-5">
			<div className="mb-3 flex items-center justify-between gap-3">
				<h3 className="flex items-center gap-2 text-xs font-medium tracking-wide text-muted-foreground uppercase"><CircleHelpIcon className="size-4" />Agent 提问</h3>
				{props.error ? <Button variant="ghost" size="sm" onClick={props.onReload}><RefreshCwIcon />重试</Button> : null}
			</div>
			{props.error ? <p className="mb-3 text-sm text-destructive">{errorMessage(props.error)}</p> : null}
			<div className="grid gap-3">
				{props.items.map((item) => <ClarificationAnswerCard key={item.id} item={item} answering={props.answeringID === item.id} onAnswer={(input) => props.onAnswer(item, input)} />)}
			</div>
		</section>
	)
}

export function ClarificationInboxDialog(props: {
	items: Clarification[]
	tasks: Task[]
	answeringID: string | null
	onClose: () => void
	onOpenTask: (taskID: string) => void
	onAnswer: (item: Clarification, input: AnswerValue) => Promise<void>
}) {
	return (
		<Dialog open onOpenChange={(open) => { if (!open) props.onClose() }}>
			<DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-2xl">
				<DialogHeader>
					<DialogTitle>等待你的回答</DialogTitle>
					<DialogDescription>回答后 Task 会自动回到队列，由新的 Run 携带这次答案继续执行。</DialogDescription>
				</DialogHeader>
				{props.items.length === 0 ? <p className="rounded-lg bg-muted/50 p-4 text-sm text-muted-foreground">当前没有待回答问题。</p> : (
					<div className="grid gap-4">
						{props.items.map((item) => (
							<div className="grid gap-2" key={item.id}>
								<Button variant="ghost" className="h-auto justify-start px-1 text-sm" onClick={() => props.onOpenTask(item.task_id)}>
									{taskLabel(props.tasks, item.task_id)}
								</Button>
								<ClarificationAnswerCard item={item} compact answering={props.answeringID === item.id} onAnswer={(input) => props.onAnswer(item, input)} />
							</div>
						))}
					</div>
				)}
			</DialogContent>
		</Dialog>
	)
}

function categoryLabel(category: Clarification['category']): string {
	return ({ REQUIREMENT: '需求', DECISION: '决策', ENVIRONMENT: '环境', VALIDATION: '验证' })[category]
}

function answeredText(item: Clarification): string {
	if (item.custom_answer) return item.custom_answer
	return item.options.find((option) => option.id === item.selected_option_id)?.label ?? '已回答'
}

function taskLabel(tasks: Task[], taskID: string): string {
	const task = tasks.find((candidate) => candidate.id === taskID)
	return task ? `${task.key} · ${task.title}` : taskID
}
