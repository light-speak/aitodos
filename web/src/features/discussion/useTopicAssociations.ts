import { useCallback, useEffect, useMemo, useState } from 'react'

import { getTaskTopics, linkTopic, unlinkTopic } from '../../api/client'
import type { Topic, TopicAssociation } from '../../types'

interface TopicAssociationState {
  taskID: string | null
  associations: TopicAssociation[]
  loading: boolean
  error: unknown
}

const emptyState: TopicAssociationState = { taskID: null, associations: [], loading: false, error: null }

export function useTopicAssociations(taskID: string | null, topics: Topic[]) {
  const [state, setState] = useState<TopicAssociationState>(emptyState)
  const [pendingTopicIDs, setPendingTopicIDs] = useState<Set<string>>(() => new Set())

  useEffect(() => {
    if (taskID === null) return
    const currentTaskID = taskID
    const controller = new AbortController()
    async function load() {
      try {
        const associations = await getTaskTopics(currentTaskID, controller.signal)
        setState({ taskID: currentTaskID, associations, loading: false, error: null })
      } catch (error: unknown) {
        if (!controller.signal.aborted) {
          setState({ taskID: currentTaskID, associations: [], loading: false, error })
        }
      }
    }
    void load()
    return () => controller.abort()
  }, [taskID])

  const add = useCallback(async (topicID: string) => {
    if (taskID === null) return
    setPendingTopicIDs((current) => withID(current, topicID, true))
    try {
      await linkTopic(taskID, topicID)
      const topic = topics.find((item) => item.id === topicID)
      if (topic !== undefined) {
        setState((current) => current.taskID === taskID
          ? { ...current, associations: appendTopic(current.associations, topic) }
          : current)
      }
    } finally {
      setPendingTopicIDs((current) => withID(current, topicID, false))
    }
  }, [taskID, topics])

  const remove = useCallback(async (topicID: string) => {
    if (taskID === null) return
    setPendingTopicIDs((current) => withID(current, topicID, true))
    try {
      await unlinkTopic(taskID, topicID)
      setState((current) => current.taskID === taskID
        ? { ...current, associations: current.associations.filter((item) => item.topic.id !== topicID) }
        : current)
    } finally {
      setPendingTopicIDs((current) => withID(current, topicID, false))
    }
  }, [taskID])

  return useMemo(() => ({
    ...(state.taskID === taskID
      ? state
      : { taskID, associations: [], loading: taskID !== null, error: null }),
    pendingTopicIDs,
    add,
    remove,
  }), [add, pendingTopicIDs, remove, state, taskID])
}

function appendTopic(associations: TopicAssociation[], topic: Topic): TopicAssociation[] {
  if (associations.some((item) => item.topic.id === topic.id)) return associations
  return [...associations, { topic, created_at: new Date().toISOString() }]
}

function withID(current: Set<string>, id: string, present: boolean): Set<string> {
  const updated = new Set(current)
  if (present) updated.add(id)
  else updated.delete(id)
  return updated
}
