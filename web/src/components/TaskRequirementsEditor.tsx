import { useState } from 'react'
import { ArchiveIcon, BanIcon, PencilIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { Task, UpdateTaskDetailsInput } from '../types'
import { MarkdownTextarea } from './MarkdownTextarea'
import { Button } from './ui/button'

interface TaskRequirementsEditorProps {
	task: Task
	onUpdate: (input: UpdateTaskDetailsInput) => Promise<void>
	onCancel: () => Promise<void>
	onArchive: () => Promise<void>
}

export function TaskRequirementsEditor({ task, onUpdate, onCancel, onArchive }: TaskRequirementsEditorProps) {
	const [editing, setEditing] = useState(false)
	const [input, setInput] = useState<UpdateTaskDetailsInput>(() => taskInput(task))
	const [confirmAction, setConfirmAction] = useState<'cancel' | 'archive' | null>(null)
	const [busy, setBusy] = useState(false)
	const [error, setError] = useState('')

	async function perform(action: () => Promise<void>) {
		setBusy(true)
		setError('')
		try {
			await action()
			setEditing(false)
			setConfirmAction(null)
		} catch (cause: unknown) {
			setError(errorMessage(cause))
		} finally {
			setBusy(false)
		}
	}

	if (editing) {
		return <section className="grid gap-3 rounded-xl border bg-background p-4" aria-label="编辑 Task 需求">
			<MarkdownTextarea aria-label="Task 描述" value={input.description} onChange={(description) => setInput({ ...input, description })} size="compact" />
			<MarkdownTextarea aria-label="Task 验收标准" value={input.acceptance_criteria} onChange={(acceptance_criteria) => setInput({ ...input, acceptance_criteria })} size="compact" />
			<div className="flex flex-wrap items-center gap-2">
				<label className="flex items-center gap-2 text-sm">优先级
					<select className="h-9 rounded-lg border bg-background px-3" value={input.priority} onChange={(event) => setInput({ ...input, priority: Number(event.currentTarget.value) })}>
						{[0, 1, 2, 3].map((priority) => <option value={priority} key={priority}>P{priority}</option>)}
					</select>
				</label>
				<div className="ml-auto flex gap-2"><Button variant="ghost" disabled={busy} onClick={() => setEditing(false)}>取消编辑</Button><Button disabled={busy} onClick={() => { void perform(() => onUpdate(input)) }}>{busy ? '保存中…' : '保存修改'}</Button></div>
			</div>
			{error ? <p className="text-sm text-destructive" role="alert">{error}</p> : null}
		</section>
	}

	const terminal = task.status === 'ACCEPTED' || task.status === 'CANCELLED'
	return <div className="flex flex-wrap items-center gap-2">
		{task.status !== 'RUNNING' && task.status !== 'CANCELLED' ? <Button variant="outline" size="sm" onClick={() => { setError(''); setInput(taskInput(task)); setEditing(true) }}><PencilIcon />编辑需求</Button> : null}
		{!terminal && task.status !== 'RUNNING' ? <Button variant="ghost" size="sm" onClick={() => setConfirmAction('cancel')}><BanIcon />取消 Task</Button> : null}
		{terminal ? <Button variant="ghost" size="sm" onClick={() => setConfirmAction('archive')}><ArchiveIcon />归档</Button> : null}
		{confirmAction ? <div className="flex items-center gap-2 rounded-lg border bg-muted/40 px-3 py-1.5 text-sm">
			<span>{confirmAction === 'cancel' ? '停止后续执行并保留历史？' : '从默认看板隐藏，历史仍可搜索。'}</span>
			<Button size="sm" variant={confirmAction === 'cancel' ? 'destructive' : 'default'} disabled={busy} onClick={() => { void perform(confirmAction === 'cancel' ? onCancel : onArchive) }}>确认</Button>
			<Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirmAction(null)}>返回</Button>
		</div> : null}
		{error ? <p className="w-full text-sm text-destructive" role="alert">{error}</p> : null}
	</div>
}

function taskInput(task: Task): UpdateTaskDetailsInput {
	return { description: task.description, acceptance_criteria: task.acceptance_criteria, priority: task.priority }
}
