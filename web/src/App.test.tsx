import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import App from './App'

const backlogTask = {
  id: 'task-1',
  key: 'ATS-001',
  title: '实现项目看板',
	title_source: 'HUMAN',
	title_locked: true,
  description: '展示当前项目中的任务',
  acceptance_criteria: '可以查看详情并排队',
  status: 'BACKLOG',
  priority: 10,
	target_branch: 'main',
	assessment_input_version: 1,
	assessment: {
		id: 'assessment-1', task_id: 'task-1', task_assessment_version: 1, revision: 1,
		suggested_title: '实现项目看板', applied_title: '实现项目看板',
		scores: { technical_complexity: 4, requirement_uncertainty: 2, change_scope: 3, validation_burden: 3, human_dependency: 4, risk_and_reversibility: 2 },
		weighted_score: 3.1, complexity: 'C4', autonomy: 'A0', confidence: 0.8,
		rationale: '需要跨模块修改并进行多轮人工验证', assumptions: ['接口保持兼容'],
		split_recommended: true, split_rationale: '建议拆分前后端任务', source_run_id: 'run-triage-1',
		created_at: '2026-08-18T00:00:00Z',
	},
	assessment_stale: false,
  version: 1,
  created_at: '2026-08-18T00:00:00Z',
  updated_at: '2026-08-18T00:00:00Z',
}

const relatedTask = {
  ...backlogTask,
  id: 'task-related',
  key: 'ATS-099',
  title: '补充接口测试',
  priority: 5,
	assessment: undefined,
	assessment_stale: false,
}

const openTopic = {
  id: 'topic-1',
  key: 'TOP-001',
  title: '讨论 Agent 上下文',
  description: '明确 Session 和持久上下文的边界',
  status: 'OPEN',
  version: 1,
  created_at: '2026-08-18T00:00:00Z',
  updated_at: '2026-08-18T00:00:00Z',
}

const topicMessage = {
  id: 'message-1',
  thread_id: 'thread-1',
  sequence: 1,
  author_kind: 'HUMAN',
  content: 'Session 不是事实来源',
  linked_task_ids: [],
  created_at: '2026-08-18T01:00:00Z',
}

const implementerProfile = {
	id: 'profile-implementer', name: '实现 Agent', role: 'IMPLEMENTER',
	created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
	current_revision: {
		id: 'revision-1', profile_id: 'profile-implementer', revision: 1,
		instructions: '实现当前 Task', adapter: 'generic', command: '', args: [], model: '',
		max_input_tokens: 64000, reserved_output_tokens: 12000, recent_message_limit: 20,
		retrieval_limit: 8, workspace_policy: 'WRITE_TASK', approval_policy: 'WORKSPACE_WRITE',
		tool_policy: { skills: [], mcp_servers: [] },
		timeout_seconds: 3600, created_at: '2026-08-20T00:00:00Z',
	},
}

const pendingClarification = {
	id: 'clarification-1', task_id: 'task-1', source_run_id: 'run-1', continuation_purpose: 'IMPLEMENTATION',
	category: 'DECISION', question: '数据库迁移是否兼容旧版本？',
	options: [
		{ id: 'compatible', label: '兼容升级', description: '保留旧数据' },
		{ id: 'fresh', label: '仅新项目', description: '不迁移旧数据' },
	],
	recommended_option_id: 'compatible', allow_custom_answer: true, status: 'OPEN', version: 1,
	created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
}

const failedRun = {
	id: 'run-failed-1', purpose: 'IMPLEMENTATION', task_id: 'task-1', status: 'FAILED',
	profile_revision_id: 'revision-1', subject_version: 1, lease_generation: 1,
	lease_expires_at: '2026-08-20T01:30:00Z', queued_at: '2026-08-20T01:00:00Z',
	claimed_at: '2026-08-20T01:00:01Z', started_at: '2026-08-20T01:00:02Z',
	finished_at: '2026-08-20T01:00:08Z', exit_code: 7,
	failure_kind: 'AGENT_PROCESS', failure_code: 'NON_ZERO_EXIT', failure_message: 'agent failed',
	failure_retryable: false, created_at: '2026-08-20T01:00:00Z', updated_at: '2026-08-20T01:00:08Z',
}

const runningRun = {
	id: 'run-running-1', purpose: 'IMPLEMENTATION', task_id: 'task-1', status: 'RUNNING',
	profile_revision_id: 'revision-1', subject_version: 1, lease_generation: 1,
	lease_expires_at: '2026-08-20T01:30:00Z', queued_at: '2026-08-20T01:00:00Z',
	claimed_at: '2026-08-20T01:00:01Z', started_at: '2026-08-20T01:00:02Z',
	created_at: '2026-08-20T01:00:00Z', updated_at: '2026-08-20T01:00:02Z',
}

const objectiveView = {
	objective: {
		id: 'objective-1', key: 'OBJ-0001', root_topic_id: 'topic-1', status: 'ACTIVE',
		current_revision_id: 'objective-revision-1', max_continuations: 20, continuation_count: 0,
		version: 1, created_at: '2026-09-05T00:00:00Z', updated_at: '2026-09-05T00:00:00Z',
	},
	revision: {
		id: 'objective-revision-1', objective_id: 'objective-1', revision: 1,
		statement: '完成生产级交付', scope: '当前项目', constraints: ['不自动 push'],
		completion_criteria: [{ id: 'criterion-1', description: '全部测试通过' }],
		created_at: '2026-09-05T00:00:00Z',
	},
	progress: { criteria_total: 1, criteria_satisfied: 0, tasks_total: 2, tasks_accepted: 1 },
}

