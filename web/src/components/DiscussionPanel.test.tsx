import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'

import type { DiscussionMessage, Task, TaskFeedback } from '../types'
import { DiscussionComposer, DiscussionMessages } from './DiscussionPanel'

const task: Task = {
	id: 'task-1', key: 'ATS-001', title: '完善错误处理', title_source: 'AI', title_locked: false,
	description: '补齐错误路径', acceptance_criteria: '回归测试通过', status: 'READY', priority: 1,
	assessment_input_version: 1, version: 3,
	created_at: '2026-08-25T00:00:00Z', updated_at: '2026-08-25T00:00:00Z',
}

it('Task 讨论默认询问 Agent，也可显式要求修改', async () => {
	const user = userEvent.setup()
	const onSendMessage = vi.fn().mockResolvedValue(undefined)
	render(<DiscussionComposer tasks={[task]} task={task} submitting={false} onSendMessage={onSendMessage} />)

	await user.type(screen.getByLabelText('发表消息'), '补充超时回归测试')
	await user.selectOptions(screen.getByLabelText('处理方式'), 'REQUEST_CHANGES')
	await user.click(screen.getByRole('button', { name: '要求修改' }))

	expect(onSendMessage).toHaveBeenCalledWith('补充超时回归测试', [], 'REQUEST_CHANGES')
})

it('在来源消息下展示失败状态并允许原位重新询问', async () => {
	const user = userEvent.setup()
	const onRetryFeedback = vi.fn().mockResolvedValue(undefined)
	const message: DiscussionMessage = {
		id: 'message-1', thread_id: 'thread-1', sequence: 1, author_kind: 'HUMAN',
		content: '还有哪些错误路径？', linked_task_ids: [], created_at: '2026-08-25T00:00:00Z',
	}
	const feedback: TaskFeedback = {
		id: 'feedback-1', task_id: task.id, source_message_id: message.id, intent: 'DISCUSS',
		status: 'FAILED', failure_message: 'Agent 进程退出',
		created_at: '2026-08-25T00:00:01Z', updated_at: '2026-08-25T00:00:02Z',
	}
	render(
		<DiscussionMessages
			messages={[message]}
			feedback={[feedback]}
			tasks={[task]}
			loading={false}
			error={null}
			onReload={vi.fn()}
			onOpenTask={vi.fn()}
			onRetryFeedback={onRetryFeedback}
		/>,
	)

	expect(screen.getByText('回答失败')).toBeInTheDocument()
	expect(screen.getByText('Agent 进程退出')).toBeInTheDocument()
	await user.click(screen.getByRole('button', { name: '重新询问' }))
	expect(onRetryFeedback).toHaveBeenCalledWith('feedback-1')
})
