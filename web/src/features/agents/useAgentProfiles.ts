import { useCallback, useEffect, useState } from 'react'

import { configureCodexAgentDefaults, createAgentProfileRevision, getAgentProfiles } from '../../api/client'
import type { AgentProfile, AgentProfileRevisionInput } from '../../types'

export function useAgentProfiles(active: boolean) {
	const [state, setState] = useState<{ profiles: AgentProfile[]; error: unknown; loadedToken: number }>({
		profiles: [], error: null, loadedToken: -1,
	})
	const [saving, setSaving] = useState(false)
	const [reloadToken, setReloadToken] = useState(0)
	useEffect(() => {
		if (!active) return
		const controller = new AbortController()
		async function load() {
			try {
				const profiles = await getAgentProfiles(controller.signal)
				if (!controller.signal.aborted) setState({ profiles, error: null, loadedToken: reloadToken })
			} catch (error: unknown) {
				if (!controller.signal.aborted) setState((current) => ({ ...current, error, loadedToken: reloadToken }))
			}
		}
		void load()
		return () => controller.abort()
	}, [active, reloadToken])
	const save = useCallback(async (profileID: string, input: AgentProfileRevisionInput) => {
		setSaving(true)
		try {
			const updated = await createAgentProfileRevision(profileID, input)
			setState((current) => ({
				...current, profiles: current.profiles.map((profile) => profile.id === profileID ? updated : profile),
			}))
		} finally { setSaving(false) }
	}, [])
	const reload = useCallback(() => setReloadToken((current) => current + 1), [])
	const configureDefaults = useCallback(async () => {
		setSaving(true)
		try {
			const profiles = await configureCodexAgentDefaults()
			setState((current) => ({ ...current, profiles, error: null }))
		} finally { setSaving(false) }
	}, [])
	return {
		profiles: state.profiles, loading: active && state.loadedToken !== reloadToken,
		saving, error: state.error, save, reload, configureDefaults,
	}
}
