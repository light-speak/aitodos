import { useCallback, useEffect, useMemo, useState } from 'react'

import { answerClarification, getOpenClarifications, getTaskClarifications } from '../../api/client'
import type { Clarification, ClarificationAnswerInput, Task } from '../../types'

interface ClarificationState {
	open: Clarification[]
	history: Clarification[]
	historyTaskID: string | null
	error: unknown
}

export function useClarifications(taskID: string | null, polling: boolean) {
	const [state, setState] = useState<ClarificationState>({ open: [], history: [], historyTaskID: null, error: null })
	const [reloadToken, setReloadToken] = useState(0)
	const [answeringID, setAnsweringID] = useState<string | null>(null)
	const reload = useCallback(() => setReloadToken((current) => current + 1), [])

	useEffect(() => {
		const controller = new AbortController()
		async function loadOpen() {
			try {
				const open = await getOpenClarifications(controller.signal)
				if (!controller.signal.aborted) setState((current) => ({ ...current, open, error: null }))
			} catch (error: unknown) {
				if (!controller.signal.aborted) setState((current) => ({ ...current, error }))
			}
		}
		void loadOpen()
		const interval = polling ? window.setInterval(() => { void loadOpen() }, 5000) : undefined
		return () => {
			controller.abort()
			if (interval !== undefined) window.clearInterval(interval)
		}
	}, [polling, reloadToken])

	useEffect(() => {
		if (taskID === null) return
		const selectedTaskID = taskID
		const controller = new AbortController()
		async function loadHistory() {
			try {
				const history = await getTaskClarifications(selectedTaskID, controller.signal)
				if (!controller.signal.aborted) setState((current) => ({ ...current, history, historyTaskID: selectedTaskID, error: null }))
			} catch (error: unknown) {
				if (!controller.signal.aborted) setState((current) => ({ ...current, history: [], historyTaskID: selectedTaskID, error }))
			}
		}
		void loadHistory()
		return () => controller.abort()
	}, [reloadToken, taskID])

	const answer = useCallback(async (item: Clarification, input: Omit<ClarificationAnswerInput, 'expected_version'>): Promise<Task> => {
		setAnsweringID(item.id)
		try {
			const result = await answerClarification(item.id, { ...input, expected_version: item.version })
			setState((current) => ({
				...current,
				open: current.open.filter((candidate) => candidate.id !== item.id),
				history: current.history.map((candidate) => candidate.id === item.id ? result.clarification : candidate),
			}))
			return result.task
		} finally {
			setAnsweringID(null)
		}
	}, [])

	return useMemo(() => ({
		open: state.open,
		history: state.historyTaskID === taskID ? state.history : [],
		error: state.error,
		answeringID,
		reload,
		answer,
	}), [answer, answeringID, reload, state, taskID])
}