let openClarifications: unknown[] = []
let openApprovals: unknown[] = []
let activeRuns: unknown[] = []
let schedulerError = ''
let currentObjective: unknown = null

const fetchMock = vi.fn<typeof fetch>((input, init) => {
  const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
  const method = init?.method ?? 'GET'

  if (url === '/api/project' && method === 'GET') {
    return Promise.resolve(Response.json({
      name: 'AiTodos',
      root: '/projects/aitodos',
      agent: 'codex',
		workers_enabled: false,
      max_workers: 2,
		scheduler_failures: schedulerError ? 1 : 0,
		scheduler_error: schedulerError,
    }))
  }
	if (url === '/api/objective' && method === 'GET') {
		return Promise.resolve(Response.json(currentObjective))
	}
	if (url === '/api/objectives' && method === 'POST') {
		currentObjective = objectiveView
		return Promise.resolve(Response.json(currentObjective, { status: 201 }))
	}
	if (url === '/api/objectives/objective-1/pause' && method === 'POST') {
		currentObjective = {
			...objectiveView,
			objective: { ...objectiveView.objective, status: 'PAUSED', version: 2 },
		}
		return Promise.resolve(Response.json(currentObjective))
	}
	if (url === '/api/project/workers' && method === 'POST') {
		return Promise.resolve(Response.json({
			name: 'AiTodos', root: '/projects/aitodos', agent: 'codex',
			workers_enabled: true, max_workers: 2,
			scheduler_failures: 0, scheduler_error: '',
		}))
	}
  if (url === '/api/tasks' && method === 'GET') {
    return Promise.resolve(Response.json([backlogTask, relatedTask]))
  }
	if (url === '/api/clarifications' && method === 'GET') {
		return Promise.resolve(Response.json(openClarifications))
	}
	if (url === '/api/approvals' && method === 'GET') {
		return Promise.resolve(Response.json(openApprovals))
	}
	if (url === '/api/approvals/approval-1/decision' && method === 'POST') {
		openApprovals = []
		return Promise.resolve(Response.json({
			id: 'approval-1', run_id: 'run-running-1', task_id: 'task-1', kind: 'NETWORK',
			reason: '读取依赖元数据', host: 'registry.npmjs.org', protocol: 'https',
			available_decisions: ['ACCEPT_ONCE', 'DECLINE', 'CANCEL_RUN'], status: 'RESOLVED',
			decision: 'ACCEPT_ONCE', version: 2, created_at: '2026-08-20T00:00:00Z',
			updated_at: '2026-08-20T00:00:01Z', resolved_at: '2026-08-20T00:00:01Z',
		}))
	}
	if (url === '/api/tasks/task-1/clarifications' && method === 'GET') {
		return Promise.resolve(Response.json(openClarifications))
	}
	if (url === '/api/clarifications/clarification-1/answer' && method === 'POST') {
		openClarifications = []
		openApprovals = []
		return Promise.resolve(Response.json({
			clarification: { ...pendingClarification, status: 'ANSWERED', selected_option_id: 'compatible', version: 2 },
			task: { ...backlogTask, status: 'READY', version: 2 },
		}))
	}
	if (url === '/api/progress' && method === 'GET') {
		return Promise.resolve(Response.json({
			total_tasks: 2, accepted_tasks: 1, strict_percent: 50,
			estimated_tasks: 1, estimate_coverage: 50, total_points: 5,
			remaining_points: 2, forecast_percent: 60,
			required_tests: 1, verified_passed_tests: 1, agent_reported_passed_tests: 0,
		}))
	}
	if (url === '/api/runs/usage' && method === 'GET') {
		return Promise.resolve(Response.json({
			total_runs: 0, runs_with_usage: 0, input_tokens: null, cached_input_tokens: null,
			uncached_input_tokens: null, cache_write_input_tokens: null, output_tokens: null,
			reasoning_output_tokens: null, model_requests: null, peak_input_tokens: null, by_purpose: [],
		}))
	}
	if (url.startsWith('/api/search?') && method === 'GET') {
		return Promise.resolve(Response.json({ items: [{
			document_id: 'MESSAGE:message-1', kind: 'MESSAGE', source_id: 'message-1',
			subject_kind: 'TOPIC', subject_id: 'topic-1', stable_key: 'TOP-001#message-1',
			title: '你的消息', snippet: 'Session 不是事实来源', status: 'HUMAN', current: true,
			updated_at: '2026-08-18T01:00:00Z',
		}] }))
	}
	if (url === '/api/runs?active=true&limit=100' && method === 'GET') {
		return Promise.resolve(Response.json({ items: activeRuns }))
	}
	if (url.startsWith('/api/runs?') && method === 'GET') {
		return Promise.resolve(Response.json({ items: [runningRun] }))
	}
	if (url === '/api/runs/run-running-1/cancel' && method === 'POST') {
		return Promise.resolve(Response.json({
			...runningRun,
			cancel_requested_at: '2026-08-20T01:00:03Z',
			cancel_reason: '停止错误方向',
		}, { status: 202 }))
	}
	if (url === '/api/agent-profiles' && method === 'GET') {
		return Promise.resolve(Response.json([implementerProfile]))
	}
	if (url === '/api/project/capabilities' && method === 'GET') {
		return Promise.resolve(Response.json({ skills: [], mcp_servers: [] }))
	}
	if (url === '/api/agent-profiles/profile-implementer/revisions' && method === 'POST') {
		return Promise.resolve(Response.json({
			...implementerProfile,
			current_revision: { ...implementerProfile.current_revision, revision: 2, command: 'codex' },
		}, { status: 201 }))
	}
  if (url === '/api/topics' && method === 'GET') {
    return Promise.resolve(Response.json([openTopic]))
  }
  if (url === '/api/git' && method === 'GET') {
    return Promise.resolve(Response.json({
		root: '/projects/aitodos',
		git_common_dir: '/projects/aitodos/.git',
		git_version: 'git version 2.50.1',
		default_branch: 'main',
		remote_default_branch: 'main',
      current_branch: 'main',
      head_sha: '1234567890abcdef',
		has_head: true,
      dirty: false,
		upstream: 'origin/main',
		ahead: 1,
		behind: 0,
		user_name: 'linty',
		user_email: 'linty@staticoft.com',
		identity_configured: true,
		branches: [
			{ name: 'main', head_sha: '1234567890abcdef' },
			{ name: 'release/macos', head_sha: 'abcdef1234567890' },
		],
		remotes: [{ name: 'origin', fetch_url: 'https://github.com/light-speak/aitodos.git', push_url: 'git@github.com:light-speak/aitodos.git' }],
    }))
  }
  if (url === '/api/releases' && method === 'GET') {
    return Promise.resolve(Response.json([]))
  }
  if (url === '/api/releases' && method === 'POST') {
    return Promise.resolve(Response.json({
      id: 'release-1',
      version: '1.0.1',
      tag_name: 'v1.0.1',
      source_branch: 'main',
      commit_sha: '1234567890abcdef',
      status: 'TAGGED',
      task_ids: [],
      created_at: '2026-08-19T00:00:00Z',
      updated_at: '2026-08-19T00:00:00Z',
      tagged_at: '2026-08-19T00:00:00Z',
    }, { status: 201 }))
  }
  if (url === '/api/tasks' && method === 'POST') {
    return Promise.resolve(Response.json(
      { ...backlogTask, id: 'task-2', key: 'ATS-002', title: '新增任务', status: 'READY', priority: 2, target_branch: 'release/macos' },
      { status: 201 },
    ))
  }
	if (url === '/api/tasks/task-1/target-branch' && method === 'PUT') {
		return Promise.resolve(Response.json({ ...backlogTask, target_branch: 'release/macos', version: 2 }))
	}
	if (url === '/api/tasks/task-1' && method === 'GET') {
		return Promise.resolve(Response.json({ ...backlogTask, version: 2, current_workspace_id: 'workspace-1' }))
	}
	if (url === '/api/tasks/task-1/title' && method === 'PUT') {
		return Promise.resolve(Response.json({
			...backlogTask,
			title: '实现可筛选项目看板',
			assessment: undefined,
			assessment_stale: undefined,
			version: 2,
		}))
	}
  if (url === '/api/topics' && method === 'POST') {
    return Promise.resolve(Response.json(
      { ...openTopic, id: 'topic-2', key: 'TOP-002', title: '规划搜索能力' },
      { status: 201 },
    ))
  }
  if (url === '/api/topics/topic-1/messages' && method === 'GET') {
    return Promise.resolve(Response.json([topicMessage]))
  }
  if (url === '/api/topics/topic-1/messages' && method === 'POST') {
    return Promise.resolve(Response.json(
      {
        ...topicMessage,
        id: 'message-2',
        sequence: 2,
        content: '批准后的 Plan 才能拆正式 Task',
        linked_task_ids: init?.body === '{"content":"批准后的 Plan 才能拆正式 Task","linked_task_ids":["task-1"]}'
          ? ['task-1']
          : [],
      },
      { status: 201 },
    ))
  }
  if (url === '/api/topics/topic-1/relations' && method === 'GET') {
    return Promise.resolve(Response.json([]))
  }
  if (url === '/api/topics/topic-1/relations' && method === 'POST') {
    return Promise.resolve(new Response(null, { status: 204 }))
  }
  if (url === '/api/tasks/task-1/messages' && method === 'GET') {
    return Promise.resolve(Response.json([]))
  }
	if (url === '/api/tasks/task-1/feedback' && method === 'GET') {
		return Promise.resolve(Response.json([]))
	}
  if (url === '/api/tasks/task-1/workspace' && method === 'GET') {
    return Promise.resolve(Response.json(null))
  }
  if (url === '/api/tasks/task-1/workspace' && method === 'POST') {
    return Promise.resolve(Response.json({
      id: 'workspace-1',
      task_id: 'task-1',
      path: '/projects/aitodos/.ats/worktrees/task-1',
      branch_name: 'aitodos/aitodos/ATS-001-task1',
      target_branch: 'main',
      base_commit_sha: '1234567890abcdef',
      head_sha: '1234567890abcdef',
		has_head: true,
      state: 'READY',
      dirty: false,
      created_at: '2026-08-19T00:00:00Z',
      updated_at: '2026-08-19T00:00:00Z',
      last_verified_at: '2026-08-19T00:00:00Z',
    }))
  }
	if (url === '/api/tasks/task-1/reviews' && method === 'GET') {
		return Promise.resolve(Response.json([]))
	}
	if (url === '/api/tasks/task-1/quality' && method === 'GET') {
		return Promise.resolve(Response.json({ estimate_history: [], test_cases: [] }))
	}
	if (url === '/api/tasks/task-1/runs' && method === 'GET') {
		return Promise.resolve(Response.json([failedRun]))
	}
	if (url === '/api/runs/run-failed-1' && method === 'GET') {
		return Promise.resolve(Response.json({
			run: failedRun,
			artifacts: [{ id: 'artifact-stderr', run_id: 'run-failed-1', kind: 'STDERR', relative_path: 'runs/run-failed-1/stderr.log', sha256: 'abc', size: 15, truncated: false, created_at: '2026-08-20T01:00:08Z' }],
			workspace_snapshot: {
				run_id: 'run-failed-1', workspace_id: 'workspace-1', branch_name: 'aitodos/aitodos/ATS-001-task1',
				target_branch: 'main', base_commit_sha: '1234567890abcdef', head_before: '1234567890abcdef',
				head_after: 'abcdef1234567890', dirty_before: false, dirty_after: true, state_after: 'DIRTY',
				captured_at: '2026-08-20T01:00:08Z',
			},
		}))
	}
	if (url === '/api/runs/run-failed-1/logs?stream=stderr' && method === 'GET') {
		return Promise.resolve(Response.json({ stream: 'stderr', content: 'compile failed\n', size: 15, truncated: false }))
	}
	if (url === '/api/tasks/task-1/assessment' && method === 'GET') {
		return Promise.resolve(Response.json({ current: backlogTask.assessment, history: [backlogTask.assessment], stale: false }))
	}
	if (url === '/api/tasks/task-1/changes' && method === 'GET') {
		return Promise.resolve(Response.json({
			base_commit_sha: '1234567890abcdef', head_sha: '1234567890abcdef', dirty: false,
			file_count: 0, additions: 0, deletions: 0, files: [],
		}))
	}
  if (url === '/api/tasks/task-1/messages' && method === 'POST') {
    return Promise.resolve(Response.json({
      ...topicMessage,
      id: 'task-message-1',
      content: '请同步检查接口返回值',
        linked_task_ids: ['task-related'],
    }, { status: 201 }))
  }
	if (url === '/api/tasks/task-1/feedback' && method === 'POST') {
		return Promise.resolve(Response.json({
			message: {
				...topicMessage, id: 'task-message-1', content: '请同步检查接口返回值', linked_task_ids: ['task-related'],
			},
			feedback: {
				id: 'feedback-1', task_id: 'task-1', source_message_id: 'task-message-1', intent: 'DISCUSS', status: 'QUEUED',
				failure_message: '', created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
			},
		}, { status: 201 }))
	}
  if (url === '/api/tasks/task-1/relations' && method === 'GET') {
    return Promise.resolve(Response.json([]))
  }
  if (url === '/api/tasks/task-1/relations' && method === 'POST') {
    return Promise.resolve(new Response(null, { status: 204 }))
  }
  if (url === '/api/tasks/task-1/topics' && method === 'GET') {
    return Promise.resolve(Response.json([]))
  }
  if (url === '/api/tasks/task-1/topics' && method === 'POST') {
    return Promise.resolve(new Response(null, { status: 204 }))
  }
  return Promise.resolve(Response.json({ error: { code: 'UNEXPECTED_REQUEST', message: url } }, { status: 500 }))
})

