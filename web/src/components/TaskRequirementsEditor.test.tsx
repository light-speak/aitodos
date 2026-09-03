import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Task } from '../types'
import { TaskRequirementsEditor } from './TaskRequirementsEditor'

const task: Task = {
	id: 'task-1', key: 'ATS-1', title: '导出项目', title_source: 'HUMAN', title_locked: true,
	description: '旧描述', acceptance_criteria: '', status: 'READY', priority: 2,
	assessment_input_version: 1, version: 1, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
}

describe('TaskRequirementsEditor', () => {
	it('edits requirements and asks before cancelling', async () => {
		const onUpdate = vi.fn().mockResolvedValue(undefined)
		const onCancel = vi.fn().mockResolvedValue(undefined)
		render(<TaskRequirementsEditor task={task} onUpdate={onUpdate} onCancel={onCancel} onArchive={vi.fn()} />)
		fireEvent.click(screen.getByRole('button', { name: '编辑需求' }))
		fireEvent.change(screen.getByLabelText('Task 验收标准'), { target: { value: 'ZIP 可以恢复' } })
		fireEvent.click(screen.getByRole('button', { name: '保存修改' }))
		await waitFor(() => expect(onUpdate).toHaveBeenCalledWith(expect.objectContaining({ acceptance_criteria: 'ZIP 可以恢复' })))

		fireEvent.click(screen.getByRole('button', { name: '取消 Task' }))
		expect(onCancel).not.toHaveBeenCalled()
		fireEvent.click(screen.getByRole('button', { name: '确认' }))
		await waitFor(() => expect(onCancel).toHaveBeenCalledOnce())
	})
})
