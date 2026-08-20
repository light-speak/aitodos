import { useMemo, useState } from 'react'
import { CheckIcon, ClipboardListIcon, PlusIcon, RefreshCwIcon, Trash2Icon, XIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { CreatePlanRevisionInput, PlanTaskDraftInput, PlanView, Topic } from '../types'
import { MarkdownContent } from './MarkdownContent'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Textarea } from './ui/textarea'

interface PlanPanelProps {
	topic: Topic
	plan: PlanView | null
	loading: boolean
	submitting: boolean
	error: unknown
	onReload: () => void
	onSubmit: (input: CreatePlanRevisionInput) => Promise<void>
	onReject: (comment: string) => Promise<void>
	onApprove: (comment: string) => Promise<void>
}

export function PlanPanel(props: PlanPanelProps) {
	const [editing, setEditing] = useState(false)
	const [comment, setComment] = useState('')
	const [localError, setLocalError] = useState('')

	if (props.loading) return <section className="py-5 text-sm text-muted-foreground">正在读取方案…</section>
	const canEdit = props.plan === null || props.plan.plan.status === 'CHANGES_REQUESTED'
	if (editing && canEdit) {
		return <PlanEditor current={props.plan} submitting={props.submitting} onCancel={() => setEditing(false)} onSubmit={async (input) => {
			await props.onSubmit(input)
			setEditing(false)
		}} />
	}
	return (
		<section className="py-5" aria-label="Plan">
			<div className="mb-3 flex items-center justify-between gap-3">
				<h3 className="flex items-center gap-2 text-xs font-medium text-muted-foreground"><ClipboardListIcon className="size-4" />执行方案</h3>
				{canEdit ? <Button size="sm" type="button" onClick={() => setEditing(true)}>{props.plan ? '提交新修订' : '编写方案'}</Button> : null}
			</div>
			{props.error ? <div className="mb-3 flex items-center gap-2 rounded-lg border border-destructive/20 p-3 text-sm text-destructive">
				<span className="flex-1">{errorMessage(props.error)}</span><Button variant="ghost" size="icon-sm" onClick={props.onReload}><RefreshCwIcon /></Button>
			</div> : null}
			{props.plan === null ? <p className="rounded-xl border border-dashed p-4 text-sm text-muted-foreground">讨论稳定后提交方案；批准前 Task 草案不会进入 Worker 队列。</p> : (
				<div className="grid gap-4 rounded-xl border bg-muted/20 p-4">
					<div className="flex flex-wrap items-center gap-2"><Badge>{planStatus(props.plan)}</Badge><Badge variant="outline">Revision {props.plan.revision.revision}</Badge><span className="font-mono text-xs text-muted-foreground">{props.plan.plan.key}</span></div>
					<div><p className="mb-1 text-xs text-muted-foreground">方案摘要</p><MarkdownContent content={props.plan.revision.summary} /></div>
					{props.plan.revision.rationale ? <div><p className="mb-1 text-xs text-muted-foreground">取舍依据</p><MarkdownContent content={props.plan.revision.rationale} /></div> : null}
					{props.plan.revision.risks ? <div><p className="mb-1 text-xs text-muted-foreground">风险</p><MarkdownContent content={props.plan.revision.risks} /></div> : null}
					<div className="grid gap-2">{props.plan.revision.drafts.map((draft) => <article className="rounded-lg border bg-background p-3" key={draft.id}>
						<div className="flex items-start gap-2"><Badge variant="outline">P{draft.priority}</Badge><div className="min-w-0"><p className="font-medium">{draft.title}</p><p className="mt-1 text-sm text-muted-foreground">{draft.description}</p></div></div>
						{draft.acceptance_criteria ? <p className="mt-2 text-xs leading-5"><span className="text-muted-foreground">验收：</span>{draft.acceptance_criteria}</p> : null}
						{draft.test_cases.length > 0 ? <ul className="mt-2 list-disc pl-5 text-xs text-muted-foreground">{draft.test_cases.map((test) => <li key={test.id}>{test.title}</li>)}</ul> : null}
					</article>)}</div>
					{props.plan.reviews[0]?.comment ? <p className="rounded-lg bg-background px-3 py-2 text-sm"><span className="text-muted-foreground">最近反馈：</span>{props.plan.reviews[0].comment}</p> : null}
					{props.plan.plan.status === 'IN_REVIEW' ? <div className="grid gap-3 border-t pt-4">
						<Textarea value={comment} onChange={(event) => setComment(event.currentTarget.value)} placeholder="审核意见（要求修改时必填）" className="min-h-20" />
						{localError ? <p className="text-sm text-destructive">{localError}</p> : null}
						<div className="flex justify-end gap-2"><Button variant="outline" disabled={props.submitting} onClick={() => {
							if (!comment.trim()) { setLocalError('请填写需要修改的内容'); return }
							setLocalError(''); void props.onReject(comment).then(() => setComment('')).catch(() => undefined)
						}}><XIcon />要求修改</Button><Button disabled={props.submitting} onClick={() => { setLocalError(''); void props.onApprove(comment).then(() => setComment('')).catch(() => undefined) }}><CheckIcon />批准并创建 {props.plan.revision.drafts.length} 个 Task</Button></div>
					</div> : null}
				</div>
			)}
		</section>
	)
}

