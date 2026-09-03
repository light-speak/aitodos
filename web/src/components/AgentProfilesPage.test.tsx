import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { AgentProfile } from '../types'
import { AgentProfilesPage } from './AgentProfilesPage'

const profile: AgentProfile = {
	id: 'profile-implementer', name: '实现 Agent', role: 'IMPLEMENTER',
	created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
	current_revision: {
		id: 'revision-1', profile_id: 'profile-implementer', revision: 1,
		instructions: '实现当前 Task', adapter: 'generic', command: '', args: [], model: '',
		max_input_tokens: 64000, reserved_output_tokens: 12000,
		recent_message_limit: 20, retrieval_limit: 8,
		workspace_policy: 'WRITE_TASK', approval_policy: 'WORKSPACE_WRITE', timeout_seconds: 3600,
		tool_policy: { skills: [], mcp_servers: [] },
		created_at: '2026-08-20T00:00:00Z',
	},
}

const triagerProfile: AgentProfile = {
	...profile,
	id: 'profile-triager',
	name: '任务评估 Agent',
	role: 'TRIAGER',
	current_revision: {
		...profile.current_revision,
		id: 'revision-triager-1',
		profile_id: 'profile-triager',
		workspace_policy: 'NONE',
		approval_policy: 'READ_ONLY',
	},
}

describe('AgentProfilesPage', () => {
	it('可以一次配置所有未配置 Agent', async () => {
		const user = userEvent.setup()
		const onConfigureDefaults = vi.fn().mockResolvedValue(undefined)
		render(<AgentProfilesPage profiles={[profile, triagerProfile]} capabilities={{ skills: [], mcp_servers: [] }} loading={false} error={null} saving={false} onReload={() => undefined} onSave={vi.fn()} onConfigureDefaults={onConfigureDefaults} />)

		expect(screen.getByText('还有 2 个 Agent 未配置')).toBeInTheDocument()
		await user.click(screen.getByRole('button', { name: '一键配置 Codex' }))
		expect(onConfigureDefaults).toHaveBeenCalledOnce()
	})
	it('将上下文预算作为系统策略而不是人工配置项', async () => {
		const user = userEvent.setup()
		render(<AgentProfilesPage profiles={[profile]} capabilities={{ skills: [], mcp_servers: [] }} loading={false} error={null} saving={false} onReload={() => undefined} onSave={vi.fn()} />)
		await user.click(screen.getByRole('button', { name: '配置实现 Agent' }))
		await user.click(screen.getByText('高级配置'))
		expect(screen.queryByLabelText('输入预算')).not.toBeInTheDocument()
		expect(screen.queryByLabelText('输出预留')).not.toBeInTheDocument()
		expect(screen.getByText(/系统会完整保留当前任务/)).toBeInTheDocument()
	})
	it('显示未配置状态并将编辑保存为新修订', async () => {
		const user = userEvent.setup()
		const onSave = vi.fn().mockResolvedValue(undefined)
		render(<AgentProfilesPage profiles={[profile]} capabilities={{ skills: [], mcp_servers: [] }} loading={false} error={null} saving={false} onReload={() => undefined} onSave={onSave} />)

		expect(screen.getByText('未配置')).toBeInTheDocument()
		expect(screen.getByText('仅当前 Task Workspace 可写')).toBeInTheDocument()
		await user.click(screen.getByRole('button', { name: '配置实现 Agent' }))
		await user.type(screen.getByLabelText('Agent 命令'), 'codex')
		await user.click(screen.getByRole('button', { name: '保存为 Revision 2' }))

		expect(onSave).toHaveBeenCalledWith('profile-implementer', expect.objectContaining({
			command: 'codex', instructions: '实现当前 Task',
		}))
	})

	it('为任务评估 Agent 填入支持面板审批的 Codex App Server 配置', async () => {
		const user = userEvent.setup()
		const onSave = vi.fn().mockResolvedValue(undefined)
		render(<AgentProfilesPage profiles={[triagerProfile]} capabilities={{ skills: [], mcp_servers: [] }} loading={false} error={null} saving={false} onReload={() => undefined} onSave={onSave} />)

		await user.click(screen.getByRole('button', { name: '配置任务评估 Agent' }))
		await user.click(screen.getByRole('button', { name: '填入 Codex 推荐配置' }))
		expect(screen.getByLabelText('模型（可留空）')).toHaveValue('gpt-5.3-codex')
		await user.click(screen.getByText('高级配置'))
		expect(screen.getByLabelText('额外参数（每行一项）')).toHaveValue('')
		await user.click(screen.getByRole('button', { name: '保存为 Revision 2' }))

		expect(onSave).toHaveBeenCalledWith('profile-triager', expect.objectContaining({
			command: 'codex',
			adapter: 'codex-app-server',
			model: 'gpt-5.3-codex',
			args: [],
		}))
	})

	it('为实现 Agent 填入无需人工维护沙箱参数的 App Server 配置', async () => {
		const user = userEvent.setup()
		const onSave = vi.fn().mockResolvedValue(undefined)
		render(<AgentProfilesPage profiles={[profile]} capabilities={{ skills: [], mcp_servers: [] }} loading={false} error={null} saving={false} onReload={() => undefined} onSave={onSave} />)

		await user.click(screen.getByRole('button', { name: '配置实现 Agent' }))
		await user.click(screen.getByRole('button', { name: '填入 Codex 推荐配置' }))
		await user.click(screen.getByText('高级配置'))
		expect(screen.getByLabelText('额外参数（每行一项）')).toHaveValue('')
		expect(screen.getByLabelText('Adapter')).toHaveValue('codex-app-server')
	})

	it('把项目 MCP 和 Tool 白名单保存到新修订', async () => {
		const user = userEvent.setup()
		const onSave = vi.fn().mockResolvedValue(undefined)
		render(<AgentProfilesPage profiles={[profile]} capabilities={{
			skills: [],
			mcp_servers: [{
				id: 'mcp-playwright', name: '浏览器', config_name: 'playwright', enabled: true,
				version: 1, created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
			}],
		}} loading={false} error={null} saving={false} onReload={() => undefined} onSave={onSave} />)

		await user.click(screen.getByRole('button', { name: '配置实现 Agent' }))
		await user.click(screen.getByRole('checkbox', { name: '允许 MCP · 浏览器' }))
		await user.click(screen.getByRole('checkbox', { name: 'MCP · 浏览器 必需' }))
		await user.type(screen.getByLabelText('允许的 Tool（逗号分隔，留空表示该 Server 全部 Tool）'), 'browser_navigate, browser_close')
		await user.click(screen.getByRole('button', { name: '保存为 Revision 2' }))

		expect(onSave).toHaveBeenCalledWith('profile-implementer', expect.objectContaining({
			adapter: 'codex-app-server',
			tool_policy: { skills: [], mcp_servers: [{ server_id: 'mcp-playwright', required: true, enabled_tools: ['browser_navigate', 'browser_close'] }] },
		}))
	})
})
