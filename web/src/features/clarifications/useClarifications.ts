import { useCallback, useEffect, useMemo, useState } from 'react'

import { answerClarification, getOpenClarifications, getTaskClarifications, getTopicClarifications } from '../../api/client'
import type { Clarification, ClarificationAnswerInput, Task, Topic } from '../../types'

interface ClarificationState {
	open: Clarification[]
	history: Clarification[]
	historySubject: string | null
	error: unknown
}

export function useClarifications(taskID: string | null, topicID: string | null, polling: boolean) {
	const [state, setState] = useState<ClarificationState>({ open: [], history: [], historySubject: null, error: null })
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
		const subject = taskID !== null ? `TASK:${taskID}` : topicID !== null ? `TOPIC:${topicID}` : null
		if (subject === null) return
		const controller = new AbortController()
		async function loadHistory() {
			try {
				const history = taskID !== null
					? await getTaskClarifications(taskID, controller.signal)
					: await getTopicClarifications(topicID!, controller.signal)
				if (!controller.signal.aborted) setState((current) => ({ ...current, history, historySubject: subject, error: null }))
			} catch (error: unknown) {
				if (!controller.signal.aborted) setState((current) => ({ ...current, history: [], historySubject: subject, error }))
			}
		}
		void loadHistory()
		return () => controller.abort()
	}, [reloadToken, taskID, topicID])

	const answer = useCallback(async (item: Clarification, input: Omit<ClarificationAnswerInput, 'expected_version'>): Promise<{ task?: Task; topic?: Topic }> => {
		setAnsweringID(item.id)
		try {
			const result = await answerClarification(item.id, { ...input, expected_version: item.version })
			setState((current) => ({
				...current,
				open: current.open.filter((candidate) => candidate.id !== item.id),
				history: current.history.map((candidate) => candidate.id === item.id ? result.clarification : candidate),
			}))
			return { task: result.task, topic: result.topic }
		} finally {
			setAnsweringID(null)
		}
	}, [])

	return useMemo(() => ({
		open: state.open,
		history: state.historySubject === (taskID !== null ? `TASK:${taskID}` : topicID !== null ? `TOPIC:${topicID}` : null) ? state.history : [],
		error: state.error,
		answeringID,
		reload,
		answer,
	}), [answer, answeringID, reload, state, taskID, topicID])
}
