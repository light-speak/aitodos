import { useCallback, useEffect, useState } from 'react'
import { BookMarkedIcon, BrainIcon, PinIcon, PlusIcon, RefreshCwIcon, TagsIcon } from 'lucide-react'

import {
	challengeExperience, confirmExperience, createLabel, createSubjectDecision, createSubjectExperience, createTaskCISnapshot,
	errorMessage, getLabels, getSubjectDecisions, getSubjectExperiences, getSubjectLabels,
	getTaskCISnapshots, pinExperience, setSubjectLabel,
} from '../api/client'
import type { CISnapshot, CIState, Decision, ExperienceRecord, KnowledgeLabel } from '../types'
import { MarkdownContent } from './MarkdownContent'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Textarea } from './ui/textarea'

type SubjectKind = 'topics' | 'tasks'

export function SubjectKnowledgePanel({ kind, id }: { kind: SubjectKind; id: string }) {
	const knowledge = useSubjectKnowledge(kind, id)
	return <section className="space-y-4 py-5" aria-label="知识与标签">
		<div className="flex items-center justify-between gap-3">
			<div><h3 className="flex items-center gap-2 text-sm font-semibold"><BookMarkedIcon className="size-4" />知识与标签</h3><p className="text-xs text-muted-foreground">固化长期有效的结论；标签仅用于查找和展示。</p></div>
			<Button variant="ghost" size="sm" type="button" onClick={knowledge.reload}><RefreshCwIcon />刷新</Button>
		</div>
		{knowledge.error ? <p className="text-sm text-destructive" role="alert">{errorMessage(knowledge.error)}</p> : null}
		<LabelsEditor knowledge={knowledge} />
		<DecisionsEditor knowledge={knowledge} />
		<ExperiencesEditor knowledge={knowledge} />
		{kind === 'tasks' ? <CISnapshotsEditor knowledge={knowledge} /> : null}
	</section>
}

type KnowledgeState = ReturnType<typeof useSubjectKnowledge>

function useSubjectKnowledge(kind: SubjectKind, id: string) {
	const [labels, setLabels] = useState<KnowledgeLabel[]>([])
	const [attachedLabels, setAttachedLabels] = useState<KnowledgeLabel[]>([])
	const [decisions, setDecisions] = useState<Decision[]>([])
	const [experiences, setExperiences] = useState<ExperienceRecord[]>([])
	const [ciSnapshots, setCISnapshots] = useState<CISnapshot[]>([])
	const [error, setError] = useState<unknown>(null)
	const [busy, setBusy] = useState(false)
	const reload = useCallback(() => {
		const controller = new AbortController()
		void Promise.all([
			getLabels(controller.signal), getSubjectLabels(kind, id, controller.signal),
			getSubjectDecisions(kind, id, controller.signal),
			getSubjectExperiences(kind, id, controller.signal),
			kind === 'tasks' ? getTaskCISnapshots(id, controller.signal) : Promise.resolve([]),
		]).then(([allLabels, currentLabels, currentDecisions, currentExperiences, snapshots]) => {
			setLabels(allLabels); setAttachedLabels(currentLabels); setDecisions(currentDecisions); setExperiences(currentExperiences); setCISnapshots(snapshots); setError(null)
		}).catch((cause: unknown) => { if (!controller.signal.aborted) setError(cause) })
		return () => controller.abort()
	}, [id, kind])
	useEffect(reload, [reload])
	const mutate = useCallback(async (operation: () => Promise<unknown>) => {
		setBusy(true); setError(null)
		try { await operation(); reload() } catch (cause: unknown) { setError(cause) } finally { setBusy(false) }
	}, [reload])
	return { kind, id, labels, attachedLabels, decisions, experiences, ciSnapshots, error, busy, reload, mutate }
}

