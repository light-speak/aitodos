import { useCallback, useEffect, useMemo, useState } from 'react'

import { getRuns } from '../../api/client'
import type { AgentRun } from '../../types'

interface AgentActivityState {
	runs: AgentRun[]
	error: unknown
	loaded: boolean
}

const refreshInterval = 2_000

export function useAgentActivity(enabled: boolean) {
	const [state, setState] = useState<AgentActivityState>({ runs: [], error: null, loaded: false })
	const [reloadToken, setReloadToken] = useState(0)

	useEffect(() => {
		if (!enabled) return
		const controller = new AbortController()
		let loading = false
		const load = async () => {
			if (loading) return
			loading = true
			try {
				const page = await getRuns({ active: true, limit: 100 }, controller.signal)
				if (!controller.signal.aborted) setState({ runs: page.items, error: null, loaded: true })
			} catch (error: unknown) {
				if (!controller.signal.aborted) setState((current) => ({ ...current, error, loaded: true }))
			} finally {
				loading = false
			}
		}

		void load()
		const timer = window.setInterval(() => { void load() }, refreshInterval)
		return () => {
			controller.abort()
			window.clearInterval(timer)
		}
	}, [enabled, reloadToken])

	const reload = useCallback(() => setReloadToken((value) => value + 1), [])
	return useMemo(() => ({
		runs: state.runs,
		error: state.error,
		loading: enabled && !state.loaded,
		reload,
	}), [enabled, reload, state])
}