describe('App', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock)
    fetchMock.mockClear()
		openClarifications = []
		openApprovals = []
		activeRuns = []
		schedulerError = ''
		currentObjective = null
  })

	it('在顶部暴露 Worker 调度异常', async () => {
		schedulerError = 'database unavailable'
		render(<App />)

		const warning = await screen.findByRole('button', { name: 'Worker 调度异常' })
		expect(warning).toHaveAttribute('title', 'database unavailable')
	})

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('展示 Topic 列表并可切换到任务看板', async () => {
    const user = userEvent.setup()
    render(<App />)

    expect(await screen.findByRole('heading', { name: 'AiTodos' })).toBeInTheDocument()
    expect(screen.getByText('本地项目')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '议题' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Topic 列表' })).toBeInTheDocument()
    expect(screen.getByText('讨论 Agent 上下文')).toBeInTheDocument()
    expect(screen.getByText('/projects/aitodos')).toBeInTheDocument()
    expect(screen.getByText('main @ 12345678')).toBeInTheDocument()
    expect(screen.queryByText('Agent')).not.toBeInTheDocument()

    await user.click(screen.getByRole('tab', { name: /Tasks/ }))

    expect(screen.getByRole('heading', { name: '任务看板' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '待办' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '进行中' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '待验收' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '已完成' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: '已就绪' })).not.toBeInTheDocument()
		expect(screen.getAllByText('C4').length).toBeGreaterThan(1)
		expect(screen.getByText('A0')).toBeInTheDocument()
		await user.selectOptions(screen.getByLabelText('复杂度筛选'), 'C4')
		expect(screen.getByRole('button', { name: /ATS-001.*实现项目看板/ })).toBeInTheDocument()
		expect(screen.queryByRole('button', { name: /ATS-099.*补充接口测试/ })).not.toBeInTheDocument()
		await user.selectOptions(screen.getByLabelText('复杂度筛选'), 'ALL')

    await user.click(screen.getByRole('button', { name: /ATS-001.*实现项目看板/ }))

    const dialog = screen.getByRole('dialog', { name: '实现项目看板' })
		expect(dialog).toHaveClass('data-[side=right]:sm:w-[min(88rem,calc(100vw-3rem))]')
		const definition = within(dialog).getByRole('region', { name: '需求与讨论' })
		const execution = within(dialog).getByRole('region', { name: '执行现场' })
		expect(within(definition).getByRole('textbox', { name: '发表消息' })).toBeInTheDocument()
		expect(within(execution).getByRole('region', { name: 'Agent Runs' })).toBeInTheDocument()
    expect(within(dialog).getByText('待完善')).toBeInTheDocument()
    expect(within(dialog).getByText('展示当前项目中的任务')).toBeInTheDocument()
    expect(within(dialog).getByText('可以查看详情并排队')).toBeInTheDocument()
		expect(within(dialog).getByText('需要跨模块修改并进行多轮人工验证')).toBeInTheDocument()
		await user.click(within(dialog).getByRole('button', { name: '编辑并锁定标题' }))
		const titleDialog = screen.getByRole('dialog', { name: '编辑 Task 标题' })
		await user.clear(within(titleDialog).getByLabelText('Task 标题'))
		await user.type(within(titleDialog).getByLabelText('Task 标题'), '实现可筛选项目看板')
		await user.click(within(titleDialog).getByRole('button', { name: '保存并锁定' }))
		expect(await within(dialog).findByRole('heading', { name: '实现可筛选项目看板' })).toBeInTheDocument()
		await user.click(within(dialog).getByRole('button', { name: '关闭' }))
		expect(screen.getAllByText('C4').length).toBeGreaterThan(1)
  })

	it('从全局待回答入口选择答案并让 Task 继续排队', async () => {
		openClarifications = [pendingClarification]
		const user = userEvent.setup()
		render(<App />)

		const pendingButton = await screen.findByRole('button', { name: /待处理 1/ })
		await user.click(pendingButton)
		const dialog = screen.getByRole('dialog', { name: '需要你处理' })
		await user.click(within(dialog).getByRole('radio', { name: /兼容升级/ }))
		await user.click(within(dialog).getByRole('button', { name: '回答并继续' }))

		expect(fetchMock).toHaveBeenCalledWith('/api/clarifications/clarification-1/answer', expect.objectContaining({
			method: 'POST',
			body: '{"selected_option_id":"compatible","custom_answer":"","expected_version":1}',
		}))
		expect(await within(dialog).findByText('当前没有待处理事项。')).toBeInTheDocument()
	})

	it('待处理入口一次只展示一条问题并支持前后翻页', async () => {
		openClarifications = [
			pendingClarification,
			{
				...pendingClarification,
				id: 'clarification-2',
				task_id: 'task-related',
				question: '是否需要兼容旧版浏览器？',
			},
		]
		const user = userEvent.setup()
		render(<App />)

		await user.click(await screen.findByRole('button', { name: /待处理 2/ }))
		const dialog = screen.getByRole('dialog', { name: '需要你处理' })
		expect(within(dialog).getByText('1 / 2')).toBeInTheDocument()
		expect(within(dialog).getByText(pendingClarification.question)).toBeInTheDocument()
		expect(within(dialog).queryByText('是否需要兼容旧版浏览器？')).not.toBeInTheDocument()
		expect(within(dialog).getByRole('button', { name: '上一条' })).toBeDisabled()

		await user.click(within(dialog).getByRole('button', { name: '下一条' }))
		expect(within(dialog).getByText('2 / 2')).toBeInTheDocument()
		expect(within(dialog).getByText('是否需要兼容旧版浏览器？')).toBeInTheDocument()
		expect(within(dialog).queryByText(pendingClarification.question)).not.toBeInTheDocument()
		expect(within(dialog).getByRole('button', { name: '下一条' })).toBeDisabled()

		await user.click(within(dialog).getByRole('button', { name: '上一条' }))
		expect(within(dialog).getByText(pendingClarification.question)).toBeInTheDocument()
	})

	it('从统一待处理入口查看并批准结构化网络权限', async () => {
		openApprovals = [{
			id: 'approval-1', run_id: 'run-running-1', task_id: 'task-1', kind: 'NETWORK',
			reason: '读取依赖元数据', host: 'registry.npmjs.org', protocol: 'https',
			available_decisions: ['ACCEPT_ONCE', 'DECLINE', 'CANCEL_RUN'], status: 'OPEN', version: 1,
			created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
		}]
		const user = userEvent.setup()
		render(<App />)

		await user.click(await screen.findByRole('button', { name: /待处理 1/ }))
		const dialog = screen.getByRole('dialog', { name: '需要你处理' })
		expect(within(dialog).getByText('访问 https://registry.npmjs.org')).toBeInTheDocument()
		await user.click(within(dialog).getByRole('button', { name: '仅本次允许' }))

		expect(fetchMock).toHaveBeenCalledWith('/api/approvals/approval-1/decision', expect.objectContaining({
			method: 'POST', body: '{"decision":"ACCEPT_ONCE","expected_version":1}',
		}))
		expect(await within(dialog).findByText('当前没有待处理事项。')).toBeInTheDocument()
	})

  it('从仓库分支创建绑定固定 Commit 的本地 Release Tag', async () => {
    const user = userEvent.setup()
    render(<App />)

    await screen.findByText('main @ 12345678')
    await user.click(screen.getByRole('button', { name: 'Releases' }))
    const dialog = screen.getByRole('dialog', { name: 'Releases' })
    expect(within(dialog).getByText(/只包含来源分支上已经提交的内容/)).toBeInTheDocument()

    await user.type(within(dialog).getByLabelText('版本'), '1.0.1')
    await user.click(within(dialog).getByRole('button', { name: '创建本地 Tag' }))

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/releases',
      expect.objectContaining({
        method: 'POST',
        body: '{"version":"1.0.1","source_branch":"main","task_ids":[]}',
      }),
    )
    expect(await within(dialog).findByText('v1.0.1')).toBeInTheDocument()
    expect(within(dialog).getAllByText(/12345678/).length).toBeGreaterThan(0)
  })

	it('从顶部 Git 状态查看本地仓库与跟踪信息', async () => {
		const user = userEvent.setup()
		render(<App />)

		await user.click(await screen.findByRole('button', { name: 'Git 仓库信息' }))
		const dialog = screen.getByRole('dialog', { name: 'Git 仓库' })
		expect(within(dialog).getByText('/projects/aitodos/.git')).toBeInTheDocument()
		expect(within(dialog).getByText('origin/main')).toBeInTheDocument()
		expect(within(dialog).getByText('领先 1 · 落后 0')).toBeInTheDocument()
		expect(within(dialog).getByText('https://github.com/light-speak/aitodos.git')).toBeInTheDocument()
		expect(within(dialog).getByText('linty <linty@staticoft.com>')).toBeInTheDocument()
	})

  it('Task Workspace 由 Worker 领取时自动准备，不暴露手动创建入口', async () => {
    const user = userEvent.setup()
    render(<App />)

    await user.click(await screen.findByRole('tab', { name: /Tasks/ }))
    await user.click(screen.getByRole('button', { name: /ATS-001.*实现项目看板/ }))
    const dialog = await screen.findByRole('dialog', { name: '实现项目看板' })
	expect(await within(dialog).findByText('Worker 领取 Task 后，系统会自动准备独立 Workspace。')).toBeInTheDocument()
	expect(within(dialog).queryByRole('button', { name: '创建 Task Workspace' })).not.toBeInTheDocument()
	expect(within(dialog).queryByRole('button', { name: '设为可执行' })).not.toBeInTheDocument()
  })

	it('Workspace 创建前可以修改 Task 目标分支', async () => {
		const user = userEvent.setup()
		render(<App />)

		await user.click(await screen.findByRole('tab', { name: /Tasks/ }))
		await user.click(screen.getByRole('button', { name: /ATS-001.*实现项目看板/ }))
		const dialog = await screen.findByRole('dialog', { name: '实现项目看板' })
		await user.selectOptions(within(dialog).getByLabelText('目标分支'), 'release/macos')
		await user.click(within(dialog).getByRole('button', { name: '保存目标分支' }))

		expect(fetchMock).toHaveBeenCalledWith('/api/tasks/task-1/target-branch', expect.objectContaining({
			method: 'PUT', body: '{"target_branch":"release/macos","expected_version":1}',
		}))
		expect(await within(dialog).findByText('Workspace 创建后目标分支将被锁定。')).toBeInTheDocument()
	})

	it('Task 详情按需查看 Run 失败原因和日志', async () => {
		const user = userEvent.setup()
		render(<App />)

		await user.click(await screen.findByRole('tab', { name: /Tasks/ }))
		await user.click(screen.getByRole('button', { name: /ATS-001.*实现项目看板/ }))
		const taskDialog = await screen.findByRole('dialog', { name: '实现项目看板' })
		await user.click(await within(taskDialog).findByRole('button', { name: '查看 Run run-failed-1' }))
		const runDialog = await screen.findByRole('dialog', { name: 'Run 详情' })
		expect(within(runDialog).getByText('NON_ZERO_EXIT')).toBeInTheDocument()
		expect(within(runDialog).getByText('agent failed')).toBeInTheDocument()
		expect(within(runDialog).getByText('12345678 → abcdef12')).toBeInTheDocument()
		await user.click(within(runDialog).getByRole('button', { name: '读取 stderr' }))
		expect(await within(runDialog).findByText('compile failed')).toBeInTheDocument()
	})

	it('从全局 Run 页面确认后取消活跃执行', async () => {
		const user = userEvent.setup()
		render(<App />)

		await user.click(await screen.findByRole('tab', { name: 'Runs' }))
		expect(await screen.findByText('run-running-1')).toBeInTheDocument()
		await user.click(screen.getByRole('button', { name: '取消 Run' }))
		await user.type(screen.getByLabelText('取消原因 run-running-1'), '停止错误方向')
		await user.click(screen.getByRole('button', { name: '确认取消' }))

		expect(fetchMock).toHaveBeenCalledWith('/api/runs/run-running-1/cancel', expect.objectContaining({
			method: 'POST', body: '{"reason":"停止错误方向"}',
		}))
		expect(await screen.findByText('等待取消')).toBeInTheDocument()
	})

  it('按 N 新建事项但不覆盖浏览器的 Ctrl+N', async () => {
    const user = userEvent.setup()
    render(<App />)
    await screen.findByText('讨论 Agent 上下文')

    await user.keyboard('{Control>}n{/Control}')
    expect(screen.queryByRole('dialog', { name: '新建事项' })).not.toBeInTheDocument()

    await user.keyboard('n')
    expect(screen.getByRole('dialog', { name: '新建事项' })).toBeInTheDocument()
  })

	it('从全局搜索结果打开对应 Topic', async () => {
		const user = userEvent.setup()
		render(<App />)
		await screen.findByText('讨论 Agent 上下文')

		await user.click(screen.getByRole('button', { name: '搜索项目' }))
		const searchDialog = await screen.findByRole('dialog', { name: '搜索项目' })
		await user.type(within(searchDialog).getByLabelText('搜索项目内容'), 'Session')
		await user.click(within(searchDialog).getByRole('button', { name: '搜索' }))
		await user.click(await within(searchDialog).findByRole('button', { name: /你的消息/ }))

		expect(await screen.findByRole('dialog', { name: '讨论 Agent 上下文' })).toBeInTheDocument()
	})

  it('输入区域内的 N 不触发新建，Cmd+Enter 可以提交', async () => {
    const user = userEvent.setup()
    render(<App />)
    await screen.findByText('讨论 Agent 上下文')

    await user.keyboard('n')
    const dialog = screen.getByRole('dialog', { name: '新建事项' })
    const textarea = within(dialog).getByLabelText('你想做什么？')
    await user.type(textarea, 'new topic')
    expect(screen.getAllByRole('dialog', { name: '新建事项' })).toHaveLength(1)

    await user.keyboard('{Meta>}{Enter}{/Meta}')
    expect(await screen.findByText('TOP-002')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/topics',
      expect.objectContaining({ method: 'POST', body: '{"title":"","description":"new topic"}' }),
    )
  })

  it('从统一入口创建的 Task 自动等待 Worker 执行', async () => {
    const user = userEvent.setup()
    render(<App />)

    await screen.findByText('讨论 Agent 上下文')
    await user.click(screen.getByRole('button', { name: '新建事项' }))
    await user.click(screen.getByRole('radio', { name: /Task/ }))
		expect(screen.getByLabelText('目标分支')).toHaveValue('main')
		await user.selectOptions(screen.getByLabelText('目标分支'), 'release/macos')
    await user.type(screen.getByLabelText('你想做什么？'), '新增任务')
    await user.click(screen.getByRole('button', { name: '创建事项' }))
    await user.click(screen.getByRole('tab', { name: /Tasks/ }))
    expect(await screen.findByText('ATS-002')).toBeInTheDocument()

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/tasks',
      expect.objectContaining({
        method: 'POST',
        body: '{"title":"","description":"新增任务","acceptance_criteria":"","priority":2,"target_branch":"release/macos"}',
      }),
    )
    const todoColumn = screen.getByRole('region', { name: '待办' })
	expect(await within(todoColumn).findByText('新增任务')).toBeInTheDocument()
	expect(await within(todoColumn).findByText('等待执行')).toBeInTheDocument()
  })

	it('在项目顶部通过全局开关启动 Workers', async () => {
		const user = userEvent.setup()
		render(<App />)
		await screen.findByText('讨论 Agent 上下文')

		await user.click(screen.getByRole('button', { name: '启动 Workers' }))

		expect(fetchMock).toHaveBeenCalledWith('/api/project/workers', expect.objectContaining({
			method: 'POST', body: '{"enabled":true,"max_workers":2}',
		}))
		expect(await screen.findByRole('button', { name: '暂停 Workers' })).toBeInTheDocument()
	})

	it('在项目顶部和 Task 标题栏展示真实 Agent 运行状态', async () => {
		activeRuns = [runningRun]
		const user = userEvent.setup()
		render(<App />)

		const activity = await screen.findByRole('button', { name: '1 个 Agent 运行中' })
		expect(activity).toBeInTheDocument()
		await user.click(screen.getByRole('tab', { name: /Tasks/ }))
		await user.click(screen.getByRole('button', { name: /ATS-001.*实现项目看板/ }))
		const dialog = await screen.findByRole('dialog', { name: '实现项目看板' })
		expect(within(dialog).getByText('实现 Agent 运行中')).toBeInTheDocument()
	})

	it('Workers 启动后自动刷新任务状态', async () => {
		vi.useFakeTimers({ shouldAdvanceTime: true })
		const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
		render(<App />)
		await screen.findByText('讨论 Agent 上下文')
		const initialReads = fetchMock.mock.calls.filter(([input]) => input === '/api/tasks').length

		await user.click(screen.getByRole('button', { name: '启动 Workers' }))
		await screen.findByRole('button', { name: '暂停 Workers' })
		await act(() => vi.advanceTimersByTimeAsync(2_100))

		expect(fetchMock.mock.calls.filter(([input]) => input === '/api/tasks')).toHaveLength(initialReads + 1)
		vi.useRealTimers()
	})

	it('可以在项目顶部配置 Worker 并发数', async () => {
		const user = userEvent.setup()
		render(<App />)
		await screen.findByText('讨论 Agent 上下文')

		await user.click(screen.getByRole('button', { name: '配置 Workers' }))
		const dialog = screen.getByRole('dialog', { name: 'Worker 设置' })
		const input = within(dialog).getByLabelText('最大并发数')
		await user.clear(input)
		await user.type(input, '4')
		await user.click(within(dialog).getByRole('button', { name: '保存设置' }))

		expect(fetchMock).toHaveBeenCalledWith('/api/project/workers', expect.objectContaining({
			method: 'POST', body: '{"enabled":false,"max_workers":4}',
		}))
		expect(screen.queryByRole('dialog', { name: 'Worker 设置' })).not.toBeInTheDocument()
	})

	it('可以查看整体进度与 Agent 配置', async () => {
		const user = userEvent.setup()
		render(<App />)
		await screen.findByText('讨论 Agent 上下文')

		await user.click(screen.getByRole('tab', { name: '整体进度' }))
		expect(await screen.findByRole('region', { name: '项目整体进度' })).toBeInTheDocument()
		expect(screen.getByText('60%')).toBeInTheDocument()

		await user.click(screen.getByRole('tab', { name: 'Agents' }))
		expect(await screen.findByText('实现 Agent')).toBeInTheDocument()
		expect(screen.getByText('未配置')).toBeInTheDocument()
	})

  it('统一入口默认创建 Topic 且不要求标题', async () => {
    const user = userEvent.setup()
    render(<App />)

    await screen.findByText('讨论 Agent 上下文')
    await user.click(screen.getByRole('button', { name: '新建事项' }))
    expect(screen.getByRole('radio', { name: /Topic/ })).toBeChecked()
    await user.type(screen.getByLabelText('你想做什么？'), '讨论全文检索边界')
    await user.click(screen.getByRole('button', { name: '创建事项' }))

    expect(await screen.findByText('TOP-002')).toBeInTheDocument()
    expect(screen.getByText('规划搜索能力')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/topics',
      expect.objectContaining({
        method: 'POST',
        body: '{"title":"","description":"讨论全文检索边界"}',
      }),
    )
  })

  it('打开 Topic 详情并参与持久讨论', async () => {
    const user = userEvent.setup()
    render(<App />)

    await user.click(await screen.findByRole('button', { name: /TOP-001.*讨论 Agent 上下文/ }))

    const dialog = await screen.findByRole('dialog', { name: '讨论 Agent 上下文' })
    expect(dialog).toHaveClass('data-[side=right]:sm:w-[min(88rem,calc(100vw-3rem))]')
    expect(within(dialog).getByText('明确 Session 和持久上下文的边界')).toBeInTheDocument()
    expect(await within(dialog).findByText('Session 不是事实来源')).toBeInTheDocument()

    await user.type(within(dialog).getByLabelText('发表消息'), '批准后的 Plan 才能拆正式 Task')
    await user.click(within(dialog).getByRole('button', { name: '发送消息' }))

    expect(await within(dialog).findByText('批准后的 Plan 才能拆正式 Task')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/topics/topic-1/messages',
      expect.objectContaining({
        method: 'POST',
        body: '{"content":"批准后的 Plan 才能拆正式 Task","linked_task_ids":[]}',
      }),
    )
  })

  it('Topic 可以直接关联 Task，消息也可以引用 Task', async () => {
    const user = userEvent.setup()
    render(<App />)

    await user.click(await screen.findByRole('button', { name: /TOP-001.*讨论 Agent 上下文/ }))
    const dialog = await screen.findByRole('dialog', { name: '讨论 Agent 上下文' })

    await user.click(within(dialog).getByRole('button', { name: '添加关联' }))
    await user.click(within(dialog).getByRole('button', { name: /ATS-001.*实现项目看板/ }))
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/topics/topic-1/relations',
      expect.objectContaining({ method: 'POST', body: '{"task_id":"task-1"}' }),
    )
    expect(within(dialog).getByText('ATS-001')).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: '引用 Task' }))
    await user.click(within(dialog).getByRole('button', { name: /ATS-001.*实现项目看板/ }))
    await user.type(within(dialog).getByLabelText('发表消息'), '批准后的 Plan 才能拆正式 Task')
    await user.keyboard('{Meta>}{Enter}{/Meta}')

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/topics/topic-1/messages',
      expect.objectContaining({
        method: 'POST',
        body: '{"content":"批准后的 Plan 才能拆正式 Task","linked_task_ids":["task-1"]}',
      }),
    )
    expect(await within(dialog).findByRole('button', { name: '打开 ATS-001' })).toBeInTheDocument()
  })

  it('Task 详情可以评论并关联其他 Task', async () => {
    const user = userEvent.setup()
    render(<App />)

    await user.click(await screen.findByRole('tab', { name: /Tasks/ }))
    await user.click(screen.getByRole('button', { name: /ATS-001.*实现项目看板/ }))
    const dialog = await screen.findByRole('dialog', { name: '实现项目看板' })
    expect(within(dialog).getByText('讨论记录')).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: '添加 Topic' }))
    await user.click(within(dialog).getByRole('button', { name: /TOP-001.*讨论 Agent 上下文/ }))
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/tasks/task-1/topics',
      expect.objectContaining({ method: 'POST', body: '{"topic_id":"topic-1"}' }),
    )
    expect(within(dialog).getByText('TOP-001')).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: '引用 Task' }))
    await user.click(within(dialog).getByRole('button', { name: /ATS-099.*补充接口测试/ }))
    await user.type(within(dialog).getByLabelText('发表消息'), '请同步检查接口返回值')
    await user.keyboard('{Control>}{Enter}{/Control}')

    expect(fetchMock).toHaveBeenCalledWith(
	  '/api/tasks/task-1/feedback',
      expect.objectContaining({
        method: 'POST',
		body: '{"content":"请同步检查接口返回值","linked_task_ids":["task-related"],"intent":"DISCUSS","expected_task_version":1}',
      }),
    )
    expect(await within(dialog).findByText('请同步检查接口返回值')).toBeInTheDocument()
	expect(within(dialog).getByText('等待 Agent')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '打开 ATS-099' })).toBeInTheDocument()
  })

  it('统一入口使用中文应用内内容校验', async () => {
    const user = userEvent.setup()
    render(<App />)

    await screen.findByText('讨论 Agent 上下文')
    await user.click(screen.getByRole('button', { name: '新建事项' }))
    await user.click(screen.getByRole('button', { name: '创建事项' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('请输入你想做的内容')
  })
})
