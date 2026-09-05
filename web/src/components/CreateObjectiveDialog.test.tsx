import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { Topic } from '../types'
import { CreateObjectiveDialog } from './CreateObjectiveDialog'

const topic: Topic = {
	id: 'topic-1', key: 'TOP-1', title: '生产交付', description: '把项目推进到生产可用',
	status: 'OPEN', version: 1,
	created_at: '2026-09-05T00:00:00Z', updated_at: '2026-09-05T00:00:00Z',
}

describe('CreateObjectiveDialog', () => {
	it('要求可验证条件，并按行去重后创建长期目标', async () => {
		const user = userEvent.setup()
		const onCreate = vi.fn().mockResolvedValue(undefined)
		const onClose = vi.fn()
		render(<CreateObjectiveDialog topic={topic} open onClose={onClose} onCreate={onCreate} />)
		const dialog = screen.getByRole('dialog', { name: '设为长期目标' })

		await user.click(within(dialog).getByRole('button', { name: '开始长期目标' }))
		expect(within(dialog).getByRole('alert')).toHaveTextContent('至少一个可验证完成条件')

		await user.type(within(dialog).getByLabelText('可验证完成条件'), '全部测试通过\n全部测试通过\n完成手工验收')
		await user.keyboard('{Meta>}{Enter}{/Meta}')

		expect(onCreate).toHaveBeenCalledWith({
			root_topic_id: 'topic-1', statement: '把项目推进到生产可用', scope: '', constraints: [],
			completion_criteria: ['全部测试通过', '完成手工验收'],
		})
		expect(onClose).toHaveBeenCalledOnce()
	})
})
