import { useCallback, useEffect, useState } from 'react'

import { addProjectMCPServer, addProjectSkill, getProjectCapabilities, refreshProjectSkill } from '../../api/client'
import type { ProjectCapabilityCatalog } from '../../types'

const emptyCatalog: ProjectCapabilityCatalog = { skills: [], mcp_servers: [] }

export function useProjectCapabilities(active: boolean) {
	const [state, setState] = useState<{ catalog: ProjectCapabilityCatalog; error: unknown; loadedToken: number }>({
		catalog: emptyCatalog, error: null, loadedToken: -1,
	})
	const [adding, setAdding] = useState(false)
	const [reloadToken, setReloadToken] = useState(0)
	useEffect(() => {
		if (!active) return
		const controller = new AbortController()
		getProjectCapabilities(controller.signal).then((value) => {
			if (!controller.signal.aborted) setState({ catalog: value, error: null, loadedToken: reloadToken })
		}).catch((cause: unknown) => {
			if (!controller.signal.aborted) setState((current) => ({ ...current, error: cause, loadedToken: reloadToken }))
		})
		return () => controller.abort()
	}, [active, reloadToken])
	const addSkill = useCallback(async (input: { name: string; source_path: string }) => {
		setAdding(true)
		try {
			const created = await addProjectSkill(input)
			setState((current) => ({ ...current, catalog: { ...current.catalog, skills: [...current.catalog.skills, created] }, error: null }))
		} catch (cause: unknown) { setState((current) => ({ ...current, error: cause })); throw cause } finally { setAdding(false) }
	}, [])
	const addMCPServer = useCallback(async (input: { name: string; config_name: string }) => {
		setAdding(true)
		try {
			const created = await addProjectMCPServer(input)
			setState((current) => ({ ...current, catalog: { ...current.catalog, mcp_servers: [...current.catalog.mcp_servers, created] }, error: null }))
		} catch (cause: unknown) { setState((current) => ({ ...current, error: cause })); throw cause } finally { setAdding(false) }
	}, [])
	const refreshSkill = useCallback(async (skillID: string, version: number) => {
		setAdding(true)
		try {
			const refreshed = await refreshProjectSkill(skillID, version)
			setState((current) => ({ ...current, catalog: { ...current.catalog, skills: current.catalog.skills.map((item) => item.id === skillID ? refreshed : item) }, error: null }))
		} catch (cause: unknown) { setState((current) => ({ ...current, error: cause })); throw cause } finally { setAdding(false) }
	}, [])
	return {
		catalog: state.catalog, loading: active && state.loadedToken !== reloadToken, adding, error: state.error,
		reload: useCallback(() => setReloadToken((current) => current + 1), []), addSkill, addMCPServer, refreshSkill,
	}
}
