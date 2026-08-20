import { useCallback, useEffect, useState } from 'react'
import { CheckIcon, ChevronDownIcon, FileDiffIcon, RefreshCwIcon, XIcon } from 'lucide-react'

import {
	errorMessage, getTaskChanges, getTaskFileDiff, getTaskReviews, reviewTask, submitTaskReview,
} from '../api/client'
import type { FileDiff, Task, TaskChanges, TaskReview } from '../types'
import { Button } from './ui/button'
import { Input } from './ui/input'

interface TaskChangesPanelProps {
	task: Task
	hasWorkspace: boolean
	onTaskUpdated: (task: Task) => void
	onWorkspaceChanged: () => void
}

export function TaskChangesPanel({ task, hasWorkspace, onTaskUpdated, onWorkspaceChanged }: TaskChangesPanelProps) {
	const [changes, setChanges] = useState<TaskChanges | null>(null)
	const [reviews, setReviews] = useState<TaskReview[]>([])
	const [selectedDiff, setSelectedDiff] = useState<FileDiff | null>(null)
	const [comment, setComment] = useState('')
	const [loading, setLoading] = useState(hasWorkspace)
	const [pending, setPending] = useState(false)
	const [error, setError] = useState('')

	const loadLatest = useCallback(async (signal?: AbortSignal) => {
		try {
			const loadedReviews = await getTaskReviews(task.id, signal)
			setReviews(loadedReviews)
			setChanges(hasWorkspace ? await getTaskChanges(task.id, signal) : null)
		} catch (loadError: unknown) {
			if (!signal?.aborted) setError(errorMessage(loadError))
		} finally {
			if (!signal?.aborted) setLoading(false)
		}
	}, [hasWorkspace, task.id])

	useEffect(() => {
		const controller = new AbortController()
		const changesRequest = hasWorkspace ? getTaskChanges(task.id, controller.signal) : Promise.resolve(null)
		void Promise.all([getTaskReviews(task.id, controller.signal), changesRequest]).then(
			([loadedReviews, loadedChanges]) => {
				setReviews(loadedReviews)
				setChanges(loadedChanges)
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
		} finally {
			setPending(false)
		}
	}

	async function openDiff(path: string) {
		setError('')
		try {
			setSelectedDiff(await getTaskFileDiff(task.id, path))
		} catch (diffError: unknown) {
			setError(errorMessage(diffError))
		}
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
								<button className="flex w-full items-center gap-3 px-4 py-3 text-left text-sm hover:bg-muted/40" type="button" onClick={() => { void openDiff(file.path) }}>
									<span className="w-16 text-xs text-muted-foreground">{file.status}</span><span className="min-w-0 flex-1 truncate font-mono text-xs">{file.path}</span>
									<span className="font-mono text-xs text-emerald-700">+{file.additions}</span><span className="font-mono text-xs text-rose-700">−{file.deletions}</span><ChevronDownIcon className="size-4" />
								</button>
								{selectedDiff?.path === file.path ? <DiffPreview diff={selectedDiff} onClose={() => setSelectedDiff(null)} /> : null}
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
			{reviews.length > 0 ? <div><h4 className="mb-2 text-xs font-medium text-muted-foreground">验收历史</h4><ul className="space-y-2">{reviews.map((review) => <li className="rounded-lg border px-3 py-2 text-sm" key={review.id}><strong>{review.decision === 'ACCEPTED' ? '已通过' : '要求修改'}</strong>{review.comment ? ` · ${review.comment}` : ''}{review.commit_sha ? <span className="ml-2 font-mono text-xs text-muted-foreground">{review.commit_sha.slice(0, 8)}</span> : null}</li>)}</ul></div> : null}
		</section>
	)
}

function DiffPreview({ diff, onClose }: { diff: FileDiff; onClose: () => void }) {
	return <div className="border-t bg-zinc-950 text-zinc-100"><div className="flex items-center justify-between px-4 py-2 text-xs"><span>{diff.binary ? '二进制文件' : diff.truncated ? 'Diff 已截断' : 'Unified diff'}</span><Button variant="ghost" size="icon-sm" type="button" aria-label="关闭 Diff" onClick={onClose}><XIcon /></Button></div><pre className="max-h-96 overflow-auto border-t border-white/10 p-4 font-mono text-xs leading-5">{diff.binary ? '不展示二进制内容' : diff.patch}</pre></div>
}
