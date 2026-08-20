import { useCallback, useEffect, useState } from 'react'

import { getProjectProgress, getRunUsageSummary } from '../../api/client'
import type { ProjectProgress, RunUsageSummary } from '../../types'

export function useProjectProgress(active: boolean) {
	const [state, setState] = useState<{ progress: ProjectProgress | null; usage: RunUsageSummary | null; error: unknown; loadedToken: number }>({
		progress: null, usage: null, error: null, loadedToken: -1,
	})
	const [reloadToken, setReloadToken] = useState(0)
	useEffect(() => {
		if (!active) return
		const controller = new AbortController()
		async function load() {
			try {
				const [progress, usage] = await Promise.all([
					getProjectProgress(controller.signal), getRunUsageSummary(controller.signal),
				])
				if (!controller.signal.aborted) setState({ progress, usage, error: null, loadedToken: reloadToken })
			} catch (error: unknown) {
				if (!controller.signal.aborted) setState((current) => ({ ...current, error, loadedToken: reloadToken }))
			}
		}
		void load()
		return () => controller.abort()
	}, [active, reloadToken])
	const reload = useCallback(() => setReloadToken((current) => current + 1), [])
	return { progress: state.progress, usage: state.usage, loading: active && state.loadedToken !== reloadToken, error: state.error, reload }
}
