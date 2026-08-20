import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'

import type { Task, TaskAssessmentState } from '../types'
import { TaskAssessmentPanel } from './TaskAssessmentPanel'

const task: Task = {
	id: 'task-1', key: 'ATS-001', title: '临时标题', title_source: 'AI', title_locked: false,
	description: '跨模块功能', acceptance_criteria: '人工回归通过', status: 'READY', priority: 2,
	assessment_input_version: 1, version: 2,
	created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
}

const state: TaskAssessmentState = {
	current: {
		id: 'assessment-1', task_id: 'task-1', task_assessment_version: 1, revision: 1,
		suggested_title: '实现跨模块功能', applied_title: '实现跨模块功能',
		scores: { technical_complexity: 4, requirement_uncertainty: 2, change_scope: 3, validation_burden: 3, human_dependency: 4, risk_and_reversibility: 2 },
		weighted_score: 3.1, complexity: 'C4', autonomy: 'A0', confidence: 0.8,
		rationale: '需要跨模块修改并进行人工环境验证', assumptions: ['测试环境由人工准备'],
		split_recommended: true, split_rationale: '建议拆分实现与环境验证',
		source_run_id: 'run-1', created_at: '2026-08-20T00:00:00Z',
	},
	history: [], stale: false,
}

it('展示可解释复杂度并允许人工编辑后锁定标题', async () => {
	const user = userEvent.setup()
	const onUpdateTitle = vi.fn().mockResolvedValue(undefined)
	render(<TaskAssessmentPanel task={task} state={state} loading={false} error={null} busy={false} onReload={() => undefined} onUpdateTitle={onUpdateTitle} />)

	expect(screen.getByText('C4')).toBeInTheDocument()
	expect(screen.getByText('A0')).toBeInTheDocument()
	expect(screen.getByText('需要跨模块修改并进行人工环境验证')).toBeInTheDocument()
	expect(screen.getByText('建议拆分实现与环境验证')).toBeInTheDocument()
	expect(screen.getByText('人工依赖')).toBeInTheDocument()

	await user.click(screen.getByRole('button', { name: '编辑并锁定标题' }))
	const input = screen.getByLabelText('Task 标题')
	await user.clear(input)
	await user.type(input, '人工确认后的标题')
	await user.click(screen.getByRole('button', { name: '保存并锁定' }))
	expect(onUpdateTitle).toHaveBeenCalledWith('人工确认后的标题')
})
