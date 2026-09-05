import { useState } from 'react'
import { TargetIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { CreateObjectiveInput, Topic } from '../types'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from './ui/dialog'
import { Label } from './ui/label'
import { Textarea } from './ui/textarea'

interface CreateObjectiveDialogProps {
	topic: Topic
	open: boolean
	onClose: () => void
	onCreate: (input: CreateObjectiveInput) => Promise<void>
}

export function CreateObjectiveDialog({ topic, open, onClose, onCreate }: CreateObjectiveDialogProps) {
	const [statement, setStatement] = useState(topic.description || topic.title)
	const [criteria, setCriteria] = useState('')
	const [scope, setScope] = useState('')
	const [constraints, setConstraints] = useState('')
	const [error, setError] = useState<unknown>(null)
	const [submitting, setSubmitting] = useState(false)

	function close() {
		setError(null)
		onClose()
	}

	async function submit() {
		const completionCriteria = splitLines(criteria)
		if (!statement.trim() || completionCriteria.length === 0) {
			setError(new Error('请填写目标说明和至少一个可验证完成条件'))
			return
		}
		setError(null)
		setSubmitting(true)
		try {
			await onCreate({
				root_topic_id: topic.id, statement: statement.trim(), scope: scope.trim(),
				constraints: splitLines(constraints), completion_criteria: completionCriteria,
			})
			close()
		} catch (cause: unknown) {
			setError(cause)
		} finally {
			setSubmitting(false)
		}
	}

	return <Dialog open={open} onOpenChange={(next) => { if (!next && !submitting) close() }}>
		<DialogContent className="sm:max-w-2xl" onKeyDown={(event) => {
			if ((event.metaKey || event.ctrlKey) && event.key === 'Enter' && !submitting) {
				event.preventDefault()
				void submit()
			}
		}}>
			<DialogHeader><DialogTitle className="flex items-center gap-2"><TargetIcon className="size-5" />设为长期目标</DialogTitle><DialogDescription>以 {topic.key} 作为根议题；讨论、Plan 和 Task 仍在原位置管理。</DialogDescription></DialogHeader>
			<div className="grid gap-4 py-2">
				<div className="grid gap-2"><Label htmlFor="objective-statement">目标说明</Label><Textarea id="objective-statement" value={statement} onChange={(event) => setStatement(event.currentTarget.value)} className="min-h-24 rounded-xl" /></div>
				<div className="grid gap-2"><Label htmlFor="objective-criteria">可验证完成条件</Label><Textarea id="objective-criteria" value={criteria} onChange={(event) => setCriteria(event.currentTarget.value)} placeholder={'每行一项，例如：\n全部必需测试通过\n验收提交已集成到目标分支'} className="min-h-28 rounded-xl" /><p className="text-xs text-muted-foreground">Agent 可以报告证据，但只有满足条件且关联 Task 全部验收后才能人工完成。</p></div>
				<details className="rounded-xl border p-3"><summary className="cursor-pointer text-sm font-medium">范围与约束</summary><div className="mt-3 grid gap-3"><Textarea aria-label="目标范围" value={scope} onChange={(event) => setScope(event.currentTarget.value)} placeholder="目标包含和不包含什么" className="min-h-20" /><Textarea aria-label="目标约束" value={constraints} onChange={(event) => setConstraints(event.currentTarget.value)} placeholder="每行一项约束" className="min-h-20" /></div></details>
				{error ? <p className="text-sm text-destructive" role="alert">{errorMessage(error)}</p> : null}
			</div>
			<DialogFooter><Button variant="outline" type="button" disabled={submitting} onClick={close}>取消</Button><Button type="button" disabled={submitting} onClick={() => { void submit() }}>{submitting ? '创建中…' : '开始长期目标'}</Button></DialogFooter>
		</DialogContent>
	</Dialog>
}

function splitLines(value: string): string[] {
	return [...new Set(value.split('\n').map((item) => item.trim()).filter(Boolean))]
}