function ExperiencesEditor({ knowledge }: { knowledge: KnowledgeState }) {
	const [editing, setEditing] = useState(false)
	const [title, setTitle] = useState('')
	const [summary, setSummary] = useState('')
	const [guidance, setGuidance] = useState('')
	const [applicability, setApplicability] = useState('')
	const [projectWide, setProjectWide] = useState(false)
	function submit() {
		void knowledge.mutate(async () => {
			await createSubjectExperience(knowledge.kind, knowledge.id, {
				title, summary, guidance, applicability, project_wide: projectWide, pinned: false,
			})
			setTitle(''); setSummary(''); setGuidance(''); setApplicability(''); setProjectWide(false); setEditing(false)
		})
	}
	return <div className="rounded-xl border p-4">
		<div className="flex items-start justify-between gap-3">
			<div><h4 className="flex items-center gap-2 text-sm font-medium"><BrainIcon className="size-4" />可复用经验</h4><p className="mt-1 text-xs text-muted-foreground">只把确认过的做法写入经验；Run 按相关性和证据动态召回摘要。</p></div>
			<Button variant="ghost" size="sm" type="button" onClick={() => setEditing((value) => !value)}>{editing ? '取消' : '记录经验'}</Button>
		</div>
		{knowledge.experiences.length === 0 ? <p className="mt-3 text-sm text-muted-foreground">尚无经验记录</p> : <ul className="mt-3 space-y-3">{knowledge.experiences.map((item) => <li key={item.id} className="rounded-lg bg-muted/40 p-3">
			<div className="flex flex-wrap items-center gap-2"><span className="text-sm font-medium">{item.title}</span><Badge variant="outline" className="font-mono text-[10px]">{item.key}</Badge><Badge variant={item.status === 'ACTIVE' ? 'secondary' : 'outline'}>{experienceStatusLabel(item.status)}</Badge>{item.project_wide ? <Badge variant="outline">项目范围</Badge> : null}</div>
			<p className="mt-2 text-sm text-muted-foreground">{item.summary}</p>
			<details className="mt-2 text-sm"><summary className="cursor-pointer text-xs text-muted-foreground">查看适用条件与完整做法</summary><p className="mt-2">适用：{item.applicability}</p><div className="mt-1"><MarkdownContent content={item.guidance} /></div></details>
			{item.status === 'CANDIDATE' ? <p className="mt-2 text-xs text-amber-700">等待人工确认，不会进入 Agent 上下文</p> : null}
			<div className="mt-3 flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground"><span>验证 {item.verification_count} 次 · 有效 {item.successful_applications} 次 · 反例 {item.failed_applications} 次 · 召回 {item.recall_count} 次</span>{item.status === 'ACTIVE' ? <span className="flex gap-1"><Button type="button" size="sm" variant="ghost" disabled={knowledge.busy} onClick={() => { void knowledge.mutate(() => pinExperience(item.id, !item.pinned)) }}><PinIcon />{item.pinned ? '取消固定' : '固定'}</Button><Button type="button" size="sm" variant="ghost" disabled={knowledge.busy} onClick={() => { void knowledge.mutate(() => challengeExperience(item.id)) }}>标记反例</Button></span> : item.status === 'CANDIDATE' ? <span className="flex gap-1"><Button type="button" size="sm" disabled={knowledge.busy} aria-label={`确认采用${item.title}`} onClick={() => { void knowledge.mutate(() => confirmExperience(item.id)) }}>确认采用</Button><Button type="button" size="sm" variant="ghost" disabled={knowledge.busy} aria-label={`不采用${item.title}`} onClick={() => { void knowledge.mutate(() => challengeExperience(item.id)) }}>不采用</Button></span> : null}</div>
		</li>)}</ul>}
		{editing ? <form className="mt-3 grid gap-2" onSubmit={(event) => { event.preventDefault(); submit() }}>
			<Input aria-label="经验标题" placeholder="经验标题" value={title} onChange={(event) => setTitle(event.currentTarget.value)} />
			<Textarea aria-label="经验摘要" placeholder="给 Context 使用的短摘要" value={summary} onChange={(event) => setSummary(event.currentTarget.value)} />
			<Textarea aria-label="经验做法" placeholder="完整做法与证据" value={guidance} onChange={(event) => setGuidance(event.currentTarget.value)} />
			<Input aria-label="适用条件" placeholder="什么情况下适用" value={applicability} onChange={(event) => setApplicability(event.currentTarget.value)} />
			<label className="flex items-center gap-2 text-xs text-muted-foreground"><input type="checkbox" checked={projectWide} onChange={(event) => setProjectWide(event.currentTarget.checked)} />允许项目内其他 Topic 和 Task 召回</label>
			<Button className="w-fit" type="submit" disabled={knowledge.busy || !title.trim() || !summary.trim() || !guidance.trim() || !applicability.trim()}>保存已验证经验</Button>
		</form> : null}
	</div>
}

function experienceStatusLabel(status: ExperienceRecord['status']): string {
	return ({ CANDIDATE: '候选', ACTIVE: '有效', CHALLENGED: '有反例', SUPERSEDED: '已替代' })[status]
}

