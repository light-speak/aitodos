import { useCallback, useEffect, useMemo, useState } from 'react'

import { createMessage, createTaskFeedback, getMessages, getTaskFeedback, retryTaskFeedback } from '../../api/client'
import type { DiscussionMessage, DiscussionSubjectKind, TaskFeedback, TaskFeedbackIntent, TaskFeedbackResponse } from '../../types'

interface DiscussionState {
  key: string | null
  messages: DiscussionMessage[]
	feedback: TaskFeedback[]
  loading: boolean
  error: unknown
}

const emptyState: DiscussionState = { key: null, messages: [], feedback: [], loading: false, error: null }

export function useDiscussion(subjectKind: DiscussionSubjectKind | null, subjectID: string | null) {
  const key = subjectKind !== null && subjectID !== null ? `${subjectKind}:${subjectID}` : null
  const [state, setState] = useState<DiscussionState>(emptyState)
  const [reloadToken, setReloadToken] = useState(0)
  const [submitting, setSubmitting] = useState(false)
	const [retryingFeedbackID, setRetryingFeedbackID] = useState<string | null>(null)

  useEffect(() => {
    if (subjectKind === null || subjectID === null || key === null) return
    const currentKind = subjectKind
    const currentID = subjectID
    const controller = new AbortController()
    async function load() {
      try {
		const [messages, feedback] = await Promise.all([
			getMessages(currentKind, currentID, controller.signal),
			currentKind === 'tasks' ? getTaskFeedback(currentID, controller.signal) : Promise.resolve([]),
		])
        setState({ key, messages, feedback, loading: false, error: null })
      } catch (error: unknown) {
		if (!controller.signal.aborted) setState({ key, messages: [], feedback: [], loading: false, error })
      }
    }
    void load()
    return () => controller.abort()
  }, [key, reloadToken, subjectID, subjectKind])

	const currentFeedback = state.key === key ? state.feedback : []
	const hasPendingFeedback = currentFeedback.some((item) => item.status === 'QUEUED' || item.status === 'RUNNING')
	useEffect(() => {
		if (subjectKind !== 'tasks' || subjectID === null || !hasPendingFeedback || typeof EventSource === 'undefined') return
		const source = new EventSource(`/api/tasks/${encodeURIComponent(subjectID)}/feedback/events`)
		let reloadTimer: number | undefined
		const handleEvent = () => {
			window.clearTimeout(reloadTimer)
			reloadTimer = window.setTimeout(() => setReloadToken((current) => current + 1), 50)
		}
		source.addEventListener('task-feedback', handleEvent)
		return () => {
			window.clearTimeout(reloadTimer)
			source.close()
		}
	}, [hasPendingFeedback, subjectID, subjectKind])

  const sendMessage = useCallback(async (
		content: string,
		linkedTaskIDs: string[],
		intent: TaskFeedbackIntent = 'NOTE',
		expectedTaskVersion = 0,
	): Promise<TaskFeedbackResponse | undefined> => {
    if (subjectKind === null || subjectID === null || key === null) return
    setSubmitting(true)
    try {
		const feedback = subjectKind === 'tasks'
			? await createTaskFeedback(subjectID, { content, linked_task_ids: linkedTaskIDs, intent, expected_task_version: expectedTaskVersion })
			: undefined
		const created = feedback?.message ?? await createMessage(subjectKind, subjectID, { content, linked_task_ids: linkedTaskIDs })
      setState((current) => ({
        key,
        messages: current.key === key ? [...current.messages, created] : [created],
		feedback: feedback?.feedback
			? current.key === key ? [...current.feedback, feedback.feedback] : [feedback.feedback]
			: current.key === key ? current.feedback : [],
        loading: false,
        error: null,
      }))
		return feedback
    } finally {
      setSubmitting(false)
    }
  }, [key, subjectID, subjectKind])

	const retryFeedback = useCallback(async (feedbackID: string): Promise<void> => {
		if (key === null) return
		setRetryingFeedbackID(feedbackID)
		try {
			const created = await retryTaskFeedback(feedbackID)
			setState((current) => ({
				key,
				messages: current.key === key ? current.messages : [],
				feedback: current.key === key ? [...current.feedback, created] : [created],
				loading: false,
				error: null,
			}))
		} catch (error: unknown) {
			setState((current) => ({ ...current, error }))
		} finally {
			setRetryingFeedbackID(null)
		}
	}, [key])

  const reload = useCallback(() => {
    if (key === null) return
    setState((current) => ({
      key,
      messages: current.key === key ? current.messages : [],
		feedback: current.key === key ? current.feedback : [],
      loading: true,
      error: null,
    }))
    setReloadToken((current) => current + 1)
  }, [key])

  return useMemo(() => ({
	...(state.key === key ? state : { key, messages: [], feedback: [], loading: key !== null, error: null }),
    submitting,
	retryingFeedbackID,
    sendMessage,
	retryFeedback,
    reload,
	}), [key, reload, retryFeedback, retryingFeedbackID, sendMessage, state, submitting])
}
