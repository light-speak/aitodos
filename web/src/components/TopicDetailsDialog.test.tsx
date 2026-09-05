import { render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Topic } from '../types'
import { TopicDetailsDialog } from './TopicDetailsDialog'

const topic: Topic = {
	id: 'topic-1', key: 'TOP-1', title: '规划社区功能', description: '先讨论最小范围',
	status: 'OPEN', version: 1,
	created_at: '2026-08-23T00:00:00Z', updated_at: '2026-08-23T00:00:00Z',
}

describe('TopicDetailsDialog', () => {
	it('把讨论和执行拆成独立工作区，并将输入框留在讨论区', () => {
			render(<TopicDetailsDialog
				topic={topic}
				objective={null}
			tasks={[]}
			messages={[]}
			associations={[]}
			discussionLoading={false}
			relationLoading={false}
			submitting={false}
			discussionError={null}
			relationError={null}
			pendingRelationTaskIDs={new Set()}
			clarifications={[]}
			clarificationError={null}
			answeringClarificationID={null}
			onClose={vi.fn()}
			onReloadDiscussion={vi.fn()}
			onSendMessage={vi.fn().mockResolvedValue(undefined)}
			onAddRelation={vi.fn().mockResolvedValue(undefined)}
			onRemoveRelation={vi.fn().mockResolvedValue(undefined)}
			onOpenTask={vi.fn()}
			onReloadClarifications={vi.fn()}
			onAnswerClarification={vi.fn().mockResolvedValue(undefined)}
			plan={null}
			planLoading={false}
			planSubmitting={false}
			planError={null}
			planningRun={null}
			planningLoading={false}
			planningError={null}
			requestingPlanning={false}
			workersEnabled
			onReloadPlan={vi.fn()}
			onRequestPlanning={vi.fn().mockResolvedValue(undefined)}
			onSubmitPlan={vi.fn().mockResolvedValue(undefined)}
			onRejectPlan={vi.fn().mockResolvedValue(undefined)}
				onApprovePlan={vi.fn().mockResolvedValue(undefined)}
				onCreateObjective={vi.fn().mockResolvedValue(undefined)}
			/>)

		const discussion = screen.getByRole('region', { name: '讨论区' })
		const execution = screen.getByRole('region', { name: '执行区' })
		expect(within(discussion).getByRole('textbox', { name: '发表消息' })).toBeInTheDocument()
		expect(within(discussion).getByRole('region', { name: '讨论消息' })).toBeInTheDocument()
		expect(within(execution).getByRole('region', { name: 'Plan' })).toBeInTheDocument()
		expect(within(execution).getByRole('region', { name: '关联 Task' })).toBeInTheDocument()
		expect(screen.getByRole('dialog')).toHaveClass('data-[side=right]:sm:w-[min(88rem,calc(100vw-3rem))]')
	})
})
