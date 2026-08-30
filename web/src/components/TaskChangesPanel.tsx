import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react'
import { CheckIcon, ChevronDownIcon, FileDiffIcon, RefreshCwIcon, XIcon } from 'lucide-react'

import {
	errorMessage, getTaskChanges, getTaskFileDiff, getTaskIntegration, getTaskReviews,
	integrateTask, reviewTask, submitTaskReview, syncTaskTarget,
} from '../api/client'
import type { FileDiff, Task, TaskChanges, TaskIntegration, TaskReview } from '../types'
import { Button } from './ui/button'
import { Input } from './ui/input'

interface TaskChangesPanelProps {
	task: Task
	hasWorkspace: boolean
	onTaskUpdated: (task: Task) => void
	onWorkspaceChanged: () => void
}

const DiffPreview = lazy(async () => {
	const module = await import('./DiffPreview')
	return { default: module.DiffPreview }
})

export function TaskChangesPanel({ task, hasWorkspace, onTaskUpdated, onWorkspaceChanged }: TaskChangesPanelProps) {
	const [changes, setChanges] = useState<TaskChanges | null>(null)
	const [reviews, setReviews] = useState<TaskReview[]>([])
	const [integration, setIntegration] = useState<TaskIntegration | null>(null)
	const [selectedDiff, setSelectedDiff] = useState<FileDiff | null>(null)
	const [loadingDiffPath, setLoadingDiffPath] = useState<string | null>(null)
	const diffRequestID = useRef(0)
	const [comment, setComment] = useState('')
	const [loading, setLoading] = useState(hasWorkspace)
	const [pending, setPending] = useState(false)
	const [error, setError] = useState('')

	const loadLatest = useCallback(async (signal?: AbortSignal) => {
		try {
			const [loadedReviews, loadedChanges, loadedIntegration] = await Promise.all([
				getTaskReviews(task.id, signal),
				hasWorkspace ? getTaskChanges(task.id, signal) : Promise.resolve(null),
				getTaskIntegration(task.id, signal),
			])
			setReviews(loadedReviews)
			setChanges(loadedChanges)
			setIntegration(loadedIntegration)
		} catch (loadError: unknown) {
			if (!signal?.aborted) setError(errorMessage(loadError))
		} finally {
			if (!signal?.aborted) setLoading(false)
		}
	}, [hasWorkspace, task.id])

	useEffect(() => {
		const controller = new AbortController()
		const changesRequest = hasWorkspace ? getTaskChanges(task.id, controller.signal) : Promise.resolve(null)
		void Promise.all([getTaskReviews(task.id, controller.signal), changesRequest, getTaskIntegration(task.id, controller.signal)]).then(
			([loadedReviews, loadedChanges, loadedIntegration]) => {
				setReviews(loadedReviews)
				setChanges(loadedChanges)
				setIntegration(loadedIntegration)
				setLoading(false)
			},
			(loadError: unknown) => {
				if (!controller.signal.aborted) {
					setError(errorMessage(loadError))
					setLoading(false)
				}
			},
		)
		return () => controller.abort()
	}, [hasWorkspace, task.id, task.version])

	async function reload() {
		setError('')
		setLoading(true)
		await loadLatest()
	}

	async function run(action: () => Promise<void>) {
		setPending(true)
		setError('')
		try {
			await action()
			await loadLatest()
		} catch (actionError: unknown) {
			setError(errorMessage(actionError))
			await loadLatest()
		} finally {
			setPending(false)
		}
	}

	async function toggleDiff(path: string) {
		if (selectedDiff?.path === path || loadingDiffPath === path) {
			diffRequestID.current += 1
			setSelectedDiff(null)
			setLoadingDiffPath(null)
			return
		}
		const requestID = diffRequestID.current + 1
		diffRequestID.current = requestID
		setError('')
		setSelectedDiff(null)
		setLoadingDiffPath(path)
		try {
			const loaded = await getTaskFileDiff(task.id, path)
			if (diffRequestID.current === requestID) setSelectedDiff(loaded)
		} catch (diffError: unknown) {
			if (diffRequestID.current === requestID) setError(errorMessage(diffError))
		} finally {
			if (diffRequestID.current === requestID) setLoadingDiffPath(null)
		}
	}

	function closeDiff() {
		diffRequestID.current += 1
		setSelectedDiff(null)
		setLoadingDiffPath(null)
	}

	return (
		<section className="space-y-4 py-5" aria-label="Task 变更与验收">
			<div className="flex items-center justify-between gap-3">
				<div>
					<h3 className="text-sm font-semibold">Changes & Review</h3>
					<p className="text-xs text-muted-foreground">先查看修改再验收；通过时系统会自动记录必要 Commit，但不会 push 或合并。</p>
				</div>
				<Button variant="ghost" size="sm" type="button" disabled={loading} onClick={() => { void reload() }}><RefreshCwIcon />刷新</Button>
			</div>
			{error ? <p className="rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive" role="alert">{error}</p> : null}
			{!hasWorkspace ? <p className="rounded-lg border border-dashed p-5 text-center text-sm text-muted-foreground">创建 Workspace 后可查看真实 Diff。</p> : null}
			{loading ? <p className="py-5 text-center text-sm text-muted-foreground">正在读取修改…</p> : null}
			{changes ? (
				<div className="rounded-xl border">
					<div className="flex items-center gap-3 border-b bg-muted/20 px-4 py-3 text-sm">
						<FileDiffIcon className="size-4" /><strong>{changes.file_count} 个文件</strong>
						<span className="font-mono text-emerald-700">+{changes.additions}</span>
						<span className="font-mono text-rose-700">−{changes.deletions}</span>
					</div>
					{changes.files.length === 0 ? <p className="p-5 text-center text-sm text-muted-foreground">没有相对 Base Commit 的修改</p> : (
						<ul>{changes.files.map((file) => (
							<li className="border-b last:border-b-0" key={file.path}>
								<button className="flex w-full items-center gap-3 px-4 py-3 text-left text-sm hover:bg-muted/40" type="button" aria-expanded={selectedDiff?.path === file.path || loadingDiffPath === file.path} onClick={() => { void toggleDiff(file.path) }}>
									<span className="w-16 text-xs text-muted-foreground">{file.status}</span><span className="min-w-0 flex-1 truncate font-mono text-xs">{file.path}</span>
									<span className="font-mono text-xs text-emerald-700">+{file.additions}</span><span className="font-mono text-xs text-rose-700">−{file.deletions}</span><ChevronDownIcon className={`size-4 transition-transform ${selectedDiff?.path === file.path ? 'rotate-180' : ''}`} />
								</button>
								{loadingDiffPath === file.path ? <p className="border-t bg-zinc-950 px-4 py-3 text-xs text-zinc-400">正在读取 Diff…</p> : null}
								{selectedDiff?.path === file.path ? <Suspense fallback={<p className="border-t bg-zinc-950 px-4 py-3 text-xs text-zinc-400">正在准备高亮…</p>}><DiffPreview diff={selectedDiff} onClose={closeDiff} /></Suspense> : null}
							</li>
						))}</ul>
					)}
				</div>
			) : null}
			{task.status === 'READY' ? <Button type="button" disabled={pending} onClick={() => { void run(async () => onTaskUpdated(await submitTaskReview(task))) }}>提交人工验收</Button> : null}
			{task.status === 'REVIEW' ? (
				<div className="space-y-3 rounded-xl border bg-muted/20 p-4">
					<Input aria-label="Review comment" placeholder="验收说明；要求修改时必填" value={comment} onChange={(event) => setComment(event.target.value)} />
					<div className="flex gap-2"><Button type="button" disabled={pending} onClick={() => { void run(async () => { onTaskUpdated((await reviewTask(task, 'ACCEPTED', comment)).task); onWorkspaceChanged() }) }}><CheckIcon />验收通过</Button><Button variant="outline" type="button" disabled={pending || !comment.trim()} onClick={() => { void run(async () => onTaskUpdated((await reviewTask(task, 'REJECTED', comment)).task)) }}><XIcon />要求修改</Button></div>
				</div>
			) : null}
			<TaskIntegrationPanel
				task={task}
				integration={integration}
				pending={pending}
				onIntegrate={() => run(async () => setIntegration(await integrateTask(task.id)))}
				onSync={() => run(async () => {
					const result = await syncTaskTarget(task.id)
					setIntegration(result.integration)
					onTaskUpdated(result.task)
					onWorkspaceChanged()
				})}
			/>
			{reviews.length > 0 ? <div><h4 className="mb-2 text-xs font-medium text-muted-foreground">验收历史</h4><ul className="space-y-2">{reviews.map((review) => <li className="rounded-lg border px-3 py-2 text-sm" key={review.id}><strong>{review.decision === 'ACCEPTED' ? '已通过' : '要求修改'}</strong>{review.comment ? ` · ${review.comment}` : ''}{review.commit_sha ? <span className="ml-2 font-mono text-xs text-muted-foreground">{review.commit_sha.slice(0, 8)}</span> : null}</li>)}</ul></div> : null}
		</section>
	)
}

