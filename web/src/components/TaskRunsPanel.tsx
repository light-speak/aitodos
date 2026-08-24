import { useState } from 'react'
import { ActivityIcon, RefreshCwIcon, TerminalIcon } from 'lucide-react'

import { errorMessage, retryTask } from '../api/client'
import { useTaskRuns } from '../features/runs/useTaskRuns'
import type { AgentRun, RunArtifact, RunLog, RunStatus, RunUsage, Task } from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from './ui/dialog'
import { Input } from './ui/input'

const statusLabels: Record<RunStatus, string> = {
	CLAIMED: '已领取', STARTING: '启动中', RUNNING: '执行中', FINALIZING: '收尾中', NEEDS_INPUT: '等待回答',
	SUCCEEDED: '成功', FAILED: '失败', CANCELLED: '已取消', TIMED_OUT: '超时', LOST: '已丢失',
}

export function TaskRunsPanel({ task, onTaskUpdated }: { task: Task; onTaskUpdated: (task: Task) => void }) {
	const state = useTaskRuns(task.id)
	const [confirmRetry, setConfirmRetry] = useState(false)
	const [retrying, setRetrying] = useState(false)
	const [retryError, setRetryError] = useState<unknown>(null)
	const canRetry = task.status === 'BLOCKED' && state.runs[0]?.status !== 'NEEDS_INPUT'
	async function runRetry() {
		setRetrying(true)
		setRetryError(null)
		try {
			onTaskUpdated(await retryTask(task.id, task.version))
			setConfirmRetry(false)
		} catch (error: unknown) {
			setRetryError(error)
		} finally {
			setRetrying(false)
		}
	}
	return (
		<section className="space-y-3 py-5" aria-label="Agent Runs">
			<div className="flex items-center justify-between gap-3">
				<div>
					<h3 className="flex items-center gap-2 text-sm font-semibold"><ActivityIcon className="size-4" />Agent Runs</h3>
					<p className="text-xs text-muted-foreground">执行历史只加载摘要；完整日志仅在你点击后读取。</p>
				</div>
				<Button variant="ghost" size="sm" type="button" disabled={state.loading} onClick={state.reload}><RefreshCwIcon />刷新</Button>
			</div>
			{canRetry && !confirmRetry ? <Button variant="outline" type="button" onClick={() => setConfirmRetry(true)}>重新排队</Button> : null}
			{canRetry && confirmRetry ? <div className="rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm"><p>重新排队会创建新的 Agent Run；Workers 开启时可能立即执行。</p><div className="mt-3 flex gap-2"><Button type="button" disabled={retrying} onClick={() => { void runRetry() }}>{retrying ? '提交中…' : '确认重新排队'}</Button><Button variant="ghost" type="button" disabled={retrying} onClick={() => setConfirmRetry(false)}>暂不执行</Button></div></div> : null}
			{retryError ? <p className="text-sm text-destructive" role="alert">{errorMessage(retryError)}</p> : null}
			{state.error ? <p className="rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive" role="alert">{errorMessage(state.error)}</p> : null}
			{state.streamRetrying ? <p className="text-xs text-amber-700">实时连接中断，浏览器正在自动重连…</p> : null}
			{state.loading ? <p className="py-4 text-center text-sm text-muted-foreground">正在读取 Run 历史…</p> : null}
			{!state.loading && !state.error && state.runs.length === 0 ? <p className="rounded-lg border border-dashed p-4 text-center text-sm text-muted-foreground">还没有 Agent Run</p> : null}
			{state.runs.length > 0 ? (
				<ul className="overflow-hidden rounded-xl border">
					{state.runs.map((run) => <RunRow key={run.id} run={run} onOpen={() => state.selectRun(run.id)} />)}
				</ul>
			) : null}
			{state.selectedRunID ? (
				<RunDetailDialog
					state={state}
					onClose={() => state.selectRun(null)}
				/>
			) : null}
		</section>
	)
}

