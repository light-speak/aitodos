import { useState } from 'react'
import type { FormEvent } from 'react'
import { AlertCircleIcon, BrainCircuitIcon, LockKeyholeIcon, PencilIcon, RefreshCwIcon, SplitIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { AssessmentScores, Task, TaskAssessment, TaskAssessmentState } from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from './ui/dialog'
import { Input } from './ui/input'
import { Label } from './ui/label'

interface TaskAssessmentPanelProps {
	task: Task
	state: TaskAssessmentState | null
	loading: boolean
	error: unknown
	busy: boolean
	onReload: () => void
	onUpdateTitle: (title: string) => Promise<void>
}

export function TaskAssessmentPanel(props: TaskAssessmentPanelProps) {
	const [editingTitle, setEditingTitle] = useState(false)
	return <section className="py-5"><header className="mb-4 flex items-center justify-between gap-3"><h3 className="flex items-center gap-2 text-xs font-medium tracking-wide text-muted-foreground uppercase"><BrainCircuitIcon className="size-4" />AI 复杂度评估</h3><div className="flex items-center gap-2">{props.task.title_locked ? <Badge variant="outline"><LockKeyholeIcon />人工锁定</Badge> : null}<Button variant="outline" size="sm" onClick={() => setEditingTitle(true)}><PencilIcon />编辑并锁定标题</Button></div></header><AssessmentContent state={props.state} loading={props.loading} error={props.error} onReload={props.onReload} />{editingTitle ? <TitleDialog task={props.task} busy={props.busy} onClose={() => setEditingTitle(false)} onSave={async (title) => { await props.onUpdateTitle(title); setEditingTitle(false) }} /> : null}</section>
}

function AssessmentContent(props: Pick<TaskAssessmentPanelProps, 'state' | 'loading' | 'error' | 'onReload'>) {
	if (props.loading) return <p className="text-sm text-muted-foreground">正在读取任务评估…</p>
	if (props.error) return <p className="flex items-center gap-2 text-sm text-destructive"><AlertCircleIcon className="size-4" />{errorMessage(props.error)}<Button variant="ghost" size="sm" onClick={props.onReload}><RefreshCwIcon />重试</Button></p>
	const current = props.state?.current
	if (!current) return <div className="rounded-xl border border-dashed p-4"><p className="text-sm font-medium">复杂度未知</p><p className="mt-1 text-xs text-muted-foreground">配置任务评估 Agent 并开启 Workers 后自动生成，不阻塞未配置时的正常执行。</p></div>
	return <div className="grid gap-4"><AssessmentHeader current={current} stale={props.state?.stale ?? false} /><DimensionGrid scores={current.scores} /><div className="rounded-xl border bg-muted/20 p-4"><p className="text-sm leading-6">{current.rationale}</p>{current.assumptions.length ? <div className="mt-3"><p className="text-xs font-medium text-muted-foreground">关键假设</p><ul className="mt-1 list-disc space-y-1 pl-5 text-xs text-muted-foreground">{current.assumptions.map((item) => <li key={item}>{item}</li>)}</ul></div> : null}</div>{current.split_recommended ? <div className="flex gap-3 rounded-xl border border-amber-300/50 bg-amber-50 p-4 text-sm text-amber-900"><SplitIcon className="mt-0.5 size-4 shrink-0" /><div><p className="font-medium">建议拆分 Task</p><p className="mt-1 leading-6">{current.split_rationale}</p></div></div> : null}</div>
}

function AssessmentHeader({ current, stale }: { current: TaskAssessment; stale: boolean }) {
	return <div className="flex flex-wrap items-center gap-3 rounded-xl border p-4"><Badge className="font-mono text-sm">{current.complexity}</Badge><Badge variant="secondary" className="font-mono text-sm">{current.autonomy}</Badge><div><p className="text-sm font-medium">加权分数 {current.weighted_score.toFixed(2)} / 4</p><p className="text-xs text-muted-foreground">{Math.round(current.confidence * 100)}% 置信度 · Revision {current.revision}</p></div>{stale ? <Badge variant="destructive" className="ml-auto">评估已过期</Badge> : null}</div>
}

const dimensions: ReadonlyArray<{ key: keyof AssessmentScores; label: string }> = [
	{ key: 'technical_complexity', label: '技术复杂度' },
	{ key: 'requirement_uncertainty', label: '需求不确定性' },
	{ key: 'change_scope', label: '修改范围' },
	{ key: 'validation_burden', label: '验证负担' },
	{ key: 'human_dependency', label: '人工依赖' },
	{ key: 'risk_and_reversibility', label: '风险与可逆性' },
]

function DimensionGrid({ scores }: { scores: AssessmentScores }) {
	return <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">{dimensions.map((item) => {
		const score = scores[item.key]
		return <div className="rounded-lg border px-3 py-2.5" key={item.key}>
			<div className="flex items-baseline justify-between gap-2"><p className="text-xs text-muted-foreground">{item.label}</p><p className="font-mono text-sm font-semibold">{score} / 4</p></div>
			<div
				className="mt-2 h-2 overflow-hidden rounded-full bg-muted"
				role="progressbar"
				aria-label={item.label}
				aria-valuemin={0}
				aria-valuemax={4}
				aria-valuenow={score}
			>
				<div className={`h-full rounded-full transition-[width] ${scoreColors[score]}`} style={{ width: `${(score + 1) * 20}%` }} />
			</div>
		</div>
	})}</div>
}

const scoreColors = ['bg-emerald-500', 'bg-green-500', 'bg-amber-400', 'bg-orange-500', 'bg-rose-600'] as const

function TitleDialog(props: { task: Task; busy: boolean; onClose: () => void; onSave: (title: string) => Promise<void> }) {
	const [title, setTitle] = useState(props.task.title)
	const [error, setError] = useState('')
	async function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); setError(''); try { await props.onSave(title) } catch (submitError: unknown) { setError(errorMessage(submitError)) } }
	return <Dialog open onOpenChange={(open) => { if (!open) props.onClose() }}><DialogContent><DialogHeader><DialogTitle>编辑 Task 标题</DialogTitle><DialogDescription>保存后标题来源变为人工并锁定，后续 Triage Agent 不再自动覆盖。</DialogDescription></DialogHeader><form className="grid gap-4" onSubmit={(event) => { void submit(event) }}><div className="grid gap-2"><Label htmlFor="task-title">Task 标题</Label><Input id="task-title" value={title} onChange={(event) => setTitle(event.currentTarget.value)} /></div>{error ? <p className="text-sm text-destructive">{error}</p> : null}<DialogFooter><Button type="button" variant="outline" onClick={props.onClose}>取消</Button><Button type="submit" disabled={props.busy}>保存并锁定</Button></DialogFooter></form></DialogContent></Dialog>
}
