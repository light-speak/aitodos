import { useState } from 'react'
import type { FormEvent } from 'react'
import { AlertCircleIcon, CheckCircle2Icon, CircleHelpIcon, PlusIcon, RefreshCwIcon, SparklesIcon, TestTube2Icon, XCircleIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { CreateTaskEstimateInput, CreateTaskTestCaseInput, CreateTaskTestResultInput, TaskQuality, TaskTestCase } from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from './ui/dialog'
import { Input } from './ui/input'
import { Label } from './ui/label'

interface TaskQualityPanelProps {
	quality: TaskQuality | null
	loading: boolean
	error: unknown
	busy: boolean
	onReload: () => void
	onCreateEstimate: (input: CreateTaskEstimateInput) => Promise<void>
	onCreateTestCase: (input: CreateTaskTestCaseInput) => Promise<void>
	onRecordResult: (testCaseID: string, input: CreateTaskTestResultInput) => Promise<void>
}

export function TaskQualityPanel(props: TaskQualityPanelProps) {
	const [dialog, setDialog] = useState<'estimate' | 'test' | null>(null)
	if (props.loading) return <section className="py-5 text-sm text-muted-foreground">正在读取估算与测试证据…</section>
	return <section className="py-5"><header className="mb-4 flex items-center justify-between gap-3"><h3 className="flex items-center gap-2 text-xs font-medium tracking-wide text-muted-foreground uppercase"><TestTube2Icon className="size-4" />估算与测试</h3><div className="flex gap-2"><Button variant="outline" size="sm" onClick={() => setDialog('estimate')}><SparklesIcon />记录估算</Button><Button variant="outline" size="sm" onClick={() => setDialog('test')}><PlusIcon />添加测试项</Button></div></header>{props.error ? <p className="mb-3 flex items-center gap-2 text-sm text-destructive"><AlertCircleIcon className="size-4" />{errorMessage(props.error)}<Button variant="ghost" size="sm" onClick={props.onReload}><RefreshCwIcon />重试</Button></p> : null}<EstimateSummary quality={props.quality} /><TestCaseGroups quality={props.quality} busy={props.busy} onRecordResult={props.onRecordResult} />{dialog === 'estimate' ? <EstimateDialog busy={props.busy} onClose={() => setDialog(null)} onSave={async (input) => { await props.onCreateEstimate(input); setDialog(null) }} /> : null}{dialog === 'test' ? <TestCaseDialog busy={props.busy} onClose={() => setDialog(null)} onSave={async (input) => { await props.onCreateTestCase(input); setDialog(null) }} /> : null}</section>
}

function TestCaseGroups(props: Pick<TaskQualityPanelProps, 'quality' | 'busy' | 'onRecordResult'>) {
	const testCases = props.quality?.test_cases ?? []
	if (testCases.length === 0) return <p className="mt-4 rounded-xl border border-dashed p-4 text-sm text-muted-foreground">尚无测试项。测试项描述验收前必须检查的行为，Agent 也可以在规划时生成。</p>
	const requiredAttention = testCases.filter((item) => item.required && !isVerified(item))
	const verified = testCases.filter(isVerified)
	const optional = testCases.filter((item) => !item.required && !isVerified(item))
	return <div className="mt-4 grid gap-3">
		{requiredAttention.length > 0 ? <section><h4 className="mb-2 text-sm font-medium">需要你处理 {requiredAttention.length} 项</h4><div className="grid gap-2">{requiredAttention.map((item) => <TestCaseRow item={item} busy={props.busy} onRecord={props.onRecordResult} key={item.id} />)}</div></section> : <p className="rounded-xl border border-emerald-200 bg-emerald-50 p-3 text-sm text-emerald-700">所有必测项已有验证证据</p>}
		{verified.length > 0 ? <TestCaseDetails label={`已验证 ${verified.length} 项`} items={verified} /> : null}
		{optional.length > 0 ? <TestCaseDetails label={`可选检查 ${optional.length} 项`} items={optional} /> : null}
	</div>
}

function TestCaseDetails({ label, items }: { label: string; items: TaskTestCase[] }) {
	return <details className="rounded-xl border bg-muted/20 px-4 py-3"><summary className="cursor-pointer text-sm font-medium">{label}</summary><div className="mt-3 grid gap-2">{items.map((item) => { const status = resultStatus(item); return <div className="flex items-center gap-2 text-sm" key={item.id}><span className={status.tone}>{status.icon}</span><span className="min-w-0 flex-1 truncate">{item.title}</span><span className="text-xs text-muted-foreground">{status.label}</span></div> })}</div></details>
}

function isVerified(item: TaskTestCase): boolean {
	return item.latest_result?.outcome === 'PASSED' && item.latest_result.evidence_kind !== 'AGENT_REPORT'
}

function EstimateSummary({ quality }: { quality: TaskQuality | null }) {
	const estimate = quality?.estimate
	if (!estimate) return <div className="rounded-xl border bg-muted/20 p-4"><p className="text-sm font-medium">尚无估算</p><p className="mt-1 text-xs text-muted-foreground">Planner 或人工可以创建估算；未覆盖 Task 不会被当作 0 点。</p></div>
	return <div className="rounded-xl border bg-muted/20 p-4"><div className="flex items-start justify-between gap-3"><div><p className="font-mono text-lg font-semibold">{estimate.points} points · 剩余 {estimate.remaining_points}</p><p className="mt-1 text-xs text-muted-foreground">{estimate.source === 'AI' ? 'AI 估算' : '人工估算'} · {Math.round(estimate.confidence * 100)}% 置信度</p></div><Badge variant="outline">Revision {estimate.revision}</Badge></div><p className="mt-3 text-sm leading-6">{estimate.rationale}</p></div>
}

function TestCaseRow({ item, busy, onRecord }: { item: TaskTestCase; busy: boolean; onRecord: TaskQualityPanelProps['onRecordResult'] }) {
	const status = resultStatus(item)
	const canConfirm = !isVerified(item) && item.latest_result?.outcome !== 'FAILED'
	return <article className="rounded-xl border p-4"><div className="flex items-start gap-3"><span className={`mt-0.5 ${status.tone}`}>{status.icon}</span><div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-2"><p className="font-medium">{item.title}</p>{item.required ? <Badge variant="secondary">必测</Badge> : <Badge variant="outline">可选</Badge>}</div>{item.description ? <p className="mt-1 text-sm text-muted-foreground">{item.description}</p> : null}<p className="mt-2 text-xs text-muted-foreground">{status.label}</p></div>{canConfirm ? <Button variant="outline" size="sm" disabled={busy} aria-label={`人工确认${item.title}通过`} onClick={() => { void onRecord(item.id, { outcome: 'PASSED', evidence_kind: 'HUMAN', summary: '人工确认通过' }) }}>人工确认通过</Button> : null}</div></article>
}

function resultStatus(item: TaskTestCase): { label: string; tone: string; icon: React.ReactNode } {
	const result = item.latest_result
	if (!result) return { label: '尚未执行', tone: 'text-muted-foreground', icon: <CircleHelpIcon className="size-4" /> }
	if (result.outcome === 'PASSED' && result.evidence_kind === 'AGENT_REPORT') return { label: 'Agent 报告通过 · 未验证', tone: 'text-amber-600', icon: <CircleHelpIcon className="size-4" /> }
	if (result.outcome === 'PASSED') return { label: `${result.summary} · ${result.evidence_kind === 'COMMAND' ? '命令已验证' : '人工已验证'}`, tone: 'text-emerald-600', icon: <CheckCircle2Icon className="size-4" /> }
	return { label: result.summary, tone: 'text-rose-600', icon: <XCircleIcon className="size-4" /> }
}

function EstimateDialog(props: { busy: boolean; onClose: () => void; onSave: (input: CreateTaskEstimateInput) => Promise<void> }) {
	const [points, setPoints] = useState(3); const [remaining, setRemaining] = useState(3); const [confidence, setConfidence] = useState(70); const [rationale, setRationale] = useState(''); const [error, setError] = useState('')
	async function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); setError(''); try { await props.onSave({ points, remaining_points: remaining, confidence: confidence / 100, rationale }) } catch (submitError: unknown) { setError(errorMessage(submitError)) } }
	return <Dialog open onOpenChange={(open) => { if (!open) props.onClose() }}><DialogContent><DialogHeader><DialogTitle>记录 Task 估算</DialogTitle><DialogDescription>新的估算会成为不可变 Revision，历史仍可追溯。</DialogDescription></DialogHeader><form className="grid gap-4" onSubmit={(event) => { void submit(event) }}><div className="grid grid-cols-3 gap-3"><NumberInput id="points" label="总点数" value={points} onChange={setPoints} /><NumberInput id="remaining" label="剩余点数" value={remaining} onChange={setRemaining} /><NumberInput id="confidence" label="置信度 %" value={confidence} onChange={setConfidence} /></div><TextInput id="rationale" label="估算依据" value={rationale} onChange={setRationale} />{error ? <p className="text-sm text-destructive">{error}</p> : null}<DialogFooter><Button type="button" variant="outline" onClick={props.onClose}>取消</Button><Button type="submit" disabled={props.busy}>保存估算</Button></DialogFooter></form></DialogContent></Dialog>
}

