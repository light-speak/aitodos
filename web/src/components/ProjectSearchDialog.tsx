import { useState } from 'react'
import { ChartNoAxesCombinedIcon, LoaderCircleIcon, PlusIcon, SearchIcon, Trash2Icon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type {
	CreateRetrievalEvalCaseInput,
	RetrievalEvalCase,
	RetrievalEvalRun,
	SearchItem,
	SearchKind,
	SearchQueryInput,
} from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from './ui/dialog'
import { Input } from './ui/input'

interface ProjectSearchDialogProps {
	items: SearchItem[]
	loading: boolean
	error: unknown
	nextCursor?: string
	evalCases: RetrievalEvalCase[]
	evalRuns: RetrievalEvalRun[]
	evalLoading: boolean
	evalError: unknown
	onClose: () => void
	onSearch: (input: SearchQueryInput) => void
	onLoadMore: () => void
	onOpenItem: (item: SearchItem) => void
	onAddEvalResult: (input: CreateRetrievalEvalCaseInput) => void
	onRemoveEvalResult: (caseID: string, documentID: string) => void
	onRunEval: (k: number) => void
}

export function ProjectSearchDialog(props: ProjectSearchDialogProps) {
	const [query, setQuery] = useState('')
	const [kind, setKind] = useState<SearchKind | 'ALL'>('ALL')
	const [onlyCurrent, setOnlyCurrent] = useState(true)
	const [searched, setSearched] = useState(false)
	const [showEval, setShowEval] = useState(false)
	function submit(event: React.FormEvent) {
		event.preventDefault()
		const normalized = query.trim()
		if (!normalized) return
		setSearched(true)
		props.onSearch({
			query: normalized, ...(kind === 'ALL' ? {} : { kinds: [kind] }),
			only_current: onlyCurrent, limit: 20,
		})
	}
	return (
		<Dialog open onOpenChange={(open) => { if (!open) props.onClose() }}>
			<DialogContent className="max-h-[calc(100svh-2rem)] gap-0 overflow-hidden p-0 sm:max-w-4xl">
				<DialogHeader className="border-b px-6 py-5">
					<DialogTitle>搜索项目</DialogTitle>
					<DialogDescription>搜索 Topic、Task、Plan、讨论、决策和项目经验。原始日志与完整 Diff 不在默认索引中。</DialogDescription>
				</DialogHeader>
				<form className="grid gap-3 border-b bg-muted/20 px-6 py-4" onSubmit={submit}>
					<div className="flex gap-2">
						<label className="flex min-w-0 flex-1 items-center gap-2 rounded-lg border bg-background px-3">
							<SearchIcon className="size-4 text-muted-foreground" />
							<Input autoFocus aria-label="搜索项目内容" className="border-0 px-0 shadow-none focus-visible:ring-0" value={query} onChange={(event) => setQuery(event.currentTarget.value)} placeholder="输入编号、标题、需求、结论或讨论内容…" />
						</label>
						<select aria-label="内容类型" className="h-9 rounded-md border bg-background px-3 text-sm" value={kind} onChange={(event) => setKind(event.currentTarget.value as SearchKind | 'ALL')}>
							<option value="ALL">全部类型</option><option value="TOPIC">Topic</option><option value="TASK">Task</option><option value="MESSAGE">讨论消息</option><option value="PLAN_REVISION">Plan</option><option value="CLARIFICATION">待确认问题</option><option value="DECISION">决策</option><option value="EXPERIENCE">经验</option>
						</select>
						<Button type="button" variant="outline" onClick={() => setShowEval((current) => !current)}><ChartNoAxesCombinedIcon />检索评测</Button>
						<Button type="submit" disabled={props.loading || !query.trim()}>{props.loading ? <LoaderCircleIcon className="animate-spin" /> : <SearchIcon />}搜索</Button>
					</div>
					<label className="flex w-fit items-center gap-2 text-xs text-muted-foreground"><input type="checkbox" checked={onlyCurrent} onChange={(event) => setOnlyCurrent(event.currentTarget.checked)} />仅当前有效版本</label>
				</form>
				{showEval ? <RetrievalEvalPanel cases={props.evalCases} runs={props.evalRuns} loading={props.evalLoading} error={props.evalError} onRemove={props.onRemoveEvalResult} onRun={props.onRunEval} /> : null}
				<div className="min-h-72 overflow-y-auto px-3 py-3">
					{props.error ? <p className="rounded-lg bg-destructive/5 px-4 py-3 text-sm text-destructive" role="alert">{errorMessage(props.error)}</p> : null}
					{!props.loading && !props.error && props.items.length === 0 ? <p className="px-4 py-20 text-center text-sm text-muted-foreground">{searched ? '没有匹配结果。' : '输入关键词开始搜索。'}</p> : null}
					<div className="grid gap-1">{props.items.map((item) => <SearchResult
						key={item.document_id} item={item} onOpen={() => props.onOpenItem(item)}
						onAddEval={() => props.onAddEvalResult({
							query: query.trim(), kinds: kind === 'ALL' ? [] : [kind], only_current: onlyCurrent,
							document_id: item.document_id,
						})}
					/>)}</div>
					{props.nextCursor ? <div className="flex justify-center py-4"><Button type="button" variant="outline" disabled={props.loading} onClick={props.onLoadMore}>{props.loading ? '读取中…' : '加载更多'}</Button></div> : null}
				</div>
			</DialogContent>
		</Dialog>
	)
}

function SearchResult({ item, onOpen, onAddEval }: { item: SearchItem; onOpen: () => void; onAddEval: () => void }) {
	return <div className="group flex items-start gap-2 rounded-xl px-4 py-3 hover:bg-muted">
		<button type="button" className="min-w-0 flex-1 text-left focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50" aria-label={`${item.title} ${item.stable_key}`} onClick={onOpen}>
			<div className="flex items-center gap-2"><Badge variant="outline">{kindLabel(item.kind)}</Badge><span className="font-mono text-xs text-muted-foreground">{item.stable_key}</span><Badge variant="secondary">{item.status}</Badge>{!item.current ? <Badge variant="secondary">历史版本</Badge> : null}</div>
			<p className="mt-2 font-medium">{item.title}</p>
			{item.snippet ? <p className="mt-1 line-clamp-2 text-sm leading-6 text-muted-foreground">{item.snippet}</p> : null}
		</button>
		<Button type="button" size="sm" variant="ghost" className="shrink-0" onClick={onAddEval}><PlusIcon />加入评测</Button>
	</div>
}

function RetrievalEvalPanel(props: {
	cases: RetrievalEvalCase[]
	runs: RetrievalEvalRun[]
	loading: boolean
	error: unknown
	onRemove: (caseID: string, documentID: string) => void
	onRun: (k: number) => void
}) {
	const latest = props.runs[0]
	return <section className="max-h-72 overflow-y-auto border-b bg-muted/10 px-6 py-4" aria-label="检索评测面板">
		<div className="flex items-center justify-between gap-3">
			<div>
				<p className="font-medium">检索质量基线</p>
				<p className="text-xs text-muted-foreground">从真实搜索结果维护相关性判断，评测使用同一生产检索路径。</p>
			</div>
			<Button type="button" size="sm" disabled={props.loading || props.cases.length === 0} onClick={() => props.onRun(10)}>{props.loading ? <LoaderCircleIcon className="animate-spin" /> : <ChartNoAxesCombinedIcon />}运行评测</Button>
		</div>
		{props.error ? <p className="mt-3 text-sm text-destructive" role="alert">{errorMessage(props.error)}</p> : null}
		{latest ? <div className="mt-3 grid grid-cols-3 gap-2">
			<Metric label={`Recall@${latest.k}`} value={latest.recall_at_k} />
			<Metric label={`Hit@${latest.k}`} value={latest.hit_at_k} />
			<Metric label="MRR" value={latest.mrr} />
		</div> : null}
		{props.cases.length === 0 ? <p className="mt-3 rounded-lg border border-dashed p-3 text-sm text-muted-foreground">先搜索，再把应当命中的结果加入评测。</p> : <div className="mt-3 grid gap-2">{props.cases.map((item) => <div key={item.id} className="rounded-lg border bg-background px-3 py-2">
			<div className="flex flex-wrap items-center gap-2 text-sm"><span className="font-medium">{item.query}</span>{item.kinds.map((kind) => <Badge key={kind} variant="outline">{kindLabel(kind)}</Badge>)}</div>
			<div className="mt-2 flex flex-wrap gap-2">{item.relevances.map((relevance) => <span key={relevance.document_id} className="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-1 text-xs">
				<span>{relevance.available ? relevance.stable_key : relevance.document_id}</span>
				<button type="button" aria-label="移除相关结果" className="text-muted-foreground hover:text-destructive" onClick={() => props.onRemove(item.id, relevance.document_id)}><Trash2Icon className="size-3.5" /></button>
			</span>)}</div>
		</div>)}</div>}
	</section>
}

function Metric({ label, value }: { label: string; value: number }) {
	return <div className="rounded-lg border bg-background px-3 py-2"><p className="text-xs text-muted-foreground">{label}</p><p className="font-mono text-lg font-semibold">{Math.round(value * 100)}%</p></div>
}

function kindLabel(kind: SearchKind): string {
	return ({
		TOPIC: 'Topic', TASK: 'Task', MESSAGE: '消息', PLAN_REVISION: 'Plan', CLARIFICATION: '待确认',
		DECISION: '决策', RUN_SUMMARY: 'Run 摘要', CI_CHECK: 'CI', LABEL: '标签', EXPERIENCE: '经验',
		OBJECTIVE: '长期目标', CHECKPOINT: '目标检查点',
	})[kind]
}
