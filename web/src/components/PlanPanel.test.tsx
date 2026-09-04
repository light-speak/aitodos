import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { AgentRun, PlanView, Topic } from '../types'
import { PlanPanel } from './PlanPanel'

const topic: Topic = {
	id: 'topic-1', key: 'TOP-1', title: '搜索', description: '', status: 'PLAN_REVIEW',
	version: 2, created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
}

const current: PlanView = {
	plan: {
		id: 'plan-1', key: 'PLN-1', topic_id: topic.id, status: 'IN_REVIEW',
		current_revision_id: 'revision-1', version: 1,
		created_at: topic.created_at, updated_at: topic.updated_at,
	},
	revision: {
		id: 'revision-1', plan_id: 'plan-1', revision: 1,
		summary: '建立索引并增加搜索 UI', rationale: '先做本地搜索', risks: '索引过期',
		readiness: {
			status: 'READY_FOR_REVIEW', confidence: 0.86, assumptions: ['数据量适合本地索引'], open_questions: [],
			alternatives: [{ title: '远程搜索服务', tradeoff: '扩展性更强，但增加部署复杂度' }],
		},
		created_at: topic.created_at,
		drafts: [{
			id: 'draft-1', plan_revision_id: 'revision-1', key: 'T1', title: '建立索引',
			description: '索引 Topic', acceptance_criteria: '可以检索', priority: 1, proposed_order: 0,
			test_cases: [{ id: 'test-1', task_draft_id: 'draft-1', title: 'Topic 可检索', description: '', required: true, sort_order: 0 }],
		}],
	},
	reviews: [],
}

const activePlanningRun: AgentRun = {
	id: 'run-1', purpose: 'PLANNING', topic_id: topic.id, status: 'RUNNING',
	profile_revision_id: 'profile-revision-1', subject_version: 1, session_resumed: false,
	lease_generation: 1, lease_expires_at: topic.updated_at, queued_at: topic.created_at,
	claimed_at: topic.created_at, created_at: topic.created_at, updated_at: topic.updated_at,
}

const planningProps = {
	planningRun: null,
	planningLoading: false,
	planningError: null,
	requestingPlanning: false,
	workersEnabled: true,
	onRequestPlanning: vi.fn().mockResolvedValue(undefined),
}

describe('PlanPanel', () => {
	it('展示不可变修订并批准创建 Task', async () => {
		const user = userEvent.setup()
		const onApprove = vi.fn().mockResolvedValue(undefined)
		render(<PlanPanel topic={topic} plan={current} loading={false} submitting={false} error={null}
			{...planningProps} onReload={() => undefined} onSubmit={vi.fn()} onReject={vi.fn()} onApprove={onApprove} />)

		expect(screen.getByText('Revision 1')).toBeInTheDocument()
		expect(screen.getByText('规划充分度 86%')).toBeInTheDocument()
		expect(screen.getByText('数据量适合本地索引')).toBeInTheDocument()
		expect(screen.getByText('Topic 可检索')).toBeInTheDocument()
		await user.type(screen.getByPlaceholderText('审核意见（要求修改时必填）'), '批准执行')
		await user.click(screen.getByRole('button', { name: '批准并创建 1 个 Task' }))
		expect(onApprove).toHaveBeenCalledWith('批准执行')
	})

	it('从空 Topic 编写多个 Task 草案', async () => {
		const user = userEvent.setup()
		const onSubmit = vi.fn().mockResolvedValue(undefined)
		render(<PlanPanel topic={{ ...topic, status: 'OPEN', version: 1 }} plan={null} loading={false} submitting={false} error={null}
			{...planningProps} onReload={() => undefined} onSubmit={onSubmit} onReject={vi.fn()} onApprove={vi.fn()} />)

		await user.click(screen.getByRole('button', { name: '手动编写' }))
		await user.type(screen.getByPlaceholderText('方案摘要：要实现什么，如何拆分'), '搜索方案')
		await user.type(screen.getByPlaceholderText('Task 1 标题'), '建立索引')
		await user.type(screen.getByPlaceholderText(/必测项/), 'Topic 可检索')
		await user.click(screen.getByRole('button', { name: '提交人工审核' }))
		expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({
			summary: '搜索方案', drafts: [expect.objectContaining({ title: '建立索引' })],
		}))
	})

	it('优先展示 Agent 自动规划状态，手工入口降为备用', () => {
		render(<PlanPanel topic={{ ...topic, status: 'OPEN', version: 1 }} plan={null} loading={false} submitting={false} error={null}
			{...planningProps} planningRun={activePlanningRun} onReload={() => undefined}
			onSubmit={vi.fn()} onReject={vi.fn()} onApprove={vi.fn()} />)

		expect(screen.getByText('Agent 正在分析讨论并整理方案…')).toBeInTheDocument()
		expect(screen.getByRole('button', { name: '手动编写' })).toBeInTheDocument()
	})

	it('规划失败时允许显式重试', async () => {
		const user = userEvent.setup()
		const onRequestPlanning = vi.fn().mockResolvedValue(undefined)
		render(<PlanPanel topic={{ ...topic, status: 'OPEN', version: 1 }} plan={null} loading={false} submitting={false} error={null}
			{...planningProps} planningRun={{ ...activePlanningRun, status: 'FAILED', failure_message: '模型不可用' }}
			onRequestPlanning={onRequestPlanning} onReload={() => undefined}
			onSubmit={vi.fn()} onReject={vi.fn()} onApprove={vi.fn()} />)

		expect(screen.getByText(/模型不可用/)).toBeInTheDocument()
		await user.click(screen.getByRole('button', { name: '重新让 Agent 分析' }))
		expect(onRequestPlanning).toHaveBeenCalledTimes(1)
	})
})