function TestCaseDialog(props: { busy: boolean; onClose: () => void; onSave: (input: CreateTaskTestCaseInput) => Promise<void> }) {
	const [title, setTitle] = useState(''); const [description, setDescription] = useState(''); const [required, setRequired] = useState(true); const [error, setError] = useState('')
	async function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); setError(''); try { await props.onSave({ title, description, required, sort_order: 0 }) } catch (submitError: unknown) { setError(errorMessage(submitError)) } }
	return <Dialog open onOpenChange={(open) => { if (!open) props.onClose() }}><DialogContent><DialogHeader><DialogTitle>添加测试项</DialogTitle><DialogDescription>必测项必须有可验证的通过证据，Task 才能验收。</DialogDescription></DialogHeader><form className="grid gap-4" onSubmit={(event) => { void submit(event) }}><TextInput id="test-title" label="测试项" value={title} onChange={setTitle} /><TextInput id="test-description" label="预期行为" value={description} onChange={setDescription} /><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={required} onChange={(event) => setRequired(event.currentTarget.checked)} />必测项</label>{error ? <p className="text-sm text-destructive">{error}</p> : null}<DialogFooter><Button type="button" variant="outline" onClick={props.onClose}>取消</Button><Button type="submit" disabled={props.busy}>添加测试项</Button></DialogFooter></form></DialogContent></Dialog>
}

function TextInput({ id, label, value, onChange }: { id: string; label: string; value: string; onChange: (value: string) => void }) { return <div className="grid gap-2"><Label htmlFor={id}>{label}</Label><Input id={id} value={value} onChange={(event) => onChange(event.currentTarget.value)} /></div> }
function NumberInput({ id, label, value, onChange }: { id: string; label: string; value: number; onChange: (value: number) => void }) { return <div className="grid gap-2"><Label htmlFor={id}>{label}</Label><Input id={id} type="number" value={value} onChange={(event) => onChange(Number(event.currentTarget.value))} /></div> }
