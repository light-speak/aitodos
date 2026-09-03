import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { RunEvent, RunStatus, Task } from '../types'
import { TaskRunsPanel } from './TaskRunsPanel'

class FakeEventSource {
	static instances: FakeEventSource[] = []
	readonly url: string
	onopen: ((event: Event) => void) | null = null
	onerror: ((event: Event) => void) | null = null
	onmessage: ((event: MessageEvent<string>) => void) | null = null
	readonly close = vi.fn()

	constructor(url: string | URL) {
		this.url = String(url)
		FakeEventSource.instances.push(this)
	}

	emit(event: RunEvent) {
		this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(event) }))
	}
}

describe('TaskRunsPanel SSE', () => {
	afterEach(() => {
		cleanup()
		FakeEventSource.instances = []
		vi.unstubAllGlobals()
	})

	it('活跃 Run 消费递增事件、忽略重复 sequence 并在终态关闭', async () => {
		let status: RunStatus = 'RUNNING'
		const fetchMock = vi.fn<typeof fetch>(() => Promise.resolve(Response.json([{
			id: 'run-live', purpose: 'IMPLEMENTATION', task_id: 'task-1', status,
			profile_revision_id: 'revision-1', subject_version: 1, lease_generation: 1,
			lease_expires_at: '2026-08-21T00:10:00Z', queued_at: '2026-08-21T00:00:00Z',
			claimed_at: '2026-08-21T00:00:01Z', started_at: '2026-08-21T00:00:02Z',
			created_at: '2026-08-21T00:00:00Z', updated_at: '2026-08-21T00:00:02Z',
		}])))
		vi.stubGlobal('fetch', fetchMock)
		vi.stubGlobal('EventSource', FakeEventSource)
		render(<TaskRunsPanel task={taskFixture} onTaskUpdated={vi.fn()} />)

		expect(await screen.findByText('执行中')).toBeInTheDocument()
		await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1))
		const source = FakeEventSource.instances[0]
		if (!source) throw new Error('EventSource was not created')
		const event = runEvent(1, 'RUNNING')
		source.emit(event)
		await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
		source.emit(event)
		await new Promise((resolve) => setTimeout(resolve, 0))
		expect(fetchMock).toHaveBeenCalledTimes(2)

		status = 'SUCCEEDED'
		source.emit(runEvent(2, 'SUCCEEDED'))
		await waitFor(() => expect(screen.getByText('成功')).toBeInTheDocument())
		expect(source.close).toHaveBeenCalled()
	})

	it('展示 Run 召回经验并记录人工结果', async () => {
		const run = {
			id: 'run-memory', purpose: 'IMPLEMENTATION', task_id: 'task-1', status: 'SUCCEEDED',
			profile_revision_id: 'revision-1', subject_version: 1, lease_generation: 1,
			lease_expires_at: '2026-08-21T00:10:00Z', queued_at: '2026-08-21T00:00:00Z',
			claimed_at: '2026-08-21T00:00:01Z', started_at: '2026-08-21T00:00:02Z',
			finished_at: '2026-08-21T00:00:04Z', created_at: '2026-08-21T00:00:00Z', updated_at: '2026-08-21T00:00:04Z',
		}
		const fetchMock = vi.fn<typeof fetch>((input, init) => {
			const path = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url
			if (path === '/api/tasks/task-1/runs') return Promise.resolve(Response.json([run]))
			if (path === '/api/runs/run-memory') return Promise.resolve(Response.json({ run, artifacts: [] }))
			if (path.endsWith('/summary')) return Promise.resolve(Response.json({ code: 'RUN_SUMMARY_NOT_FOUND', message: 'missing' }, { status: 404 }))
			if (path.endsWith('/mcp-calls') || path.endsWith('/resources')) return Promise.resolve(Response.json([]))
			if (path.endsWith('/experiences')) return Promise.resolve(Response.json([{
				recall_id: 'recall-1', rank: 1, outcome: 'PENDING', recalled_at: '2026-08-21T00:00:02Z',
				score: { relevance_score: 0.9, utility_score: 0.8, scope_score: 1, freshness_score: 1, final_score: 0.88 },
				experience: {
					id: 'experience-1', key: 'EXP-1', task_id: 'task-1', title: '先跑状态机测试', summary: '修改前后都运行表驱动测试',
					guidance: 'go test', applicability: '状态迁移', project_wide: true, status: 'ACTIVE', pinned: false,
					verification_count: 1, successful_applications: 0, failed_applications: 0, recall_count: 1,
					created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
				},
			}]))
			if (path === '/api/experience-recalls/recall-1/outcome' && init?.method === 'POST') return Promise.resolve(new Response(null, { status: 204 }))
			return Promise.reject(new Error(`unexpected request: ${path}`))
		})
		vi.stubGlobal('fetch', fetchMock)
		render(<TaskRunsPanel task={{ ...taskFixture, status: 'REVIEW' }} onTaskUpdated={vi.fn()} />)
		await userEvent.click(await screen.findByRole('button', { name: '查看 Run run-memory' }))
		expect(await screen.findByText('先跑状态机测试')).toBeInTheDocument()
		await userEvent.click(screen.getByRole('button', { name: '有帮助' }))
		await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/experience-recalls/recall-1/outcome', expect.objectContaining({ method: 'POST' })))
	})
})

const taskFixture: Task = {
	id: 'task-1', key: 'ATS-1', title: '实时执行', title_source: 'HUMAN', title_locked: true,
	description: '', acceptance_criteria: '', status: 'RUNNING', priority: 2,
	assessment_input_version: 1, version: 2,
	created_at: '2026-08-21T00:00:00Z', updated_at: '2026-08-21T00:00:02Z',
}

function runEvent(sequence: number, to: RunStatus): RunEvent {
	return {
		id: `event-${sequence}`, run_id: 'run-live', sequence, type: 'RUN_STATUS_CHANGED',
		payload: { schema_version: 1, from: 'RUNNING', to }, occurred_at: '2026-08-21T00:00:03Z',
	}
}
