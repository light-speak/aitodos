import { useCallback, useEffect, useMemo, useState } from 'react'

import { getRuns, requestRunCancellation } from '../../api/client'
import type { AgentRun, RunPurpose, RunStatus } from '../../types'

interface QueryState {
	key: string
	items: AgentRun[]
	nextCursor?: string
	error: unknown
}

export function useRunQuery(enabled: boolean) {
	const [status, setStatus] = useState<RunStatus | ''>('')
	const [purpose, setPurpose] = useState<RunPurpose | ''>('')
	const [state, setState] = useState<QueryState | null>(null)
	const [reloadToken, setReloadToken] = useState(0)
	const [loadingMore, setLoadingMore] = useState(false)
	const [cancellingRunID, setCancellingRunID] = useState<string | null>(null)
	const key = `${status}|${purpose}`

	useEffect(() => {
		if (!enabled) return
		const controller = new AbortController()
		void getRuns({ status: status || undefined, purpose: purpose || undefined, limit: 50 }, controller.signal).then(
			(page) => setState({ key, items: page.items, nextCursor: page.next_cursor, error: null }),
			(error: unknown) => { if (!controller.signal.aborted) setState({ key, items: [], error }) },
		)
		return () => controller.abort()
	}, [enabled, key, purpose, reloadToken, status])

	const loadMore = useCallback(async () => {
		if (state?.key !== key || !state.nextCursor) return
		setLoadingMore(true)
		try {
			const page = await getRuns({ status: status || undefined, purpose: purpose || undefined, limit: 50, cursor: state.nextCursor })
			setState((current) => current?.key === key ? {
				key, items: [...current.items, ...page.items], nextCursor: page.next_cursor, error: null,
			} : current)
		} catch (error: unknown) {
			setState((current) => current?.key === key ? { ...current, error } : current)
		} finally {
			setLoadingMore(false)
		}
	}, [key, purpose, state, status])

	const cancelRun = useCallback(async (runID: string, reason: string) => {
		setCancellingRunID(runID)
		try {
			const updated = await requestRunCancellation(runID, reason)
			setState((current) => current === null ? null : {
				...current, items: current.items.map((item) => item.id === runID ? updated : item),
			})
		} finally {
			setCancellingRunID(null)
		}
	}, [])

	const current = state?.key === key ? state : null
	return useMemo(() => ({
		status, purpose, setStatus, setPurpose,
		items: current?.items ?? [], error: current?.error ?? null,
		loading: enabled && current === null, hasMore: Boolean(current?.nextCursor), loadingMore,
		cancellingRunID, cancelRun, loadMore,
		reload: () => setReloadToken((value) => value + 1),
	}), [cancelRun, cancellingRunID, current, enabled, loadMore, loadingMore, purpose, status])
}
