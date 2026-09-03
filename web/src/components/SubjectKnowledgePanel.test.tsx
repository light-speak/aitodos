import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { SubjectKnowledgePanel } from './SubjectKnowledgePanel'

afterEach(() => {
	cleanup()
	vi.unstubAllGlobals()
})

it('展示 Task 的标签、有效决策和最新 CI 快照', async () => {
	const label = { id: 'label-1', name: '安全', color: '#ef4444', created_at: '2026-09-02T00:00:00Z' }
	const fetchMock = vi.fn<typeof fetch>((input) => {
		const path = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url
		if (path === '/api/labels' || path.endsWith('/labels')) return Promise.resolve(Response.json([label]))
		if (path.endsWith('/decisions')) return Promise.resolve(Response.json([{
			id: 'decision-1', key: 'DEC-1', task_id: 'task-1', title: '密码只存摘要',
			content: '不得保存明文密码。', status: 'ACTIVE', created_at: '2026-09-02T00:00:00Z',
		}]))
		if (path.includes('/experiences')) return Promise.resolve(Response.json([{
			id: 'experience-1', key: 'EXP-1', task_id: 'task-1', title: '先跑安全测试',
			summary: '认证改动先运行回归', guidance: '运行 go test', applicability: '修改认证模块',
			project_wide: true, status: 'ACTIVE', pinned: false, verification_count: 1,
			successful_applications: 2, failed_applications: 0, recall_count: 3,
			created_at: '2026-09-02T00:00:00Z', updated_at: '2026-09-02T00:00:00Z',
		}]))
		if (path.endsWith('/ci-snapshots')) return Promise.resolve(Response.json([{
			id: 'ci-1', task_id: 'task-1', provider: 'GitHub Actions', commit_sha: 'abcdef123456',
			state: 'PASSED', checks: [], observed_at: '2026-09-02T00:00:00Z', created_at: '2026-09-02T00:00:00Z',
		}]))
		return Promise.reject(new Error(`unexpected request: ${path}`))
	})
	vi.stubGlobal('fetch', fetchMock)
	render(<SubjectKnowledgePanel kind="tasks" id="task-1" />)

	expect(await screen.findByRole('button', { name: '安全' })).toHaveAttribute('aria-pressed', 'true')
	expect(await screen.findByText('密码只存摘要')).toBeInTheDocument()
	expect(await screen.findByText('先跑安全测试')).toBeInTheDocument()
	expect(await screen.findByText(/召回 3 次/)).toBeInTheDocument()
	expect(await screen.findByText(/GitHub Actions/)).toBeInTheDocument()
})

it('允许人类记录项目范围经验', async () => {
	const fetchMock = vi.fn<typeof fetch>((input, init) => {
		const path = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url
		if (path.endsWith('/experiences') && init?.method === 'POST') return Promise.resolve(Response.json({
			id: 'experience-2', key: 'EXP-2', topic_id: 'topic-1', title: '先验证迁移', summary: '迁移前备份数据库',
			guidance: '运行 doctor 后再迁移', applicability: 'Schema 升级', project_wide: true,
			status: 'ACTIVE', pinned: false, verification_count: 1, successful_applications: 0,
			failed_applications: 0, recall_count: 0, created_at: '2026-09-02T00:00:00Z', updated_at: '2026-09-02T00:00:00Z',
		}))
		if (path === '/api/labels' || path.endsWith('/labels') || path.endsWith('/decisions') || path.includes('/experiences')) {
			return Promise.resolve(Response.json([]))
		}
		return Promise.reject(new Error(`unexpected request: ${path}`))
	})
	vi.stubGlobal('fetch', fetchMock)
	render(<SubjectKnowledgePanel kind="topics" id="topic-1" />)
	await userEvent.click(await screen.findByRole('button', { name: '记录经验' }))
	await userEvent.type(screen.getByRole('textbox', { name: '经验标题' }), '先验证迁移')
	await userEvent.type(screen.getByRole('textbox', { name: '经验摘要' }), '迁移前备份数据库')
	await userEvent.type(screen.getByRole('textbox', { name: '经验做法' }), '运行 doctor 后再迁移')
	await userEvent.type(screen.getByRole('textbox', { name: '适用条件' }), 'Schema 升级')
	await userEvent.click(screen.getByRole('checkbox', { name: '允许项目内其他 Topic 和 Task 召回' }))
	await userEvent.click(screen.getByRole('button', { name: '保存已验证经验' }))
	const request = fetchMock.mock.calls.find(([input]) => input === '/api/topics/topic-1/experiences')
	expect(request?.[1]?.method).toBe('POST')
	const body = request?.[1]?.body
	if (typeof body !== 'string') throw new Error('experience request body is not JSON')
	expect(body).toContain('"project_wide":true')
})

it('允许人类确认 Agent 产生的经验候选', async () => {
	const candidate = {
		id: 'experience-candidate', key: 'EXP-CAND', task_id: 'task-1', title: '先验证退出码',
		summary: '只使用真实命令事件', guidance: '匹配命令和退出码', applicability: '记录测试证据时',
		project_wide: false, status: 'CANDIDATE', pinned: false, verification_count: 0,
		successful_applications: 0, failed_applications: 0, recall_count: 0, source_run_id: 'run-1',
		created_at: '2026-09-03T00:00:00Z', updated_at: '2026-09-03T00:00:00Z',
	}
	const fetchMock = vi.fn<typeof fetch>((input, init) => {
		const path = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url
		if (path === '/api/experiences/experience-candidate/confirm' && init?.method === 'POST') {
			return Promise.resolve(Response.json({ ...candidate, status: 'ACTIVE', verification_count: 1 }))
		}
		if (path.endsWith('/experiences?include_inactive=true')) return Promise.resolve(Response.json([candidate]))
		if (path === '/api/labels' || path.endsWith('/labels') || path.endsWith('/decisions') || path.endsWith('/ci-snapshots')) {
			return Promise.resolve(Response.json([]))
		}
		return Promise.reject(new Error(`unexpected request: ${path}`))
	})
	vi.stubGlobal('fetch', fetchMock)
	render(<SubjectKnowledgePanel kind="tasks" id="task-1" />)

	expect(await screen.findByText('等待人工确认，不会进入 Agent 上下文')).toBeInTheDocument()
	await userEvent.click(screen.getByRole('button', { name: '确认采用先验证退出码' }))
	expect(fetchMock).toHaveBeenCalledWith('/api/experiences/experience-candidate/confirm', expect.objectContaining({ method: 'POST' }))
})