function LabelsEditor({ knowledge }: { knowledge: KnowledgeState }) {
	const [name, setName] = useState('')
	const attached = new Set(knowledge.attachedLabels.map((label) => label.id))
	return <div className="rounded-xl border p-4">
		<h4 className="mb-3 flex items-center gap-2 text-sm font-medium"><TagsIcon className="size-4" />标签</h4>
		<div className="flex flex-wrap gap-2">{knowledge.labels.map((label) => <button key={label.id} type="button" disabled={knowledge.busy} aria-pressed={attached.has(label.id)} className={`rounded-full border px-2.5 py-1 text-xs ${attached.has(label.id) ? 'bg-muted font-medium' : 'text-muted-foreground opacity-70'}`} style={{ borderColor: label.color }} onClick={() => { void knowledge.mutate(() => setSubjectLabel(knowledge.kind, knowledge.id, label.id, !attached.has(label.id))) }}>{label.name}</button>)}</div>
		<form className="mt-3 flex gap-2" onSubmit={(event) => { event.preventDefault(); const value = name.trim(); if (!value) return; void knowledge.mutate(async () => { const label = await createLabel(value, '#64748B'); await setSubjectLabel(knowledge.kind, knowledge.id, label.id, true); setName('') }) }}>
			<Input aria-label="新标签名称" placeholder="新标签" value={name} onChange={(event) => setName(event.currentTarget.value)} />
			<Button type="submit" variant="outline" disabled={knowledge.busy || !name.trim()}><PlusIcon />添加</Button>
		</form>
	</div>
}

function DecisionsEditor({ knowledge }: { knowledge: KnowledgeState }) {
	const [editing, setEditing] = useState(false)
	const [title, setTitle] = useState('')
	const [content, setContent] = useState('')
	function submit() {
		void knowledge.mutate(async () => { await createSubjectDecision(knowledge.kind, knowledge.id, title, content); setTitle(''); setContent(''); setEditing(false) })
	}
	return <div className="rounded-xl border p-4">
		<div className="flex items-center justify-between"><h4 className="text-sm font-medium">有效决策</h4><Button variant="ghost" size="sm" type="button" onClick={() => setEditing((value) => !value)}>{editing ? '取消' : '记录决策'}</Button></div>
		{knowledge.decisions.length === 0 ? <p className="mt-3 text-sm text-muted-foreground">尚无固化决策</p> : <ul className="mt-3 space-y-3">{knowledge.decisions.map((decision) => <li key={decision.id} className="rounded-lg bg-muted/40 p-3"><div className="flex items-center gap-2"><span className="text-sm font-medium">{decision.title}</span><Badge variant="outline" className="font-mono text-[10px]">{decision.key}</Badge></div><div className="mt-2 text-sm"><MarkdownContent content={decision.content} /></div></li>)}</ul>}
		{editing ? <form className="mt-3 space-y-2" onSubmit={(event) => { event.preventDefault(); submit() }}><Input aria-label="决策标题" placeholder="决策标题" value={title} onChange={(event) => setTitle(event.currentTarget.value)} /><Textarea aria-label="决策内容" placeholder="结论、原因和约束" value={content} onChange={(event) => setContent(event.currentTarget.value)} /><Button type="submit" disabled={knowledge.busy || !title.trim() || !content.trim()}>保存不可变决策</Button></form> : null}
	</div>
}

function CISnapshotsEditor({ knowledge }: { knowledge: KnowledgeState }) {
	const [editing, setEditing] = useState(false)
	const [provider, setProvider] = useState('GitHub Actions')
	const [commitSHA, setCommitSHA] = useState('')
	const [state, setState] = useState<CIState>('PASSED')
	const latest = knowledge.ciSnapshots[0]
	return <div className="rounded-xl border p-4">
		<div className="flex items-center justify-between"><h4 className="text-sm font-medium">CI 状态</h4><Button variant="ghost" size="sm" type="button" onClick={() => setEditing((value) => !value)}>{editing ? '取消' : '导入快照'}</Button></div>
		{latest ? <p className="mt-3 text-sm"><Badge variant={latest.state === 'PASSED' ? 'secondary' : latest.state === 'FAILED' ? 'destructive' : 'outline'}>{latest.state}</Badge><span className="ml-2">{latest.provider} · <span className="font-mono text-xs">{latest.commit_sha.slice(0, 8)}</span></span></p> : <p className="mt-3 text-sm text-muted-foreground">尚未导入 CI 结果</p>}
		{editing ? <form className="mt-3 grid gap-2 sm:grid-cols-2" onSubmit={(event) => { event.preventDefault(); void knowledge.mutate(async () => { await createTaskCISnapshot(knowledge.id, { provider, commit_sha: commitSHA, state, source_url: '' }); setEditing(false) }) }}><Input aria-label="CI 提供方" value={provider} onChange={(event) => setProvider(event.currentTarget.value)} /><Input aria-label="Commit SHA" placeholder="Commit SHA" value={commitSHA} onChange={(event) => setCommitSHA(event.currentTarget.value)} /><select aria-label="CI 状态" className="h-9 rounded-md border bg-transparent px-3 text-sm" value={state} onChange={(event) => setState(event.currentTarget.value as CIState)}>{(['PASSED', 'FAILED', 'PENDING', 'CANCELLED', 'UNKNOWN'] as const).map((item) => <option key={item}>{item}</option>)}</select><Button type="submit" disabled={knowledge.busy || !provider.trim() || commitSHA.trim().length < 7}>保存快照</Button></form> : null}
	</div>
}