function PlanEditor({ current, submitting, onCancel, onSubmit }: { current: PlanView | null; submitting: boolean; onCancel: () => void; onSubmit: (input: CreatePlanRevisionInput) => Promise<void> }) {
	const initial = useMemo(() => editorInput(current), [current])
	const [value, setValue] = useState(initial)
	const [error, setError] = useState('')
	function updateDraft(index: number, next: PlanTaskDraftInput) { setValue((state) => ({ ...state, drafts: state.drafts.map((draft, position) => position === index ? next : draft) })) }
	return <section className="grid gap-4 py-5" aria-label="编辑 Plan">
		<div className="flex items-center justify-between"><h3 className="flex items-center gap-2 text-sm font-medium"><ClipboardListIcon className="size-4" />{current ? 'Plan 新修订' : 'Plan 草案'}</h3><Button variant="ghost" size="sm" onClick={onCancel}>取消</Button></div>
		<Textarea value={value.summary} onChange={(event) => setValue({ ...value, summary: event.currentTarget.value })} placeholder="方案摘要：要实现什么，如何拆分" className="min-h-28" />
		<div className="grid gap-3 sm:grid-cols-2"><Textarea value={value.rationale} onChange={(event) => setValue({ ...value, rationale: event.currentTarget.value })} placeholder="取舍依据" /><Textarea value={value.risks} onChange={(event) => setValue({ ...value, risks: event.currentTarget.value })} placeholder="风险与验证重点" /></div>
		<div className="grid gap-3">{value.drafts.map((draft, index) => <div className="grid gap-2 rounded-xl border p-3" key={index}>
			<div className="flex gap-2"><Input value={draft.title} onChange={(event) => updateDraft(index, { ...draft, title: event.currentTarget.value })} placeholder={`Task ${index + 1} 标题`} /><select className="h-8 rounded-lg border bg-background px-2 text-sm" value={draft.priority} onChange={(event) => updateDraft(index, { ...draft, priority: Number(event.currentTarget.value) })}>{[0, 1, 2, 3].map((priority) => <option value={priority} key={priority}>P{priority}</option>)}</select>{value.drafts.length > 1 ? <Button variant="ghost" size="icon-sm" aria-label={`删除 Task ${index + 1}`} onClick={() => setValue({ ...value, drafts: value.drafts.filter((_, position) => position !== index) })}><Trash2Icon /></Button> : null}</div>
			<Textarea value={draft.description} onChange={(event) => updateDraft(index, { ...draft, description: event.currentTarget.value })} placeholder="工作描述" />
			<Textarea value={draft.acceptance_criteria} onChange={(event) => updateDraft(index, { ...draft, acceptance_criteria: event.currentTarget.value })} placeholder="验收标准" />
			<Textarea value={draft.test_cases.map((test) => test.title).join('\n')} onChange={(event) => updateDraft(index, { ...draft, test_cases: event.currentTarget.value.split('\n').filter((line) => line.trim()).map((title) => ({ title: title.trim(), description: '', required: true })) })} placeholder={'必测项，每行一项\n例如：刷新后筛选条件保持'} className="min-h-20" />
		</div>)}</div>
		<Button variant="outline" onClick={() => setValue({ ...value, drafts: [...value.drafts, emptyDraft()] })}><PlusIcon />增加 Task 草案</Button>
		{error ? <p className="text-sm text-destructive">{error}</p> : null}
		<div className="flex justify-end"><Button disabled={submitting} onClick={() => {
			if (!value.summary.trim() || value.drafts.some((draft) => !draft.title.trim())) { setError('请填写方案摘要和每个 Task 标题'); return }
			setError(''); void onSubmit(value).catch((cause: unknown) => setError(errorMessage(cause)))
		}}>提交人工审核</Button></div>
	</section>
}

function emptyDraft(): PlanTaskDraftInput { return { title: '', description: '', acceptance_criteria: '', priority: 2, test_cases: [] } }
function editorInput(current: PlanView | null): CreatePlanRevisionInput { return current ? { summary: current.revision.summary, rationale: current.revision.rationale, risks: current.revision.risks, drafts: current.revision.drafts.map((draft) => ({ title: draft.title, description: draft.description, acceptance_criteria: draft.acceptance_criteria, priority: draft.priority, test_cases: draft.test_cases.map((test) => ({ title: test.title, description: test.description, required: test.required })) })) } : { summary: '', rationale: '', risks: '', drafts: [emptyDraft()] } }
function planStatus(current: PlanView): string { return current.plan.status === 'IN_REVIEW' ? '待审核' : current.plan.status === 'CHANGES_REQUESTED' ? '需修改' : '已批准' }
