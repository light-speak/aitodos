import { useState } from 'react'
import { ShieldQuestionIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { ApprovalDecision, ApprovalRequest, Task } from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'

export function ApprovalInbox(props: {
	items: ApprovalRequest[]
	tasks: Task[]
	decidingID: string | null
	onOpenTask: (taskID: string) => void
	onDecide: (item: ApprovalRequest, decision: ApprovalDecision) => Promise<void>
}) {
	if (props.items.length === 0) return null
	return (
		<section className="grid gap-3" aria-label="权限请求">
			<div><h3 className="flex items-center gap-2 font-medium"><ShieldQuestionIcon className="size-4" />Agent 权限请求</h3><p className="mt-1 text-xs text-muted-foreground">只批准你看得懂且符合当前任务范围的操作。</p></div>
			{props.items.map((item) => <ApprovalCard key={item.id} item={item} task={props.tasks.find((task) => task.id === item.task_id)} deciding={props.decidingID === item.id} onOpenTask={props.onOpenTask} onDecide={props.onDecide} />)}
		</section>
	)
}

function ApprovalCard(props: {
	item: ApprovalRequest
	task?: Task
	deciding: boolean
	onOpenTask: (taskID: string) => void
	onDecide: (item: ApprovalRequest, decision: ApprovalDecision) => Promise<void>
}) {
	const [error, setError] = useState<unknown>(null)
	const [confirmCancel, setConfirmCancel] = useState(false)
	async function decide(decision: ApprovalDecision) {
		setError(null)
		try {
			await props.onDecide(props.item, decision)
		} catch (decisionError: unknown) {
			setError(decisionError)
		}
	}
	return (
		<article className="rounded-xl border bg-card p-4">
			<div className="flex flex-wrap items-center gap-2"><Badge variant="outline">{kindLabel(props.item.kind)}</Badge>{props.task ? <Button className="h-auto p-0" variant="link" type="button" onClick={() => props.onOpenTask(props.task?.id ?? '')}>{props.task.key} · {props.task.title}</Button> : null}</div>
			<p className="mt-3 text-sm font-medium">{requestSummary(props.item)}</p>
			{props.item.reason ? <p className="mt-1 text-sm text-muted-foreground">{props.item.reason}</p> : null}
			{props.item.command ? <pre className="mt-3 max-h-32 overflow-auto rounded-lg bg-zinc-950 p-3 font-mono text-xs whitespace-pre-wrap text-zinc-100">{props.item.command}</pre> : null}
			{props.item.kind === 'PERMISSIONS' && props.item.grant_root ? <pre className="mt-3 max-h-32 overflow-auto rounded-lg bg-zinc-950 p-3 font-mono text-xs whitespace-pre-wrap text-zinc-100">{props.item.grant_root}</pre> : null}
			{props.item.cwd ? <p className="mt-2 break-all font-mono text-[11px] text-muted-foreground">工作目录：{props.item.cwd}</p> : null}
			{error ? <p className="mt-3 text-sm text-destructive" role="alert">{errorMessage(error)}</p> : null}
			<div className="mt-4 flex flex-wrap gap-2">
				{supports(props.item, 'ACCEPT_ONCE') ? <Button type="button" disabled={props.deciding} onClick={() => { void decide('ACCEPT_ONCE') }}>仅本次允许</Button> : null}
				{supports(props.item, 'ACCEPT_SESSION') ? <Button variant="outline" type="button" disabled={props.deciding} onClick={() => { void decide('ACCEPT_SESSION') }}>本 Session 允许</Button> : null}
				{supports(props.item, 'DECLINE') ? <Button variant="outline" type="button" disabled={props.deciding} onClick={() => { void decide('DECLINE') }}>拒绝</Button> : null}
				{supports(props.item, 'CANCEL_RUN') && !confirmCancel ? <Button variant="ghost" type="button" disabled={props.deciding} onClick={() => setConfirmCancel(true)}>停止 Run</Button> : null}
				{supports(props.item, 'CANCEL_RUN') && confirmCancel ? <><Button variant="destructive" type="button" disabled={props.deciding} onClick={() => { void decide('CANCEL_RUN') }}>确认停止 Run</Button><Button variant="ghost" type="button" disabled={props.deciding} onClick={() => setConfirmCancel(false)}>返回</Button></> : null}
			</div>
		</article>
	)
}

function supports(item: ApprovalRequest, decision: ApprovalDecision): boolean {
	return item.available_decisions.includes(decision)
}

function kindLabel(kind: ApprovalRequest['kind']): string {
	return ({ COMMAND: '命令执行', FILE_CHANGE: '文件修改', NETWORK: '网络访问', PERMISSIONS: '额外权限' })[kind]
}

function requestSummary(item: ApprovalRequest): string {
	if (item.kind === 'NETWORK') return `访问 ${item.protocol ? `${item.protocol}://` : ''}${item.host || '外部网络'}`
	if (item.kind === 'FILE_CHANGE') return item.grant_root ? `修改 ${item.grant_root}` : '修改 Workspace 文件'
	if (item.kind === 'PERMISSIONS') return '临时扩大当前执行权限'
	return '执行以下命令'
}
