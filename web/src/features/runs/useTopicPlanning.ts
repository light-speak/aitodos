import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { getRuns, requestTopicPlanning } from '../../api/client'
import type { AgentRun, RunStatus, Topic } from '../../types'

interface PlanningState {
	topicID: string
	run: AgentRun | null
	error: unknown
}

const activeStatuses = new Set<RunStatus>(['CLAIMED', 'STARTING', 'RUNNING', 'FINALIZING'])

export function useTopicPlanning(topic: Topic | null, workersEnabled: boolean) {
	const [state, setState] = useState<PlanningState | null>(null)
	const [reloadToken, setReloadToken] = useState(0)
	const [requesting, setRequesting] = useState(false)
	const eventCursor = useRef<Record<string, number>>({})
	const topicID = topic?.id ?? null

	useEffect(() => {
		if (topicID === null) return
		const selectedTopicID = topicID
		const controller = new AbortController()
		let loading = false
		const load = async () => {
			if (loading) return
			loading = true
			try {
				const page = await getRuns({ topic_id: selectedTopicID, purpose: 'PLANNING', limit: 20 }, controller.signal)
				if (!controller.signal.aborted) setState({ topicID: selectedTopicID, run: page.items[0] ?? null, error: null })
			} catch (error: unknown) {
				if (!controller.signal.aborted) setState((current) => ({ topicID: selectedTopicID, run: current?.topicID === selectedTopicID ? current.run : null, error }))
			} finally {
				loading = false
			}
		}
		void load()
		if (!workersEnabled) return () => controller.abort()
		const timer = window.setInterval(() => { void load() }, 1_500)
		return () => {
			controller.abort()
			window.clearInterval(timer)
		}
	}, [reloadToken, topicID, workersEnabled])

	const current = state?.topicID === topicID ? state : null
	const activeRunID = current?.run && activeStatuses.has(current.run.status) ? current.run.id : null
	useEffect(() => {
		if (activeRunID === null || typeof EventSource === 'undefined') return
		const after = eventCursor.current[activeRunID] ?? 0
		const suffix = after > 0 ? `?after=${after}` : ''
		const source = new EventSource(`/api/runs/${encodeURIComponent(activeRunID)}/events${suffix}`)
		source.onmessage = (message) => {
			const sequence = Number(message.lastEventId)
			if (Number.isFinite(sequence) && sequence > (eventCursor.current[activeRunID] ?? 0)) {
				eventCursor.current[activeRunID] = sequence
			}
			setReloadToken((value) => value + 1)
		}
		return () => source.close()
	}, [activeRunID])

	const reload = useCallback(() => setReloadToken((value) => value + 1), [])
	const requestPlanning = useCallback(async () => {
		if (topic === null) throw new Error('当前没有 Topic')
		setRequesting(true)
		try {
			const updated = await requestTopicPlanning(topic.id, topic.version)
			reload()
			return updated
		} finally {
			setRequesting(false)
		}
	}, [reload, topic])

	return useMemo(() => ({
		run: current?.run ?? null,
		loading: topic !== null && current === null,
		error: current?.error ?? null,
		requesting,
		reload,
		requestPlanning,
	}), [current, reload, requesting, requestPlanning, topic])
}
