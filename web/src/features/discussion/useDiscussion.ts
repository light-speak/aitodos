import { useCallback, useEffect, useMemo, useState } from 'react'

import { createMessage, getMessages } from '../../api/client'
import type { DiscussionMessage, DiscussionSubjectKind } from '../../types'

interface DiscussionState {
  key: string | null
  messages: DiscussionMessage[]
  loading: boolean
  error: unknown
}

const emptyState: DiscussionState = { key: null, messages: [], loading: false, error: null }

export function useDiscussion(subjectKind: DiscussionSubjectKind | null, subjectID: string | null) {
  const key = subjectKind !== null && subjectID !== null ? `${subjectKind}:${subjectID}` : null
  const [state, setState] = useState<DiscussionState>(emptyState)
  const [reloadToken, setReloadToken] = useState(0)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (subjectKind === null || subjectID === null || key === null) return
    const currentKind = subjectKind
    const currentID = subjectID
    const controller = new AbortController()
    async function load() {
      try {
        const messages = await getMessages(currentKind, currentID, controller.signal)
        setState({ key, messages, loading: false, error: null })
      } catch (error: unknown) {
        if (!controller.signal.aborted) setState({ key, messages: [], loading: false, error })
      }
    }
    void load()
    return () => controller.abort()
  }, [key, reloadToken, subjectID, subjectKind])

  const sendMessage = useCallback(async (content: string, linkedTaskIDs: string[]) => {
    if (subjectKind === null || subjectID === null || key === null) return
    setSubmitting(true)
    try {
      const created = await createMessage(subjectKind, subjectID, { content, linked_task_ids: linkedTaskIDs })
      setState((current) => ({
        key,
        messages: current.key === key ? [...current.messages, created] : [created],
        loading: false,
        error: null,
      }))
    } finally {
      setSubmitting(false)
    }
  }, [key, subjectID, subjectKind])

  const reload = useCallback(() => {
    if (key === null) return
    setState((current) => ({
      key,
      messages: current.key === key ? current.messages : [],
      loading: true,
      error: null,
    }))
    setReloadToken((current) => current + 1)
  }, [key])

  return useMemo(() => ({
    ...(state.key === key ? state : { key, messages: [], loading: key !== null, error: null }),
    submitting,
    sendMessage,
    reload,
  }), [key, reload, sendMessage, state, submitting])
}
