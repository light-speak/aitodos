import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { SearchItem } from '../types'
import { ProjectSearchDialog } from './ProjectSearchDialog'

const result: SearchItem = {
	document_id: 'MESSAGE:message-1', kind: 'MESSAGE', source_id: 'message-1',
	subject_kind: 'TOPIC', subject_id: 'topic-1', stable_key: 'TOP-001#message-1',
	title: 'Agent 消息', snippet: 'Session 不是事实来源，需要持久上下文。',
	status: 'AGENT', current: true, updated_at: '2026-08-31T00:00:00Z',
}

describe('ProjectSearchDialog', () => {
	it('提交过滤条件、打开来源并按需读取下一页', async () => {
		const onSearch = vi.fn()
		const onOpenItem = vi.fn()
		const onLoadMore = vi.fn()
		render(<ProjectSearchDialog
			items={[result]} loading={false} error={null} nextCursor="MQ"
			evalCases={[]} evalRuns={[]} evalLoading={false} evalError={null}
			onClose={() => undefined} onSearch={onSearch} onLoadMore={onLoadMore} onOpenItem={onOpenItem}
			onAddEvalResult={() => undefined} onRemoveEvalResult={() => undefined} onRunEval={() => undefined}
		/>)

		await userEvent.type(screen.getByLabelText('搜索项目内容'), '持久上下文')
		await userEvent.selectOptions(screen.getByLabelText('内容类型'), 'MESSAGE')
		await userEvent.click(screen.getByRole('button', { name: '搜索' }))
		expect(onSearch).toHaveBeenCalledWith({ query: '持久上下文', kinds: ['MESSAGE'], only_current: true, limit: 20 })

		await userEvent.click(screen.getByRole('button', { name: /Agent 消息/ }))
		expect(onOpenItem).toHaveBeenCalledWith(result)
		await userEvent.click(screen.getByRole('button', { name: '加载更多' }))
		expect(onLoadMore).toHaveBeenCalled()
	})

	it('从当前搜索结果维护评测集并展示最近指标', async () => {
		const onAddEvalResult = vi.fn()
		const onRemoveEvalResult = vi.fn()
		const onRunEval = vi.fn()
		render(<ProjectSearchDialog
			items={[result]} loading={false} error={null}
			evalCases={[{
				id: 'case-1', query: '持久上下文', kinds: ['MESSAGE'], only_current: true, note: '', active: true,
				relevances: [{ document_id: result.document_id, stable_key: result.stable_key, title: result.title, available: true }],
				created_at: '2026-09-03T00:00:00Z', updated_at: '2026-09-03T00:00:00Z',
			}]}
			evalRuns={[{
				id: 'run-1', engine: 'LEXICAL_V1', k: 10, case_count: 1, relevant_count: 1,
				recalled_count: 1, hit_cases: 1, recall_at_k: 1, hit_at_k: 1, mrr: 1,
				results: [], created_at: '2026-09-03T00:00:00Z',
			}]}
			evalLoading={false} evalError={null}
			onClose={() => undefined} onSearch={() => undefined} onLoadMore={() => undefined} onOpenItem={() => undefined}
			onAddEvalResult={onAddEvalResult} onRemoveEvalResult={onRemoveEvalResult} onRunEval={onRunEval}
		/>)

		await userEvent.type(screen.getByLabelText('搜索项目内容'), '持久上下文')
		await userEvent.selectOptions(screen.getByLabelText('内容类型'), 'MESSAGE')
		await userEvent.click(screen.getByRole('button', { name: '加入评测' }))
		expect(onAddEvalResult).toHaveBeenCalledWith({
			query: '持久上下文', kinds: ['MESSAGE'], only_current: true, document_id: result.document_id,
		})

		await userEvent.click(screen.getByRole('button', { name: '检索评测' }))
		expect(screen.getByText('Recall@10')).toBeInTheDocument()
		expect(screen.getAllByText('100%').length).toBeGreaterThan(0)
		await userEvent.click(screen.getByRole('button', { name: '运行评测' }))
		expect(onRunEval).toHaveBeenCalledWith(10)
		await userEvent.click(screen.getByRole('button', { name: '移除相关结果' }))
		expect(onRemoveEvalResult).toHaveBeenCalledWith('case-1', result.document_id)
	})
})
