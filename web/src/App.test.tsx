import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import App from './App'

const backlogTask = {
  id: 'task-1',
  key: 'ATS-001',
  title: '实现项目看板',
  description: '展示当前项目中的任务',
  acceptance_criteria: '可以查看详情并排队',
  status: 'BACKLOG',
  priority: 10,
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

const fetchMock = vi.fn<typeof fetch>((input, init) => {
  const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
  const method = init?.method ?? 'GET'

  if (url === '/api/project' && method === 'GET') {
    return Promise.resolve(Response.json({
      name: 'AiTodos',
      root: '/projects/aitodos',
      agent: 'codex',
      max_workers: 2,
    }))
  }
  if (url === '/api/tasks' && method === 'GET') {
    return Promise.resolve(Response.json([backlogTask, relatedTask]))
  }
  if (url === '/api/topics' && method === 'GET') {
    return Promise.resolve(Response.json([openTopic]))
  }
  if (url === '/api/git' && method === 'GET') {
    return Promise.resolve(Response.json({
      current_branch: 'main',
      head_sha: '1234567890abcdef',
      dirty: false,
      branches: [{ name: 'main', head_sha: '1234567890abcdef' }],
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
      { ...backlogTask, id: 'task-2', key: 'ATS-002', title: '新增任务', priority: 0 },
      { status: 201 },
    ))
  }
  if (url === '/api/tasks/task-1/queue' && method === 'POST') {
    return Promise.resolve(Response.json({ ...backlogTask, status: 'READY', version: 2 }))
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
      state: 'READY',
      dirty: false,
      created_at: '2026-08-19T00:00:00Z',
      updated_at: '2026-08-19T00:00:00Z',
      last_verified_at: '2026-08-19T00:00:00Z',
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

    await user.click(screen.getByRole('button', { name: /ATS-001.*实现项目看板/ }))

    const dialog = screen.getByRole('dialog', { name: '实现项目看板' })
    expect(dialog).toHaveClass('data-[side=right]:sm:w-[min(52rem,calc(100vw-3rem))]')
    expect(within(dialog).getByText('待完善')).toBeInTheDocument()
    expect(within(dialog).getByText('展示当前项目中的任务')).toBeInTheDocument()
    expect(within(dialog).getByText('可以查看详情并排队')).toBeInTheDocument()
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

  it('Task 可显式创建长期 Workspace 并显示 Git 身份', async () => {
    const user = userEvent.setup()
    render(<App />)

    await user.click(await screen.findByRole('tab', { name: /Tasks/ }))
    await user.click(screen.getByRole('button', { name: /ATS-001.*实现项目看板/ }))
    const dialog = await screen.findByRole('dialog', { name: '实现项目看板' })
    await user.click(await within(dialog).findByRole('button', { name: '创建 Task Workspace' }))

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/tasks/task-1/workspace',
      expect.objectContaining({ method: 'POST' }),
    )
    expect(await within(dialog).findByText('aitodos/aitodos/ATS-001-task1')).toBeInTheDocument()
    expect(within(dialog).getByText('工作区干净')).toBeInTheDocument()
    expect(within(dialog).queryByText('v1')).not.toBeInTheDocument()
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

  it('从统一入口直接创建 Task 并通过领域命令排队', async () => {
    const user = userEvent.setup()
    render(<App />)

    await screen.findByText('讨论 Agent 上下文')
    await user.click(screen.getByRole('button', { name: '新建事项' }))
    await user.click(screen.getByRole('radio', { name: /Task/ }))
    await user.type(screen.getByLabelText('你想做什么？'), '新增任务')
    await user.click(screen.getByRole('button', { name: '创建事项' }))
    await user.click(screen.getByRole('tab', { name: /Tasks/ }))
    expect(await screen.findByText('ATS-002')).toBeInTheDocument()

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/tasks',
      expect.objectContaining({
        method: 'POST',
        body: '{"title":"","description":"新增任务","acceptance_criteria":"","priority":0}',
      }),
    )

    await user.click(screen.getByRole('button', { name: /ATS-001.*实现项目看板/ }))
    await user.click(screen.getByRole('button', { name: '加入执行队列' }))

    const todoColumn = screen.getByRole('region', { name: '待办' })
    expect(await within(todoColumn).findByText('实现项目看板')).toBeInTheDocument()
    expect(await within(todoColumn).findByText('可执行')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/tasks/task-1/queue',
      expect.objectContaining({ method: 'POST', body: '{"version":1}' }),
    )
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
    expect(dialog).toHaveClass('data-[side=right]:sm:w-[min(52rem,calc(100vw-3rem))]')
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
      '/api/tasks/task-1/messages',
      expect.objectContaining({
        method: 'POST',
        body: '{"content":"请同步检查接口返回值","linked_task_ids":["task-related"]}',
      }),
    )
    expect(await within(dialog).findByText('请同步检查接口返回值')).toBeInTheDocument()
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
