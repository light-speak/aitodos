import { useState } from 'react'
import type { FormEvent } from 'react'

import { errorMessage } from '../api/client'
import type { ProjectInfo } from '../types'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from './ui/dialog'
import { Input } from './ui/input'
import { Label } from './ui/label'

interface WorkerSettingsDialogProps {
	project: ProjectInfo
	onClose: () => void
	onSave: (enabled: boolean, maxWorkers: number) => Promise<void>
}

export function WorkerSettingsDialog({ project, onClose, onSave }: WorkerSettingsDialogProps) {
	const [maxWorkers, setMaxWorkers] = useState(String(project.max_workers))
	const [submitting, setSubmitting] = useState(false)
	const [error, setError] = useState('')

	async function submit(event: FormEvent<HTMLFormElement>) {
		event.preventDefault()
		const parsed = Number(maxWorkers)
		if (!Number.isInteger(parsed) || parsed < 1 || parsed > 32) {
			setError('最大并发数必须为 1 到 32 的整数')
			return
		}
		setSubmitting(true)
		setError('')
		try {
			await onSave(project.workers_enabled, parsed)
			onClose()
		} catch (saveError: unknown) {
			setError(errorMessage(saveError))
		} finally {
			setSubmitting(false)
		}
	}

	return (
		<Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>Worker 设置</DialogTitle>
					<DialogDescription>只限制当前项目的新 Run；降低并发数不会终止正在执行的 Run。</DialogDescription>
				</DialogHeader>
				<form className="grid gap-4" onSubmit={(event) => { void submit(event) }}>
					<div className="grid gap-2">
						<Label htmlFor="worker-max">最大并发数</Label>
						<Input
							id="worker-max"
							type="number"
							min={1}
							max={32}
							step={1}
							value={maxWorkers}
							onChange={(event) => setMaxWorkers(event.currentTarget.value)}
						/>
						<p className="text-xs text-muted-foreground">默认 2；同一 Task 同时仍只允许一个 Run。</p>
					</div>
					{error ? <p className="text-sm text-destructive" role="alert">{error}</p> : null}
					<DialogFooter>
						<Button variant="outline" type="button" onClick={onClose}>取消</Button>
						<Button type="submit" disabled={submitting}>{submitting ? '保存中…' : '保存设置'}</Button>
					</DialogFooter>
				</form>
			</DialogContent>
		</Dialog>
	)
}
