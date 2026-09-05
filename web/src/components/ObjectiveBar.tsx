import { useState } from 'react'
import { CirclePauseIcon, CirclePlayIcon, FlagIcon, TargetIcon, XIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { ObjectiveCommand, ObjectiveView } from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'

interface ObjectiveBarProps {
	value: ObjectiveView
	onOpenTopic: (topicID: string) => void
	onCommand: (command: ObjectiveCommand) => Promise<unknown>
}

export function ObjectiveBar({ value, onOpenTopic, onCommand }: ObjectiveBarProps) {
	const [pending, setPending] = useState<ObjectiveCommand | null>(null)
	const [error, setError] = useState<unknown>(null)
	const progress = objectiveProgress(value)
	const paused = value.objective.status === 'PAUSED'

	async function execute(command: ObjectiveCommand) {
		setPending(command)
		setError(null)
		try {
			await onCommand(command)
		} catch (cause: unknown) {
			setError(cause)
		} finally {
			setPending(null)
		}
	}

	return <section className="border-b bg-muted/20 px-4 py-3 sm:px-6 lg:px-8" aria-label="长期目标">
		<div className="flex items-center gap-4">
			<div className="flex size-9 shrink-0 items-center justify-center rounded-xl border bg-background text-primary"><TargetIcon className="size-4" /></div>
			<button type="button" className="min-w-0 flex-1 text-left" onClick={() => onOpenTopic(value.objective.root_topic_id)}>
				<div className="flex items-center gap-2"><span className="font-mono text-xs text-muted-foreground">{value.objective.key}</span><Badge variant={paused ? 'outline' : 'secondary'}>{paused ? '已暂停' : '持续推进'}</Badge></div>
				<p className="truncate text-sm font-medium">{value.revision.statement}</p>
			</button>
			<div className="hidden w-64 shrink-0 sm:block">
				<div className="mb-1 flex justify-between text-xs text-muted-foreground"><span>证据进度</span><span>{progress.percent}%</span></div>
				<div className="h-2 overflow-hidden rounded-full bg-muted"><div className={`h-full rounded-full ${progress.tone}`} style={{ width: `${progress.percent}%` }} /></div>
				<p className="mt-1 truncate text-[11px] text-muted-foreground">条件 {value.progress.criteria_satisfied}/{value.progress.criteria_total} · Task {value.progress.tasks_accepted}/{value.progress.tasks_total}</p>
			</div>
			<Button variant="outline" size="sm" type="button" disabled={pending !== null} onClick={() => { void execute(paused ? 'resume' : 'pause') }}>
				{paused ? <CirclePlayIcon /> : <CirclePauseIcon />}{paused ? '恢复' : '暂停'}
			</Button>
			<Button size="sm" type="button" disabled={pending !== null || !progress.ready} onClick={() => { void execute('achieve') }}><FlagIcon />完成</Button>
			<Button variant="ghost" size="icon-sm" type="button" aria-label="取消长期目标" disabled={pending !== null} onClick={() => {
				if (window.confirm('取消后会保留全部历史，确定取消这个长期目标吗？')) void execute('cancel')
			}}><XIcon /></Button>
		</div>
		{value.latest_checkpoint ? <p className="mt-2 truncate pl-12 text-xs text-muted-foreground">最近检查点：{value.latest_checkpoint.summary}{value.latest_checkpoint.next_action ? ` · 下一步：${value.latest_checkpoint.next_action}` : ''}</p> : null}
		{error ? <p className="mt-2 pl-12 text-xs text-destructive" role="alert">{errorMessage(error)}</p> : null}
	</section>
}

function objectiveProgress(value: ObjectiveView) {
	const completed = value.progress.criteria_satisfied + value.progress.tasks_accepted
	const total = value.progress.criteria_total + value.progress.tasks_total
	const percent = total === 0 ? 0 : Math.round(completed / total * 100)
	const tone = percent === 100 ? 'bg-emerald-500' : percent >= 60 ? 'bg-amber-400' : percent >= 30 ? 'bg-orange-400' : 'bg-rose-400'
	return { percent, tone, ready: total > 0 && completed === total }
}
