import { useState } from 'react'
import { LoaderCircleIcon, SearchIcon } from 'lucide-react'

import { errorMessage } from '../api/client'
import type { SearchItem, SearchKind, SearchQueryInput } from '../types'
import { Badge } from './ui/badge'
import { Button } from './ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from './ui/dialog'
import { Input } from './ui/input'

interface ProjectSearchDialogProps {
	items: SearchItem[]
	loading: boolean
	error: unknown
	nextCursor?: string
	onClose: () => void
	onSearch: (input: SearchQueryInput) => void
	onLoadMore: () => void
	onOpenItem: (item: SearchItem) => void
}

export function ProjectSearchDialog(props: ProjectSearchDialogProps) {
	const [query, setQuery] = useState('')
	const [kind, setKind] = useState<SearchKind | 'ALL'>('ALL')
	const [onlyCurrent, setOnlyCurrent] = useState(true)
	const [searched, setSearched] = useState(false)
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
			<DialogContent className="max-h-[calc(100svh-2rem)] gap-0 overflow-hidden p-0 sm:max-w-3xl">
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
						<Button type="submit" disabled={props.loading || !query.trim()}>{props.loading ? <LoaderCircleIcon className="animate-spin" /> : <SearchIcon />}搜索</Button>
					</div>
					<label className="flex w-fit items-center gap-2 text-xs text-muted-foreground"><input type="checkbox" checked={onlyCurrent} onChange={(event) => setOnlyCurrent(event.currentTarget.checked)} />仅当前有效版本</label>
				</form>
				<div className="min-h-72 overflow-y-auto px-3 py-3">
					{props.error ? <p className="rounded-lg bg-destructive/5 px-4 py-3 text-sm text-destructive" role="alert">{errorMessage(props.error)}</p> : null}
					{!props.loading && !props.error && props.items.length === 0 ? <p className="px-4 py-20 text-center text-sm text-muted-foreground">{searched ? '没有匹配结果。' : '输入关键词开始搜索。'}</p> : null}
					<div className="grid gap-1">{props.items.map((item) => <SearchResult key={item.document_id} item={item} onOpen={() => props.onOpenItem(item)} />)}</div>
					{props.nextCursor ? <div className="flex justify-center py-4"><Button type="button" variant="outline" disabled={props.loading} onClick={props.onLoadMore}>{props.loading ? '读取中…' : '加载更多'}</Button></div> : null}
				</div>
			</DialogContent>
		</Dialog>
	)
}

function SearchResult({ item, onOpen }: { item: SearchItem; onOpen: () => void }) {
	return <button type="button" className="rounded-xl px-4 py-3 text-left hover:bg-muted focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50" aria-label={`${item.title} ${item.stable_key}`} onClick={onOpen}>
		<div className="flex items-center gap-2"><Badge variant="outline">{kindLabel(item.kind)}</Badge><span className="font-mono text-xs text-muted-foreground">{item.stable_key}</span><Badge variant="secondary">{item.status}</Badge>{!item.current ? <Badge variant="secondary">历史版本</Badge> : null}</div>
		<p className="mt-2 font-medium">{item.title}</p>
		{item.snippet ? <p className="mt-1 line-clamp-2 text-sm leading-6 text-muted-foreground">{item.snippet}</p> : null}
	</button>
}

function kindLabel(kind: SearchKind): string {
	return ({
		TOPIC: 'Topic', TASK: 'Task', MESSAGE: '消息', PLAN_REVISION: 'Plan', CLARIFICATION: '待确认',
		DECISION: '决策', RUN_SUMMARY: 'Run 摘要', CI_CHECK: 'CI', LABEL: '标签', EXPERIENCE: '经验',
	})[kind]
}
