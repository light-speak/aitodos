import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { ProjectCapabilityCatalog } from '../types'
import { ProjectCapabilitiesPanel } from './ProjectCapabilitiesPanel'

const catalog: ProjectCapabilityCatalog = {
	skills: [{
		id: 'skill-1', name: '发布检查', source_path: '.agents/skills/release',
		content_sha256: '0'.repeat(64), enabled: true, version: 1,
		created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
	}],
	mcp_servers: [{
		id: 'mcp-1', name: '浏览器', config_name: 'playwright', enabled: true, version: 1,
		created_at: '2026-08-20T00:00:00Z', updated_at: '2026-08-20T00:00:00Z',
	}],
}

describe('ProjectCapabilitiesPanel', () => {
	it('展示目录并添加本机 Skill 和 MCP 引用', async () => {
		const user = userEvent.setup()
		const onAddSkill = vi.fn().mockResolvedValue(undefined)
		const onRefreshSkill = vi.fn().mockResolvedValue(undefined)
		const onAddMCPServer = vi.fn().mockResolvedValue(undefined)
		render(<ProjectCapabilitiesPanel catalog={catalog} loading={false} adding={false} error={null}
			onReload={() => undefined} onAddSkill={onAddSkill} onRefreshSkill={onRefreshSkill} onAddMCPServer={onAddMCPServer} />)

		expect(screen.getByText('发布检查')).toBeInTheDocument()
		expect(screen.getByText('playwright')).toBeInTheDocument()
		await user.click(screen.getByRole('button', { name: '重新校验 发布检查' }))
		expect(onRefreshSkill).toHaveBeenCalledWith('skill-1', 1)
		await user.type(screen.getByLabelText('Skill 名称'), '测试规范')
		await user.type(screen.getByLabelText('Skill 路径'), '.agents/skills/testing')
		await user.click(screen.getByRole('button', { name: '添加 Skill' }))
		expect(onAddSkill).toHaveBeenCalledWith({ name: '测试规范', source_path: '.agents/skills/testing' })

		await user.type(screen.getByLabelText('MCP 名称'), '代码搜索')
		await user.type(screen.getByLabelText('Codex MCP 配置名'), 'code-search')
		await user.click(screen.getByRole('button', { name: '添加 MCP' }))
		expect(onAddMCPServer).toHaveBeenCalledWith({ name: '代码搜索', config_name: 'code-search' })
	})
})
