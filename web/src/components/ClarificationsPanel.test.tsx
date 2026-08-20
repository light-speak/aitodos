import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { Clarification } from '../types'
import { ClarificationAnswerCard } from './ClarificationsPanel'

const question: Clarification = {
	id: 'clarification-1', task_id: 'task-1', source_run_id: 'run-1', continuation_purpose: 'IMPLEMENTATION',
	category: 'DECISION', question: '数据库迁移是否兼容旧版本？',
	options: [
		{ id: 'compatible', label: '兼容升级', description: '保留旧数据' },
		{ id: 'fresh', label: '仅新项目', description: '不迁移旧数据' },
	],
	recommended_option_id: 'compatible', allow_custom_answer: true, status: 'OPEN', version: 1,
	created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
}

describe('ClarificationAnswerCard', () => {
	it('突出推荐选项并提交选择', async () => {
		const user = userEvent.setup()
		const onAnswer = vi.fn().mockResolvedValue(undefined)
		render(<ClarificationAnswerCard item={question} answering={false} onAnswer={onAnswer} />)

		expect(screen.getByText('推荐')).toBeInTheDocument()
		await user.click(screen.getByRole('radio', { name: /兼容升级/ }))
		await user.click(screen.getByRole('button', { name: '回答并继续' }))

		expect(onAnswer).toHaveBeenCalledWith({ selected_option_id: 'compatible', custom_answer: '' })
	})

	it('允许提交自定义回答', async () => {
		const user = userEvent.setup()
		const onAnswer = vi.fn().mockResolvedValue(undefined)
		render(<ClarificationAnswerCard item={question} answering={false} onAnswer={onAnswer} />)

		await user.type(screen.getByLabelText('自定义回答'), '只兼容最近两个版本')
		await user.click(screen.getByRole('button', { name: '回答并继续' }))

		expect(onAnswer).toHaveBeenCalledWith({ selected_option_id: '', custom_answer: '只兼容最近两个版本' })
	})
})
