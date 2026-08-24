import { cleanup, render, screen, waitFor } from '@testing-library/react'
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
