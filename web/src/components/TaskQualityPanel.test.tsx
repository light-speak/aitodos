import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { TaskQuality } from '../types'
import { TaskQualityPanel } from './TaskQualityPanel'

const taskQuality: TaskQuality = {
	estimate: {
		id: 'estimate-1', task_id: 'task-1', revision: 2, points: 5, remaining_points: 2,
		confidence: 0.8, rationale: '接口已完成，剩余浏览器回归', source: 'AI', source_run_id: 'run-1',
		created_at: '2026-08-20T00:00:00Z',
	},
	estimate_history: [],
	test_cases: [{
		id: 'test-1', task_id: 'task-1', title: '浏览器创建流程', description: '创建后进入等待执行',
		required: true, sort_order: 0, created_by: 'AGENT', source_run_id: 'run-1',
		created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
		latest_result: {
			id: 'result-1', test_case_id: 'test-1', task_id: 'task-1', outcome: 'PASSED',
			evidence_kind: 'AGENT_REPORT', summary: 'Agent 报告通过', source_run_id: 'run-1',
			created_at: '2026-08-20T00:00:00Z',
		},
	}],
}

describe('TaskQualityPanel', () => {
	it('展示 AI 估算，并把 Agent 自报结果标成未验证', async () => {
		const user = userEvent.setup()
		const onRecordResult = vi.fn().mockResolvedValue(undefined)
		render(<TaskQualityPanel quality={taskQuality} loading={false} error={null} busy={false} onReload={() => undefined} onCreateEstimate={vi.fn()} onCreateTestCase={vi.fn()} onRecordResult={onRecordResult} />)

		expect(screen.getByText('5 points · 剩余 2')).toBeInTheDocument()
		expect(screen.getByText('AI 估算 · 80% 置信度')).toBeInTheDocument()
		expect(screen.getByText('Agent 报告通过 · 未验证')).toBeInTheDocument()
		await user.click(screen.getByRole('button', { name: '人工确认浏览器创建流程通过' }))
		expect(onRecordResult).toHaveBeenCalledWith('test-1', expect.objectContaining({
			outcome: 'PASSED', summary: '人工确认通过',
		}))
	})

	it('最新证据已经人工验证通过时不再显示重复确认按钮', () => {
		const testCase = taskQuality.test_cases[0]!
		const verifiedQuality: TaskQuality = {
			...taskQuality,
			test_cases: [{
				...testCase,
				latest_result: {
					...testCase.latest_result!,
					evidence_kind: 'HUMAN',
					summary: '人工确认通过',
				},
			}],
		}
		render(<TaskQualityPanel quality={verifiedQuality} loading={false} error={null} busy={false} onReload={() => undefined} onCreateEstimate={vi.fn()} onCreateTestCase={vi.fn()} onRecordResult={vi.fn()} />)

		expect(screen.getByText('人工确认通过 · 人工已验证')).toBeInTheDocument()
		expect(screen.queryByRole('button', { name: '人工确认浏览器创建流程通过' })).not.toBeInTheDocument()
	})

	it('旧版命令记录缺少退出码时仍要求重新验证', () => {
		const testCase = taskQuality.test_cases[0]!
		const legacyQuality: TaskQuality = {
			...taskQuality,
			test_cases: [{
				...testCase,
				latest_result: { ...testCase.latest_result!, evidence_kind: 'COMMAND', command: 'pnpm test', summary: '旧测试通过' },
			}],
		}
		render(<TaskQualityPanel quality={legacyQuality} loading={false} error={null} busy={false} onReload={() => undefined} onCreateEstimate={vi.fn()} onCreateTestCase={vi.fn()} onRecordResult={vi.fn()} />)

		expect(screen.getByText('旧测试通过 · 旧版命令证据，需重新验证')).toBeInTheDocument()
		expect(screen.getByRole('button', { name: '人工确认浏览器创建流程通过' })).toBeInTheDocument()
	})

	it('默认只展开待处理必测项，并折叠 Runner 命令验证通过的项目', () => {
		const testCase = taskQuality.test_cases[0]!
		const commandQuality: TaskQuality = {
			...taskQuality,
			test_cases: [
				{
					...testCase,
					latest_result: {
						...testCase.latest_result!, evidence_kind: 'COMMAND', summary: 'vitest 通过',
						command: 'pnpm test', exit_code: 0, artifact_ref: 'runs/run-1/stdout.log',
					},
				},
				{ ...testCase, id: 'test-2', title: '浏览器人工检查', latest_result: undefined },
			],
		}
		render(<TaskQualityPanel quality={commandQuality} loading={false} error={null} busy={false} onReload={() => undefined} onCreateEstimate={vi.fn()} onCreateTestCase={vi.fn()} onRecordResult={vi.fn()} />)

		expect(screen.getByText('需要你处理 1 项')).toBeInTheDocument()
		expect(screen.getByText('已验证 1 项')).toBeInTheDocument()
		expect(screen.getByText('vitest 通过 · 命令已验证 · exit 0')).toBeInTheDocument()
		expect(screen.queryByRole('button', { name: '人工确认浏览器创建流程通过' })).not.toBeInTheDocument()
	})
})
