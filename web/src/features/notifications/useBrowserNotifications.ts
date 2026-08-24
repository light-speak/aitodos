import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { getRuns } from '../../api/client'
import type { AgentRun, ApprovalRequest, Clarification, RunStatus } from '../../types'

const terminalStatuses = new Set<RunStatus>(['NEEDS_INPUT', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'TIMED_OUT', 'LOST'])

export function useBrowserNotifications(
	projectRoot: string | undefined,
	workersEnabled: boolean,
	approvals: ApprovalRequest[],
	clarifications: Clarification[],
) {
	const storageKey = projectRoot ? `aitodos.notifications:${projectRoot}` : ''
	const [, setPreferenceRevision] = useState(0)
	const enabled = notificationEnabled(storageKey)
	const seenApprovals = useRef<Set<string> | null>(null)
	const seenClarifications = useRef<Set<string> | null>(null)
	const runStatuses = useRef<Map<string, RunStatus> | null>(null)

	useEffect(() => {
		seenApprovals.current = notifyNewItems(
			seenApprovals.current, approvals, enabled, 'Agent 等待权限',
			(item) => item.command || item.reason || '打开 AiTodos 查看并决定',
		)
	}, [approvals, enabled])
	useEffect(() => {
		seenClarifications.current = notifyNewItems(
			seenClarifications.current, clarifications, enabled, 'Agent 等待回答',
			(item) => item.question,
		)
	}, [clarifications, enabled])
	useEffect(() => {
		if (!enabled || !workersEnabled) return
		const controller = new AbortController()
		async function loadRuns() {
			try {
				const page = await getRuns({ limit: 50 }, controller.signal)
				if (!controller.signal.aborted) runStatuses.current = notifyRunTransitions(runStatuses.current, page.items)
			} catch {
				// 通知是辅助能力，查询失败不覆盖主界面的可操作错误。
			}
		}
		void loadRuns()
		const interval = window.setInterval(() => { void loadRuns() }, 5000)
		return () => { controller.abort(); window.clearInterval(interval) }
	}, [enabled, workersEnabled])

	const toggle = useCallback(async () => {
		if (!notificationSupported() || storageKey === '') return
		if (enabled) {
			localStorage.setItem(storageKey, 'off')
			setPreferenceRevision((current) => current + 1)
			return
		}
		const permission = Notification.permission === 'granted' ? 'granted' : await Notification.requestPermission()
		if (permission === 'granted') {
			localStorage.setItem(storageKey, 'on')
		}
		setPreferenceRevision((current) => current + 1)
	}, [enabled, storageKey])

	const permission = notificationSupported() ? Notification.permission : 'denied'
	return useMemo(() => ({
		enabled,
		supported: notificationSupported(),
		blocked: notificationSupported() && permission === 'denied',
		label: enabled ? '桌面提醒已开启' : permission === 'denied' ? '桌面提醒已被浏览器禁用' : '开启桌面提醒',
		toggle,
	}), [enabled, permission, toggle])
}

function notificationEnabled(storageKey: string): boolean {
	return storageKey !== '' && notificationSupported() && Notification.permission === 'granted' && localStorage.getItem(storageKey) === 'on'
}

function notificationSupported(): boolean {
	return typeof Notification !== 'undefined'
}

function notifyNewItems<T extends { id: string }>(
	previous: Set<string> | null,
	items: T[],
	enabled: boolean,
	title: string,
	body: (item: T) => string,
): Set<string> {
	const current = new Set(items.map((item) => item.id))
	if (previous !== null && enabled) {
		for (const item of items) {
			if (!previous.has(item.id)) showNotification(title, body(item), item.id)
		}
	}
	return current
}

function notifyRunTransitions(previous: Map<string, RunStatus> | null, runs: AgentRun[]): Map<string, RunStatus> {
	const current = new Map(runs.map((run) => [run.id, run.status]))
	if (previous !== null) {
		for (const run of runs) {
			const oldStatus = previous.get(run.id)
			if (oldStatus && !terminalStatuses.has(oldStatus) && terminalStatuses.has(run.status)) {
				showNotification(run.status === 'SUCCEEDED' ? 'Run 已完成' : 'Run 需要关注', runStatusMessage(run.status), run.id)
			}
		}
	}
	return current
}

function runStatusMessage(status: RunStatus): string {
	return ({
		NEEDS_INPUT: 'Agent 正在等待你的回答', SUCCEEDED: '执行完成，等待人工验收', FAILED: '执行失败，请查看日志',
		CANCELLED: '执行已取消', TIMED_OUT: '执行超时', LOST: '执行进程状态丢失，需要人工检查',
	} as Partial<Record<RunStatus, string>>)[status] ?? status
}

function showNotification(title: string, body: string, tag: string) {
	if (!notificationSupported() || Notification.permission !== 'granted' || !document.hidden) return
	const notification = new Notification(title, { body, tag })
	notification.onclick = () => window.focus()
}