function RunRow({ run, onOpen }: { run: AgentRun; onOpen: () => void }) {
	return (
		<li className="border-b last:border-b-0">
			<button type="button" aria-label={`查看 Run ${run.id}`} className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-muted/40" onClick={onOpen}>
				<Badge variant={run.status === 'SUCCEEDED' ? 'secondary' : run.status === 'FAILED' ? 'destructive' : 'outline'}>{statusLabels[run.status]}</Badge>
				<span className="min-w-0 flex-1">
					<span className="block text-sm font-medium">{run.purpose}</span>
					<span className="block truncate font-mono text-[11px] text-muted-foreground">{run.id}</span>
				</span>
				<span className="text-xs text-muted-foreground">{formatDuration(run)}</span>
			</button>
		</li>
	)
}

type RunPanelState = ReturnType<typeof useTaskRuns>

function RunDetailDialog({ state, onClose }: { state: RunPanelState; onClose: () => void }) {
	return (
		<Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
			<DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-3xl">
				<DialogHeader>
					<DialogTitle>Run 详情</DialogTitle>
					<DialogDescription className="break-all font-mono">{state.selectedRunID}</DialogDescription>
				</DialogHeader>
				{state.detailLoading ? <p className="py-8 text-center text-muted-foreground">正在读取 Run 详情…</p> : null}
				{state.detailError ? <p className="rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-destructive" role="alert">{errorMessage(state.detailError)}</p> : null}
				{state.detail ? <RunDetailContent state={state} /> : null}
			</DialogContent>
		</Dialog>
	)
}

function RunDetailContent({ state }: { state: RunPanelState }) {
	const [confirmCancel, setConfirmCancel] = useState(false)
	const [cancelReason, setCancelReason] = useState('')
	const [cancelError, setCancelError] = useState<unknown>(null)
	const detail = state.detail
	if (detail === null) return null
	const run = detail.run
	const cancellable = ['CLAIMED', 'STARTING', 'RUNNING'].includes(run.status) && !run.cancel_requested_at
	async function cancelCurrentRun() {
		setCancelError(null)
		try {
			await state.cancelRun(run.id, cancelReason)
			setConfirmCancel(false)
		} catch (error: unknown) {
			setCancelError(error)
		}
	}
	return (
		<div className="grid gap-4">
			{cancellable && !confirmCancel ? <Button variant="outline" type="button" className="justify-self-start" onClick={() => setConfirmCancel(true)}>取消当前 Run</Button> : null}
			{cancellable && confirmCancel ? <div className="rounded-xl border border-amber-200 bg-amber-50 p-4"><p className="text-sm">系统会停止当前 Agent、完成 Workspace 收尾，并把 Task 置为已阻塞；不会删除修改。</p><Input className="mt-3" aria-label="取消原因" placeholder="可选：为什么停止这次执行" value={cancelReason} onChange={(event) => setCancelReason(event.currentTarget.value)} /><div className="mt-3 flex gap-2"><Button type="button" disabled={state.cancellingRunID === run.id} onClick={() => { void cancelCurrentRun() }}>{state.cancellingRunID === run.id ? '正在请求…' : '确认取消 Run'}</Button><Button variant="ghost" type="button" disabled={state.cancellingRunID === run.id} onClick={() => setConfirmCancel(false)}>继续执行</Button></div>{cancelError ? <p className="mt-2 text-sm text-destructive" role="alert">{errorMessage(cancelError)}</p> : null}</div> : null}
			{run.cancel_requested_at ? <p className="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm">已请求取消，等待 Runner 停止 Agent 并完成收尾。{run.cancel_reason ? ` 原因：${run.cancel_reason}` : ''}</p> : null}
			<div className="grid gap-3 rounded-xl border p-4 sm:grid-cols-2">
				<Detail label="状态" value={statusLabels[run.status]} />
				<Detail label="Purpose" value={run.purpose} mono />
				<Detail label="Exit Code" value={run.exit_code === undefined ? '未知' : String(run.exit_code)} mono />
				<Detail label="耗时" value={formatDuration(run)} />
				<Detail label="Agent Session" value={sessionLabel(run)} />
				{run.failure_code ? <Detail label="失败代码" value={run.failure_code} mono /> : null}
				{run.failure_kind ? <Detail label="失败类型" value={run.failure_kind} mono /> : null}
				{run.failure_message ? <div className="sm:col-span-2"><Detail label="失败信息" value={run.failure_message} /></div> : null}
			</div>
			{detail.workspace_snapshot ? (
				<div className="rounded-xl border p-4">
					<h4 className="mb-3 text-sm font-semibold">Workspace Finalization</h4>
					<div className="grid gap-3 sm:grid-cols-2">
						<Detail label="HEAD" value={`${shortSHA(detail.workspace_snapshot.head_before)} → ${shortSHA(detail.workspace_snapshot.head_after)}`} mono />
						<Detail label="最终状态" value={`${detail.workspace_snapshot.state_after}${detail.workspace_snapshot.dirty_after ? ' · 有修改' : ' · 干净'}`} />
						<Detail label="分支" value={detail.workspace_snapshot.branch_name} mono />
						<Detail label="目标分支" value={detail.workspace_snapshot.target_branch} mono />
					</div>
				</div>
			) : null}
			{detail.usage ? <UsageSummary usage={detail.usage} /> : null}
			<RunLogs artifacts={detail.artifacts} state={state} />
		</div>
	)
}

