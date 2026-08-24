import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { ApprovalRequest } from '../../types'
import { useBrowserNotifications } from './useBrowserNotifications'

vi.mock('../../api/client', async (loadOriginal) => {
	const original = await loadOriginal<typeof import('../../api/client')>()
	return { ...original, getRuns: vi.fn().mockResolvedValue({ items: [] }) }
})

const createdNotifications: Array<{ title: string; options?: NotificationOptions }> = []
const requestPermission = vi.fn<() => Promise<NotificationPermission>>()

class FakeNotification {
	static permission: NotificationPermission = 'default'
	static requestPermission = requestPermission
	onclick: (() => void) | null = null

	constructor(title: string, options?: NotificationOptions) {
		createdNotifications.push({ title, options })
	}
}

function Harness({ approvals }: { approvals: ApprovalRequest[] }) {
	const notifications = useBrowserNotifications('/project/a', true, approvals, [])
	return <button type="button" onClick={() => { void notifications.toggle() }}>{notifications.label}</button>
}

describe('useBrowserNotifications', () => {
	beforeEach(() => {
		createdNotifications.length = 0
		requestPermission.mockReset().mockImplementation(() => {
			FakeNotification.permission = 'granted'
			return Promise.resolve('granted')
		})
		FakeNotification.permission = 'default'
		vi.stubGlobal('Notification', FakeNotification)
		const values = new Map<string, string>()
		vi.stubGlobal('localStorage', {
			getItem: (key: string) => values.get(key) ?? null,
			setItem: (key: string, value: string) => values.set(key, value),
			clear: () => values.clear(),
		})
		localStorage.clear()
		Object.defineProperty(document, 'hidden', { configurable: true, value: true })
	})

	it('只在用户显式开启后通知新增权限请求，并按 ID 去重', async () => {
		const user = userEvent.setup()
		const { rerender } = render(<Harness approvals={[]} />)
		expect(requestPermission).not.toHaveBeenCalled()

		await user.click(screen.getByRole('button', { name: '开启桌面提醒' }))
		await waitFor(() => expect(screen.getByRole('button', { name: '桌面提醒已开启' })).toBeInTheDocument())
		const approval = {
			id: 'approval-1', run_id: 'run-1', task_id: 'task-1', kind: 'COMMAND', status: 'OPEN',
			reason: '需要检查状态', command: 'git status', available_decisions: ['ACCEPT_ONCE'],
			version: 1, created_at: '2026-08-21T00:00:00Z', updated_at: '2026-08-21T00:00:00Z',
		} satisfies ApprovalRequest
		rerender(<Harness approvals={[approval]} />)
		await waitFor(() => expect(createdNotifications).toHaveLength(1))
		expect(createdNotifications[0]?.title).toBe('Agent 等待权限')
		rerender(<Harness approvals={[approval]} />)
		await new Promise((resolve) => window.setTimeout(resolve, 0))
		expect(createdNotifications).toHaveLength(1)
	})

	it('用户拒绝权限后立即显示浏览器已禁用', async () => {
		requestPermission.mockImplementation(() => {
			FakeNotification.permission = 'denied'
			return Promise.resolve('denied')
		})
		render(<Harness approvals={[]} />)

		await userEvent.click(screen.getByRole('button', { name: '开启桌面提醒' }))

		await waitFor(() => expect(screen.getByRole('button', { name: '桌面提醒已被浏览器禁用' })).toBeInTheDocument())
	})
})
