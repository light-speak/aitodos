import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import type { Task } from '../types'
import { TaskChangesPanel } from './TaskChangesPanel'

const task: Task = {
	id: 'task-1', key: 'ATS-001', title: '展示 Diff', description: '', acceptance_criteria: '',
	title_source: 'HUMAN', title_locked: true, assessment_input_version: 1,
	status: 'READY', priority: 0, version: 2, created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
}

afterEach(() => vi.unstubAllGlobals())

it('默认只展示文件摘要，点击后按需读取 Diff，并可提交人工验收', async () => {
	const updated = { ...task, status: 'REVIEW' as const, version: 3 }
	const fetchMock = vi.fn<typeof fetch>((input, init) => {
		const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
		if (url === '/api/tasks/task-1/reviews') return Promise.resolve(Response.json([]))
		if (url === '/api/tasks/task-1/changes') return Promise.resolve(Response.json({
			base_commit_sha: 'base', head_sha: 'head', dirty: true, file_count: 1, additions: 2, deletions: 1,
			files: [{ path: 'internal/authn/authn.go', status: 'MODIFIED', additions: 2, deletions: 1, binary: false }],
		}))
		if (url === '/api/tasks/task-1/changes/file?path=internal%2Fauthn%2Fauthn.go') return Promise.resolve(Response.json({
			path: 'internal/authn/authn.go', patch: 'diff --git a/internal/authn/authn.go b/internal/authn/authn.go\n--- a/internal/authn/authn.go\n+++ b/internal/authn/authn.go\n@@ -1,2 +1,2 @@\n-package old\n+package authn\n+var count = 34', truncated: false, binary: false,
		}))
		if (url === '/api/tasks/task-1/submit-review' && init?.method === 'POST') return Promise.resolve(Response.json(updated))
		return Promise.resolve(Response.json({ error: { code: 'UNEXPECTED', message: url } }, { status: 500 }))
	})
	vi.stubGlobal('fetch', fetchMock)
	const onTaskUpdated = vi.fn()
	render(<TaskChangesPanel task={task} hasWorkspace onTaskUpdated={onTaskUpdated} onWorkspaceChanged={vi.fn()} />)

	expect(await screen.findByText('1 个文件')).toBeInTheDocument()
	const fileButton = screen.getByRole('button', { name: /internal\/authn\/authn.go/ })
	await userEvent.click(fileButton)
	const addedLine = await screen.findByTestId('diff-addition-5')
	expect(addedLine).toHaveTextContent('+package authn')
	expect(addedLine).toHaveClass('bg-emerald-950/35')
	expect(screen.getByText('Go · Unified diff')).toBeInTheDocument()
	expect(screen.getAllByText('package')[0]).toHaveClass('text-violet-300')
	expect(fileButton).toHaveAttribute('aria-expanded', 'true')

	await userEvent.click(fileButton)
	expect(screen.queryByText('Go · Unified diff')).not.toBeInTheDocument()
	expect(fileButton).toHaveAttribute('aria-expanded', 'false')
	await userEvent.click(screen.getByRole('button', { name: '提交人工验收' }))
	expect(onTaskUpdated).toHaveBeenCalledWith(updated)
})

it('验收通过会把脏 Workspace 的 Commit 交给后端自动处理', async () => {
	const reviewTask = { ...task, status: 'REVIEW' as const, version: 3 }
	const accepted = { ...reviewTask, status: 'ACCEPTED' as const, version: 4 }
	const fetchMock = vi.fn<typeof fetch>((input, init) => {
		const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
		if (url === '/api/tasks/task-1/reviews' && (init?.method ?? 'GET') === 'GET') return Promise.resolve(Response.json([]))
		if (url === '/api/tasks/task-1/changes') return Promise.resolve(Response.json({
			base_commit_sha: 'base', head_sha: 'head', dirty: true, file_count: 1, additions: 1, deletions: 0,
			files: [{ path: 'main.go', status: 'MODIFIED', additions: 1, deletions: 0, binary: false }],
		}))
		if (url === '/api/tasks/task-1/reviews' && init?.method === 'POST') return Promise.resolve(Response.json({
			task: accepted,
			review: { id: 'review-1', task_id: 'task-1', decision: 'ACCEPTED', comment: '', commit_sha: 'commit', created_at: '2026-08-20T00:00:00Z' },
		}))
		return Promise.resolve(Response.json({ error: { code: 'UNEXPECTED', message: url } }, { status: 500 }))
	})
	vi.stubGlobal('fetch', fetchMock)
	const onTaskUpdated = vi.fn()
	const onWorkspaceChanged = vi.fn()
	render(<TaskChangesPanel task={reviewTask} hasWorkspace onTaskUpdated={onTaskUpdated} onWorkspaceChanged={onWorkspaceChanged} />)

	const accept = await screen.findByRole('button', { name: '验收通过' })
	expect(accept).toBeEnabled()
	await userEvent.click(accept)
	expect(onTaskUpdated).toHaveBeenCalledWith(accepted)
	expect(onWorkspaceChanged).toHaveBeenCalled()
	expect(screen.queryByRole('button', { name: '创建 Commit' })).not.toBeInTheDocument()
})