function UsageSummary({ usage }: { usage: RunUsage }) {
	return (
		<div className="rounded-xl border p-4">
			<h4 className="mb-3 text-sm font-semibold">实际用量</h4>
			<div className="grid gap-3 sm:grid-cols-3">
				<Detail label="输入" value={formatCount(usage.input_tokens)} />
				<Detail label="缓存输入" value={formatCount(usage.cached_input_tokens)} />
				<Detail label="输出" value={formatCount(usage.output_tokens)} />
			</div>
		</div>
	)
}

function RunLogs({ artifacts, state }: { artifacts: RunArtifact[]; state: RunPanelState }) {
	const streams = (['stdout', 'stderr'] as const).filter((stream) => artifacts.some((artifact) => artifact.kind === stream.toUpperCase()))
	if (streams.length === 0) return <p className="rounded-lg border border-dashed p-4 text-center text-sm text-muted-foreground">没有可读取的 stdout/stderr Artifact</p>
	return (
		<div className="rounded-xl border p-4">
			<h4 className="mb-3 flex items-center gap-2 text-sm font-semibold"><TerminalIcon className="size-4" />日志</h4>
			<div className="flex flex-wrap gap-2">
				{streams.map((stream) => <Button key={stream} variant="outline" size="sm" type="button" disabled={state.loadingLog === stream} onClick={() => { void state.loadLog(stream) }}>读取 {stream}</Button>)}
			</div>
			{state.logError ? <p className="mt-3 text-sm text-destructive" role="alert">{errorMessage(state.logError)}</p> : null}
			{streams.map((stream) => state.logs[stream] ? <LogPreview key={stream} log={state.logs[stream]} /> : null)}
		</div>
	)
}

function LogPreview({ log }: { log: RunLog }) {
	return <div className="mt-3 overflow-hidden rounded-lg bg-zinc-950 text-zinc-100"><div className="border-b border-white/10 px-3 py-2 text-xs">{log.stream}{log.truncated ? ' · 已截断' : ''}</div><pre className="max-h-96 overflow-auto p-3 font-mono text-xs leading-5 whitespace-pre-wrap">{log.content}</pre></div>
}

function Detail({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
	return <dl><dt className="mb-1 text-xs text-muted-foreground">{label}</dt><dd className={`m-0 break-all text-sm ${mono ? 'font-mono text-xs' : ''}`}>{value}</dd></dl>
}

function shortSHA(value: string): string {
	return value.slice(0, 8)
}

function formatCount(value: number | undefined): string {
	return value === undefined ? '未知' : new Intl.NumberFormat('zh-CN').format(value)
}

function formatDuration(run: AgentRun): string {
	if (!run.started_at || !run.finished_at) return '未知'
	const milliseconds = new Date(run.finished_at).getTime() - new Date(run.started_at).getTime()
	if (!Number.isFinite(milliseconds) || milliseconds < 0) return '未知'
	return milliseconds < 1000 ? `${milliseconds} ms` : `${(milliseconds / 1000).toFixed(milliseconds < 10000 ? 1 : 0)} s`
}

function sessionLabel(run: AgentRun): string {
	if (!run.agent_session_id) return '未提供'
	return run.session_resumed ? '已恢复既有会话' : '本次新建会话'
}
