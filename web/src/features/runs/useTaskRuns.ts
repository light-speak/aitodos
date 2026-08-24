import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { getRunDetail, getRunLog, getTaskRuns, requestRunCancellation } from '../../api/client'
import type { AgentRun, RunDetail, RunEvent, RunLog, RunStatus } from '../../types'

interface RunListState {
	taskID: string
	runs: AgentRun[]
	error: unknown
}

interface RunDetailState {
	runID: string
	detail: RunDetail | null
	error: unknown
}

export function useTaskRuns(taskID: string) {
	const [listState, setListState] = useState<RunListState | null>(null)
	const [selectedRunID, setSelectedRunID] = useState<string | null>(null)
	const [detailState, setDetailState] = useState<RunDetailState | null>(null)
	const [logs, setLogs] = useState<Partial<Record<RunLog['stream'], RunLog>>>({})
	const [loadingLog, setLoadingLog] = useState<RunLog['stream'] | null>(null)
	const [logError, setLogError] = useState<unknown>(null)
	const [reloadToken, setReloadToken] = useState(0)
	const [detailReloadToken, setDetailReloadToken] = useState(0)
	const [streamRetrying, setStreamRetrying] = useState(false)
	const [cancellingRunID, setCancellingRunID] = useState<string | null>(null)
	const eventCursor = useRef<Record<string, number>>({})

	useEffect(() => {
		const controller = new AbortController()
		void getTaskRuns(taskID, controller.signal).then(
			(runs) => setListState({ taskID, runs, error: null }),
			(error: unknown) => { if (!controller.signal.aborted) setListState({ taskID, runs: [], error }) },
		)
		return () => controller.abort()
	}, [reloadToken, taskID])

	useEffect(() => {
		if (selectedRunID === null) return
		const runID = selectedRunID
		const controller = new AbortController()
		void getRunDetail(runID, controller.signal).then(
			(detail) => setDetailState({ runID, detail, error: null }),
			(error: unknown) => { if (!controller.signal.aborted) setDetailState({ runID, detail: null, error }) },
		)
		return () => controller.abort()
	}, [detailReloadToken, selectedRunID])

	const currentList = listState?.taskID === taskID ? listState : null
	const activeRunID = currentList?.runs.find((run) => isActiveRunStatus(run.status))?.id ?? null
	useEffect(() => {
		if (activeRunID === null || typeof EventSource === 'undefined') return
		const after = eventCursor.current[activeRunID] ?? 0
		const suffix = after > 0 ? `?after=${after}` : ''
		const source = new EventSource(`/api/runs/${encodeURIComponent(activeRunID)}/events${suffix}`)
		source.onopen = () => setStreamRetrying(false)
		source.onerror = () => setStreamRetrying(true)
		source.onmessage = (message) => {
			const data: unknown = message.data
			if (typeof data !== 'string') return
			const event = parseRunEvent(data)
			if (event === null || event.run_id !== activeRunID || event.sequence <= (eventCursor.current[activeRunID] ?? 0)) return
			eventCursor.current[activeRunID] = event.sequence
			setReloadToken((current) => current + 1)
			if (selectedRunID === activeRunID) setDetailReloadToken((current) => current + 1)
			if (isTerminalRunEvent(event)) source.close()
		}
		return () => source.close()
	}, [activeRunID, selectedRunID])

	const selectRun = useCallback((runID: string | null) => {
		setSelectedRunID(runID)
		setLogs({})
		setLogError(null)
		setLoadingLog(null)
	}, [])

	const loadLog = useCallback(async (stream: RunLog['stream']) => {
		if (selectedRunID === null || logs[stream]) return
		setLoadingLog(stream)
		setLogError(null)
		try {
			const log = await getRunLog(selectedRunID, stream)
			setLogs((current) => ({ ...current, [stream]: log }))
		} catch (error: unknown) {
			setLogError(error)
		} finally {
			setLoadingLog(null)
		}
	}, [logs, selectedRunID])

	const cancelRun = useCallback(async (runID: string, reason: string) => {
		setCancellingRunID(runID)
		try {
			await requestRunCancellation(runID, reason)
			setReloadToken((current) => current + 1)
			if (selectedRunID === runID) setDetailReloadToken((current) => current + 1)
		} finally {
			setCancellingRunID(null)
		}
	}, [selectedRunID])

	const currentDetail = detailState?.runID === selectedRunID ? detailState : null
	return useMemo(() => ({
		runs: currentList?.runs ?? [],
		loading: currentList === null,
		error: currentList?.error ?? null,
		selectedRunID,
		detail: currentDetail?.detail ?? null,
		detailLoading: selectedRunID !== null && currentDetail === null,
		detailError: currentDetail?.error ?? null,
		logs,
		loadingLog,
		logError,
		streamRetrying: activeRunID !== null && streamRetrying,
		cancellingRunID,
		selectRun,
		loadLog,
		cancelRun,
		reload: () => setReloadToken((current) => current + 1),
	}), [activeRunID, cancelRun, cancellingRunID, currentDetail, currentList, loadLog, loadingLog, logError, logs, selectRun, selectedRunID, streamRetrying])
}

const activeRunStatuses = new Set<RunStatus>(['CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING'])
const terminalRunStatuses = new Set<RunStatus>(['NEEDS_INPUT', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'TIMED_OUT', 'LOST'])

function isActiveRunStatus(status: RunStatus): boolean {
	return activeRunStatuses.has(status)
}

function parseRunEvent(data: string): RunEvent | null {
	try {
		const value: unknown = JSON.parse(data)
		if (typeof value !== 'object' || value === null) return null
		const record = value as Record<string, unknown>
		if (typeof record.id !== 'string' || typeof record.run_id !== 'string' || typeof record.sequence !== 'number' ||
			typeof record.type !== 'string' || typeof record.occurred_at !== 'string') return null
		return {
			id: record.id, run_id: record.run_id, sequence: record.sequence,
			type: record.type, payload: record.payload, occurred_at: record.occurred_at,
		}
	} catch {
		return null
	}
}

function isTerminalRunEvent(event: RunEvent): boolean {
	if (event.type !== 'RUN_STATUS_CHANGED' || typeof event.payload !== 'object' || event.payload === null) return false
	const status = (event.payload as Record<string, unknown>).to
	return typeof status === 'string' && terminalRunStatuses.has(status as RunStatus)
}
