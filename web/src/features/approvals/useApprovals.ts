import { useCallback, useEffect, useMemo, useState } from 'react'

import { decideApprovalRequest, getOpenApprovalRequests } from '../../api/client'
import type { ApprovalDecision, ApprovalRequest } from '../../types'

export function useApprovals(polling: boolean) {
	const [items, setItems] = useState<ApprovalRequest[]>([])
	const [error, setError] = useState<unknown>(null)
	const [decidingID, setDecidingID] = useState<string | null>(null)
	const [reloadToken, setReloadToken] = useState(0)

	useEffect(() => {
		const controller = new AbortController()
		async function load() {
			try {
				const open = await getOpenApprovalRequests(controller.signal)
				if (!controller.signal.aborted) {
					setItems(open)
					setError(null)
				}
			} catch (loadError: unknown) {
				if (!controller.signal.aborted) setError(loadError)
			}
		}
		void load()
		const interval = polling ? window.setInterval(() => { void load() }, 2000) : undefined
		return () => {
			controller.abort()
			if (interval !== undefined) window.clearInterval(interval)
		}
	}, [polling, reloadToken])

	const decide = useCallback(async (item: ApprovalRequest, decision: ApprovalDecision) => {
		setDecidingID(item.id)
		try {
			await decideApprovalRequest(item.id, decision, item.version)
			setItems((current) => current.filter((candidate) => candidate.id !== item.id))
		} finally {
			setDecidingID(null)
		}
	}, [])

	return useMemo(() => ({
		items, error, decidingID, decide,
		reload: () => setReloadToken((current) => current + 1),
	}), [decide, decidingID, error, items])
}
