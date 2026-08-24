import { useState } from 'react'
import { RefreshCwIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { AgentRun, RunPurpose, RunStatus, Task } from '../types'
import { useRunQuery } from '../features/runs/useRunQuery'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Input } from './ui/input'

const statuses: Array<RunStatus | ''> = ['', 'CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING', 'NEEDS_INPUT', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'TIMED_OUT', 'LOST']
const purposes: Array<RunPurpose | ''> = ['', 'PLANNING', 'TRIAGE', 'IMPLEMENTATION', 'REVISION', 'REVIEW']

export function RunsPage({ enabled, tasks, onOpenTask }: { enabled: boolean; tasks: Task[]; onOpenTask: (taskID: string) => void }) {
	const query = useRunQuery(enabled)
	return (
		<section className="space-y-4 px-4 sm:px-6 lg:px-8" aria-label="Run 查询">
			<div className="flex flex-wrap items-end gap-3 rounded-xl border bg-card p-4">
				<label className="grid gap-1.5 text-xs font-medium text-muted-foreground">状态<select className="h-9 min-w-40 rounded-lg border bg-background px-3 text-sm text-foreground" value={query.status} onChange={(event) => query.setStatus(event.currentTarget.value as RunStatus | '')}>{statuses.map((status) => <option key={status || 'all'} value={status}>{status || '全部状态'}</option>)}</select></label>
				<label className="grid gap-1.5 text-xs font-medium text-muted-foreground">职责<select className="h-9 min-w-40 rounded-lg border bg-background px-3 text-sm text-foreground" value={query.purpose} onChange={(event) => query.setPurpose(event.currentTarget.value as RunPurpose | '')}>{purposes.map((purpose) => <option key={purpose || 'all'} value={purpose}>{purpose || '全部职责'}</option>)}</select></label>
				<Button variant="outline" type="button" disabled={query.loading} onClick={query.reload}><RefreshCwIcon />刷新</Button>
			</div>
			{query.error ? <p className="rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive" role="alert">{errorMessage(query.error)}</p> : null}
			{query.loading ? <p className="py-12 text-center text-sm text-muted-foreground">正在读取 Run…</p> : null}
			{!query.loading && query.items.length === 0 ? <p className="rounded-xl border border-dashed py-12 text-center text-sm text-muted-foreground">没有符合条件的 Run</p> : null}
			<div className="grid gap-3">{query.items.map((run) => <RunQueryCard key={run.id} run={run} task={tasks.find((item) => item.id === run.task_id)} cancelling={query.cancellingRunID === run.id} onCancel={query.cancelRun} onOpenTask={onOpenTask} />)}</div>
			{query.hasMore ? <div className="flex justify-center"><Button variant="outline" type="button" disabled={query.loadingMore} onClick={() => { void query.loadMore() }}>{query.loadingMore ? '读取中…' : '加载更多'}</Button></div> : null}
		</section>
	)
}

function RunQueryCard(props: { run: AgentRun; task?: Task; cancelling: boolean; onCancel: (runID: string, reason: string) => Promise<void>; onOpenTask: (taskID: string) => void }) {
	const [confirming, setConfirming] = useState(false)
	const [reason, setReason] = useState('')
	const [error, setError] = useState<unknown>(null)
	const cancellable = ['CLAIMED', 'STARTING', 'RUNNING'].includes(props.run.status) && !props.run.cancel_requested_at
	async function cancel() {
		setError(null)
		try {
			await props.onCancel(props.run.id, reason)
			setConfirming(false)
		} catch (cancelError: unknown) {
			setError(cancelError)
		}
	}
	return (
		<article className="rounded-xl border bg-card p-4">
			<div className="flex flex-wrap items-start gap-3">
				<Badge variant={props.run.status === 'FAILED' ? 'destructive' : props.run.status === 'SUCCEEDED' ? 'secondary' : 'outline'}>{props.run.status}</Badge>
				<div className="min-w-0 flex-1"><p className="font-medium">{props.run.purpose}{props.run.session_resumed ? ' · Session 已恢复' : ''}</p><p className="break-all font-mono text-xs text-muted-foreground">{props.run.id}</p>{props.task ? <Button className="mt-1 h-auto p-0" variant="link" type="button" onClick={() => props.onOpenTask(props.task?.id ?? '')}>{props.task.key} · {props.task.title}</Button> : null}</div>
				<div className="text-right text-xs text-muted-foreground"><p>{formatDate(props.run.created_at)}</p>{props.run.cancel_requested_at ? <p className="mt-1 text-amber-700">等待取消</p> : null}</div>
				{cancellable && !confirming ? <Button variant="outline" size="sm" type="button" onClick={() => setConfirming(true)}>取消 Run</Button> : null}
			</div>
			{props.run.failure_message ? <p className="mt-3 rounded-lg bg-destructive/5 p-3 text-sm text-destructive">{props.run.failure_code ? `${props.run.failure_code} · ` : ''}{props.run.failure_message}</p> : null}
			{cancellable && confirming ? <div className="mt-3 rounded-lg border border-amber-200 bg-amber-50 p-3"><p className="text-sm">停止 Agent 后 Task 会进入已阻塞，Workspace 修改会保留。</p><Input className="mt-2" aria-label={`取消原因 ${props.run.id}`} value={reason} onChange={(event) => setReason(event.currentTarget.value)} placeholder="可选取消原因" /><div className="mt-2 flex gap-2"><Button size="sm" type="button" disabled={props.cancelling} onClick={() => { void cancel() }}>{props.cancelling ? '请求中…' : '确认取消'}</Button><Button size="sm" variant="ghost" type="button" disabled={props.cancelling} onClick={() => setConfirming(false)}>继续执行</Button></div>{error ? <p className="mt-2 text-sm text-destructive" role="alert">{errorMessage(error)}</p> : null}</div> : null}
		</article>
	)
}

function formatDate(value: string): string {
	const date = new Date(value)
	return Number.isNaN(date.getTime()) ? '时间未知' : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'short', timeStyle: 'short' }).format(date)
}
