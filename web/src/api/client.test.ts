import { afterEach, describe, expect, it, vi } from 'vitest'

import {
	getProjectProgress,
	getRepositoryInfo,
	getRunDetail,
	getRunLog,
	getRuns,
	getRunUsageSummary,
	getTaskRuns,
	getTaskFeedback,
	getTaskReviews,
	requestRunCancellation,
	requestTopicPlanning,
	retryTask,
} from './client'

describe('getTaskFeedback', () => {
	afterEach(() => vi.unstubAllGlobals())

	it('接受后端省略空 failure_message 的已完成反馈', async () => {
		vi.stubGlobal('fetch', vi.fn<typeof fetch>(() => Promise.resolve(Response.json([{
			id: 'feedback-1', task_id: 'task-1', source_message_id: 'message-1',
			intent: 'REQUEST_CHANGES', status: 'APPLIED',
			created_at: '2026-08-31T00:00:00Z', updated_at: '2026-08-31T00:00:01Z',
		}]))))

		const feedback = await getTaskFeedback('task-1')

		expect(feedback[0]?.status).toBe('APPLIED')
		expect(feedback[0]?.failure_message).toBe('')
	})
})

describe('getTaskReviews', () => {
	afterEach(() => vi.unstubAllGlobals())

	it('接受没有 commit_sha 的驳回记录', async () => {
		vi.stubGlobal('fetch', vi.fn<typeof fetch>(() => Promise.resolve(Response.json([{
			id: 'review-1', task_id: 'task-1', decision: 'REJECTED', comment: '需要修改',
			created_at: '2026-08-31T00:00:00Z',
		}]))))

		const reviews = await getTaskReviews('task-1')

		expect(reviews[0]?.decision).toBe('REJECTED')
		expect(reviews[0]).not.toHaveProperty('commit_sha')
	})
})

describe('getRepositoryInfo', () => {
	afterEach(() => vi.unstubAllGlobals())

	it('解析本地分支、Upstream 和脱敏 Remote 信息', async () => {
		vi.stubGlobal('fetch', vi.fn<typeof fetch>(() => Promise.resolve(Response.json({
			root: '/repo', git_common_dir: '/repo/.git', git_version: 'git version 2.50.1',
			default_branch: 'main', remote_default_branch: 'main', current_branch: 'feature/ui',
			head_sha: '1234567890abcdef', has_head: true, dirty: false,
			upstream: 'origin/feature/ui', ahead: 2, behind: 1,
			user_name: 'linty', user_email: 'linty@staticoft.com', identity_configured: true,
			branches: [{ name: 'main', head_sha: 'abcdef' }],
			remotes: [{ name: 'origin', fetch_url: 'https://example.com/repo.git', push_url: 'git@example.com:repo.git' }],
		}))))

		const repository = await getRepositoryInfo()
		expect(repository.default_branch).toBe('main')
		expect(repository.ahead).toBe(2)
		expect(repository.remotes[0]?.fetch_url).toBe('https://example.com/repo.git')
	})
})

describe('getProjectProgress', () => {
	afterEach(() => vi.unstubAllGlobals())

	it('把尚无估算的 null 预测解析为未知', async () => {
		vi.stubGlobal('fetch', vi.fn<typeof fetch>(() => Promise.resolve(Response.json({
			total_tasks: 2,
			accepted_tasks: 0,
			strict_percent: 0,
			estimated_tasks: 0,
			estimate_coverage: 0,
			total_points: 0,
			remaining_points: 0,
			forecast_percent: null,
			required_tests: 0,
			verified_passed_tests: 0,
			agent_reported_passed_tests: 0,
		}))))

		const progress = await getProjectProgress()
		expect(progress.total_tasks).toBe(2)
		expect(progress).not.toHaveProperty('forecast_percent')
	})
})

describe('requestTopicPlanning', () => {
	afterEach(() => vi.unstubAllGlobals())

	it('携带当前 Topic 版本请求新一轮规划', async () => {
		const fetchMock = vi.fn<typeof fetch>(() => Promise.resolve(Response.json({
			id: 'topic-1', key: 'TOP-1', title: '讨论搜索', description: '', status: 'OPEN', version: 3,
			created_at: '2026-08-23T00:00:00Z', updated_at: '2026-08-23T00:00:01Z',
		})))
		vi.stubGlobal('fetch', fetchMock)

		const updated = await requestTopicPlanning('topic-1', 2)
		expect(updated.version).toBe(3)
		expect(fetchMock).toHaveBeenCalledWith('/api/topics/topic-1/planning', expect.objectContaining({
			method: 'POST', body: JSON.stringify({ expected_version: 2 }),
		}))
	})
})

describe('getRunUsageSummary', () => {
	afterEach(() => vi.unstubAllGlobals())

	it('区分累计输入、缓存输入与未知峰值', async () => {
		vi.stubGlobal('fetch', vi.fn<typeof fetch>(() => Promise.resolve(Response.json({
			total_runs: 2, runs_with_usage: 1,
			input_tokens: 71420, cached_input_tokens: 56320, uncached_input_tokens: 15100,
			cache_write_input_tokens: 0, output_tokens: 916, reasoning_output_tokens: 276,
			model_requests: null, peak_input_tokens: null,
			by_purpose: [{
				purpose: 'IMPLEMENTATION', total_runs: 2, runs_with_usage: 1,
				input_tokens: 71420, cached_input_tokens: 56320, uncached_input_tokens: 15100,
				cache_write_input_tokens: 0, output_tokens: 916, reasoning_output_tokens: 276,
				model_requests: null, peak_input_tokens: null,
			}],
		}))))

		const usage = await getRunUsageSummary()
		expect(usage.input_tokens).toBe(71420)
		expect(usage.peak_input_tokens).toBeUndefined()
		expect(usage.by_purpose[0]?.purpose).toBe('IMPLEMENTATION')
	})
})

