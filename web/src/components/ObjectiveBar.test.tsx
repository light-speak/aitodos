import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { ObjectiveView } from '../types'
import { ObjectiveBar } from './ObjectiveBar'

function objectiveView(criteriaSatisfied = 1, tasksAccepted = 1): ObjectiveView {
	return {
		objective: {
			id: 'objective-1', key: 'OBJ-0001', root_topic_id: 'topic-1', status: 'ACTIVE',
			current_revision_id: 'revision-1', max_continuations: 20, continuation_count: 1,
			version: 2, created_at: '2026-09-05T00:00:00Z', updated_at: '2026-09-05T01:00:00Z',
		},
		revision: {
			id: 'revision-1', objective_id: 'objective-1', revision: 1, statement: '达到生产可用', scope: '',
			constraints: [], completion_criteria: [{ id: 'criterion-1', description: '测试通过' }],
			created_at: '2026-09-05T00:00:00Z',
		},
		latest_checkpoint: {
			id: 'checkpoint-1', objective_id: 'objective-1', sequence: 1, summary: '核心流程已完成',
			criteria: [{ criterion_id: 'criterion-1', status: 'SATISFIED', evidence: 'go test ./...' }],
			completed: ['实现'], remaining: ['验收'], risks: [], stop_reason: 'REVIEW_REQUIRED',
			next_action: '人工验收', created_at: '2026-09-05T01:00:00Z',
		},
		progress: { criteria_total: 1, criteria_satisfied: criteriaSatisfied, tasks_total: 1, tasks_accepted: tasksAccepted },
	}
}

describe('ObjectiveBar', () => {
	it('显示证据进度，并只在所有条件与 Task 完成后允许收口', async () => {
		const user = userEvent.setup()
		const onCommand = vi.fn().mockResolvedValue(undefined)
		const onOpenTopic = vi.fn()
		const { rerender } = render(<ObjectiveBar value={objectiveView(0, 1)} onOpenTopic={onOpenTopic} onCommand={onCommand} />)

		expect(screen.getByText('50%')).toBeInTheDocument()
		expect(screen.getByRole('button', { name: '完成' })).toBeDisabled()
		await user.click(screen.getByText('达到生产可用'))
		expect(onOpenTopic).toHaveBeenCalledWith('topic-1')

		rerender(<ObjectiveBar value={objectiveView()} onOpenTopic={onOpenTopic} onCommand={onCommand} />)
		expect(screen.getByText('100%')).toBeInTheDocument()
		await user.click(screen.getByRole('button', { name: '完成' }))
		expect(onCommand).toHaveBeenCalledWith('achieve')
	})
})