function TaskIntegrationPanel(props: {
	task: Task
	integration: TaskIntegration | null
	pending: boolean
	onIntegrate: () => Promise<void>
	onSync: () => Promise<void>
}) {
	if (props.integration?.status === 'SUCCEEDED') {
		return <p className="rounded-xl border border-emerald-200 bg-emerald-50 p-3 text-sm text-emerald-700">已集成到 {props.integration.target_branch} · {props.integration.target_after_sha?.slice(0, 8)}</p>
	}
	if (props.integration?.status === 'NEEDS_SYNC') {
		return <div className="flex items-center justify-between gap-3 rounded-xl border border-amber-200 bg-amber-50 p-3"><p className="text-sm text-amber-800">目标分支已经前进，需要同步并重新验证。</p><Button type="button" variant="outline" disabled={props.pending} onClick={() => { void props.onSync() }}>同步并重新验证</Button></div>
	}
	if (props.integration?.status === 'CONFLICT') {
		return <p className="rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800">目标分支存在冲突，已交给 Revision Agent 在 Task Workspace 内处理。</p>
	}
	if (props.integration?.status === 'SYNCED') {
		return <p className="rounded-xl border border-blue-200 bg-blue-50 p-3 text-sm text-blue-800">目标分支已同步，等待重新执行测试并验收。</p>
	}
	if (props.task.status !== 'ACCEPTED') return null
	return <div className="flex items-center justify-between gap-3 rounded-xl border p-3"><div><p className="text-sm font-medium">目标分支交付</p><p className="text-xs text-muted-foreground">只执行本地 fast-forward，不会 push。</p></div><Button type="button" disabled={props.pending} onClick={() => { void props.onIntegrate() }}>集成到 {props.task.target_branch || '目标分支'}</Button></div>
}