describe('Run history', () => {
	afterEach(() => vi.unstubAllGlobals())

	it('解析失败信息、Workspace 快照并按需读取日志', async () => {
		const currentRun = {
			id: 'run-1', purpose: 'IMPLEMENTATION', task_id: 'task-1', status: 'FAILED',
			profile_revision_id: 'revision-1', subject_version: 1, lease_generation: 1,
			lease_expires_at: '2026-08-21T00:10:00Z', queued_at: '2026-08-21T00:00:00Z',
			claimed_at: '2026-08-21T00:00:01Z', started_at: '2026-08-21T00:00:02Z',
			finished_at: '2026-08-21T00:00:09Z', exit_code: 7, failure_kind: 'AGENT_PROCESS',
			failure_code: 'NON_ZERO_EXIT', failure_message: 'agent failed', failure_retryable: false,
			created_at: '2026-08-21T00:00:00Z', updated_at: '2026-08-21T00:00:09Z',
		}
		vi.stubGlobal('fetch', vi.fn<typeof fetch>((input) => {
			const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
			if (url === '/api/tasks/task-1/runs') return Promise.resolve(Response.json([currentRun]))
			if (url === '/api/runs/run-1') return Promise.resolve(Response.json({
				run: currentRun,
				usage: null,
				artifacts: [{ id: 'stderr-1', run_id: 'run-1', kind: 'STDERR', relative_path: 'runs/run-1/stderr.log', sha256: 'abc', size: 15, truncated: false, created_at: '2026-08-21T00:00:09Z' }],
				workspace_snapshot: {
					run_id: 'run-1', workspace_id: 'workspace-1', branch_name: 'aitodos/task-1', target_branch: 'main',
					base_commit_sha: '12345678', head_before: '12345678', head_after: 'abcdef12',
					dirty_before: false, dirty_after: true, state_after: 'DIRTY', captured_at: '2026-08-21T00:00:09Z',
				},
			}))
			return Promise.resolve(Response.json({ stream: 'stderr', content: 'compile failed\n', size: 15, truncated: false }))
		}))

		const runs = await getTaskRuns('task-1')
		const detail = await getRunDetail(runs[0]?.id ?? '')
		expect(detail.run.failure_code).toBe('NON_ZERO_EXIT')
		expect(detail.usage).toBeUndefined()
		expect(detail.workspace_snapshot?.dirty_after).toBe(true)
		expect((await getRunLog('run-1', 'stderr')).content).toContain('compile failed')
	})
})

describe('Run commands and query', () => {
	afterEach(() => vi.unstubAllGlobals())

	it('编码筛选和游标并解析分页结果', async () => {
		const fetchMock = vi.fn<typeof fetch>(() => Promise.resolve(Response.json({
			items: [{
				id: 'run-1', purpose: 'REVIEW', task_id: 'task-1', status: 'RUNNING',
				profile_revision_id: 'revision-1', subject_version: 1, lease_generation: 1,
				lease_expires_at: '2026-08-21T00:10:00Z', queued_at: '2026-08-21T00:00:00Z',
				claimed_at: '2026-08-21T00:00:01Z',
				created_at: '2026-08-21T00:00:00Z', updated_at: '2026-08-21T00:00:01Z',
			}],
			next_cursor: 'cursor-2',
		})))
		vi.stubGlobal('fetch', fetchMock)

		const page = await getRuns({ active: true, purpose: 'REVIEW', limit: 25, cursor: 'cursor 1' })

		expect(fetchMock).toHaveBeenCalledWith(
			'/api/runs?active=true&purpose=REVIEW&limit=25&cursor=cursor+1',
			expect.objectContaining({ signal: undefined }),
		)
		expect(page.next_cursor).toBe('cursor-2')
		expect(page.items[0]?.status).toBe('RUNNING')
	})

	it('取消和重新排队使用显式领域命令', async () => {
		const fetchMock = vi.fn<typeof fetch>((input) => {
			const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
			if (url.endsWith('/cancel')) return Promise.resolve(Response.json({
				id: 'run-1', purpose: 'IMPLEMENTATION', task_id: 'task-1', status: 'RUNNING',
				profile_revision_id: 'revision-1', subject_version: 1, lease_generation: 1,
				lease_expires_at: '2026-08-21T00:10:00Z', queued_at: '2026-08-21T00:00:00Z',
				claimed_at: '2026-08-21T00:00:01Z',
				cancel_requested_at: '2026-08-21T00:00:02Z', cancel_reason: '方向错误',
				created_at: '2026-08-21T00:00:00Z', updated_at: '2026-08-21T00:00:02Z',
			}, { status: 202 }))
			return Promise.resolve(Response.json({
				id: 'task-1', key: 'ATS-001', title: '修复执行', title_source: 'HUMAN', title_locked: true,
				description: '', acceptance_criteria: '', status: 'READY', priority: 1, target_branch: 'main',
				assessment_input_version: 1, version: 3,
				created_at: '2026-08-21T00:00:00Z', updated_at: '2026-08-21T00:01:00Z',
			}))
		})
		vi.stubGlobal('fetch', fetchMock)

		expect((await requestRunCancellation('run-1', '方向错误')).cancel_reason).toBe('方向错误')
		expect((await retryTask('task-1', 2)).status).toBe('READY')
		expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/runs/run-1/cancel', expect.objectContaining({
			method: 'POST', body: '{"reason":"方向错误"}',
		}))
		expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/tasks/task-1/retry', expect.objectContaining({
			method: 'POST', body: '{"expected_version":2}',
		}))
	})
})
