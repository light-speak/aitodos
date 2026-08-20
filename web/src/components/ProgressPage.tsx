import { AlertCircleIcon, CheckCircle2Icon, GaugeIcon, RefreshCwIcon, SparklesIcon, TestTube2Icon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { ProjectProgress, RunPurpose, RunUsageSummary } from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card'
import { Skeleton } from './ui/skeleton'

interface ProgressPageProps {
	progress: ProjectProgress | null
	usage: RunUsageSummary | null
	loading: boolean
	error: unknown
	onReload: () => void
}

export function ProgressPage({ progress, usage, loading, error, onReload }: ProgressPageProps) {
	if (loading) return <ProgressLoading />
	if (error) {
		return <div className="px-4 sm:px-6 lg:px-8"><div className="flex items-center gap-3 rounded-xl border border-destructive/20 bg-destructive/5 p-4 text-sm text-destructive"><AlertCircleIcon className="size-4" />{errorMessage(error)}<Button className="ml-auto" variant="ghost" size="sm" onClick={onReload}><RefreshCwIcon />重试</Button></div></div>
	}
	if (!progress) return null
	return (
		<section className="grid gap-4 px-4 pb-8 sm:px-6 lg:grid-cols-3 lg:px-8" aria-label="项目整体进度">
			<ProgressCard icon={<CheckCircle2Icon />} title="严格进度" value={formatPercent(progress.strict_percent)} description={`${progress.accepted_tasks} / ${progress.total_tasks} Tasks 已人工验收`} percent={progress.strict_percent} />
			<ProgressCard icon={<SparklesIcon />} title="估算进度" value={progress.forecast_percent === undefined ? '未知' : formatPercent(progress.forecast_percent)} description={`估算覆盖 ${progress.estimated_tasks} / ${progress.total_tasks} Tasks`} percent={progress.forecast_percent ?? 0} muted={progress.forecast_percent === undefined} />
			<Card>
				<CardHeader><div className="flex items-center gap-2 text-muted-foreground"><TestTube2Icon className="size-4" /><span className="text-xs font-medium uppercase">必测项证据</span></div><CardTitle className="text-3xl">{progress.verified_passed_tests} / {progress.required_tests}</CardTitle><CardDescription>只有命令证据或人工确认计入已验证</CardDescription></CardHeader>
				<CardContent className="flex flex-wrap gap-2"><Badge variant="secondary">已验证 {progress.verified_passed_tests}</Badge><Badge variant="outline">仅 Agent 报告 {progress.agent_reported_passed_tests}</Badge></CardContent>
			</Card>
			<Card className="lg:col-span-3"><CardContent className="grid gap-4 py-5 sm:grid-cols-3"><ProgressFact label="估算覆盖率" value={formatPercent(progress.estimate_coverage)} /><ProgressFact label="总点数" value={progress.total_points === 0 ? '未知' : String(progress.total_points)} /><ProgressFact label="估算剩余" value={progress.total_points === 0 ? '未知' : String(progress.remaining_points)} /></CardContent></Card>
			{usage ? <UsageCard usage={usage} /> : null}
		</section>
	)
}

function UsageCard({ usage }: { usage: RunUsageSummary }) {
	const cacheRate = calculateCacheRate(usage)
	return <Card className="lg:col-span-3"><CardHeader><div className="flex flex-wrap items-center justify-between gap-2"><div><CardTitle>真实 Run 用量</CardTitle><CardDescription>累计输入不是单次上下文大小；缓存输入已包含在累计输入中</CardDescription></div><Badge variant="outline">已采集 {usage.runs_with_usage} / {usage.total_runs} Runs</Badge></div></CardHeader><CardContent className="grid gap-5"><div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-5"><UsageFact label="累计输入" value={formatTokens(usage.input_tokens)} /><UsageFact label="其中缓存" value={formatTokens(usage.cached_input_tokens)} /><UsageFact label="非缓存输入" value={formatTokens(usage.uncached_input_tokens)} /><UsageFact label="缓存命中率" value={cacheRate === undefined ? '未知' : formatPercent(cacheRate)} /><UsageFact label="累计输出" value={formatTokens(usage.output_tokens)} /></div>{usage.by_purpose.length > 0 ? <div className="overflow-x-auto"><table className="w-full min-w-[36rem] text-sm"><thead><tr className="border-b text-left text-xs text-muted-foreground"><th className="py-2 font-medium">职责</th><th className="py-2 font-medium">采集 Run</th><th className="py-2 text-right font-medium">输入</th><th className="py-2 text-right font-medium">缓存</th><th className="py-2 text-right font-medium">输出</th></tr></thead><tbody>{usage.by_purpose.map((item) => <tr className="border-b last:border-0" key={item.purpose}><td className="py-2.5">{purposeLabel(item.purpose)}</td><td className="py-2.5">{item.runs_with_usage} / {item.total_runs}</td><td className="py-2.5 text-right font-mono">{formatTokens(item.input_tokens)}</td><td className="py-2.5 text-right font-mono">{formatTokens(item.cached_input_tokens)}</td><td className="py-2.5 text-right font-mono">{formatTokens(item.output_tokens)}</td></tr>)}</tbody></table></div> : null}<p className="text-xs leading-5 text-muted-foreground">单次请求峰值和模型请求次数只有 Adapter 能可靠提供时才记录；当前无法采集时显示未知，不从累计值推断。成本也不会在缺少价格快照时估算。</p></CardContent></Card>
}

function UsageFact({ label, value }: { label: string; value: string }) {
	return <div><p className="text-xs text-muted-foreground">{label}</p><p className="font-mono text-xl font-semibold">{value}</p></div>
}

function calculateCacheRate(usage: RunUsageSummary): number | undefined {
	if (usage.cached_input_tokens === undefined || usage.uncached_input_tokens === undefined) return undefined
	const total = usage.cached_input_tokens + usage.uncached_input_tokens
	return total === 0 ? undefined : usage.cached_input_tokens * 100 / total
}

function formatTokens(value: number | undefined): string {
	return value === undefined ? '未知' : value.toLocaleString('en-US')
}

function purposeLabel(purpose: RunPurpose): string {
	return ({ PLANNING: '规划', TRIAGE: '任务评估', IMPLEMENTATION: '实现', REVISION: '修订', REVIEW: '审查' })[purpose]
}

function ProgressCard(props: { icon: React.ReactNode; title: string; value: string; description: string; percent: number; muted?: boolean }) {
	return <Card><CardHeader><div className="flex items-center gap-2 text-muted-foreground"><span className="[&_svg]:size-4">{props.icon}</span><span className="text-xs font-medium uppercase">{props.title}</span></div><CardTitle className={props.muted ? 'text-3xl text-muted-foreground' : 'text-3xl'}>{props.value}</CardTitle><CardDescription>{props.description}</CardDescription></CardHeader><CardContent><div className="h-2 overflow-hidden rounded-full bg-muted"><div className="h-full rounded-full bg-primary transition-[width]" style={{ width: `${Math.min(100, Math.max(0, props.percent))}%` }} /></div></CardContent></Card>
}

function ProgressFact({ label, value }: { label: string; value: string }) {
	return <div className="flex items-center gap-3"><GaugeIcon className="size-4 text-muted-foreground" /><div><p className="text-xs text-muted-foreground">{label}</p><p className="font-mono text-lg font-semibold">{value}</p></div></div>
}

function ProgressLoading() {
	return <div className="grid gap-4 px-4 sm:px-6 lg:grid-cols-3 lg:px-8">{[1, 2, 3].map((item) => <Skeleton className="h-48 rounded-xl" key={item} />)}</div>
}

function formatPercent(value: number): string {
	return `${Number.isInteger(value) ? value.toFixed(0) : value.toFixed(1)}%`
}
