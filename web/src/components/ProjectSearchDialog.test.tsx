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
			onClose={() => undefined} onSearch={onSearch} onLoadMore={onLoadMore} onOpenItem={onOpenItem}
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
})
