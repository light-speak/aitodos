import { useCallback, useEffect, useMemo, useState } from 'react'

import { getTaskAssociations, linkTask, unlinkTask } from '../../api/client'
import type { DiscussionSubjectKind, Task, TaskAssociation, TaskRelationType } from '../../types'

interface AssociationState {
  key: string | null
  associations: TaskAssociation[]
  loading: boolean
  error: unknown
}

const emptyState: AssociationState = { key: null, associations: [], loading: false, error: null }

export function useTaskAssociations(
  subjectKind: DiscussionSubjectKind | null,
  subjectID: string | null,
  tasks: Task[],
) {
  const key = subjectKind !== null && subjectID !== null ? `${subjectKind}:${subjectID}` : null
  const [state, setState] = useState<AssociationState>(emptyState)
  const [reloadToken, setReloadToken] = useState(0)
  const [pendingTaskIDs, setPendingTaskIDs] = useState<Set<string>>(() => new Set())

  useEffect(() => {
    if (subjectKind === null || subjectID === null || key === null) return
    const currentKind = subjectKind
    const currentID = subjectID
    const controller = new AbortController()
    async function load() {
      try {
        const associations = await getTaskAssociations(currentKind, currentID, controller.signal)
        setState({ key, associations, loading: false, error: null })
      } catch (error: unknown) {
        if (!controller.signal.aborted) setState({ key, associations: [], loading: false, error })
      }
    }
    void load()
    return () => controller.abort()
  }, [key, reloadToken, subjectID, subjectKind])

  const add = useCallback(async (taskID: string, relationType: TaskRelationType = 'RELATES_TO') => {
    if (subjectKind === null || subjectID === null || key === null) return
    setPendingTaskIDs((current) => withTaskID(current, taskID, true))
    try {
      await linkTask(subjectKind, subjectID, taskID, relationType)
      const linkedTask = tasks.find((task) => task.id === taskID)
      if (linkedTask !== undefined) {
        includeAssociations(setState, key, [linkedTask], subjectKind === 'tasks' ? relationType : undefined)
      }
    } finally {
      setPendingTaskIDs((current) => withTaskID(current, taskID, false))
    }
  }, [key, subjectID, subjectKind, tasks])

  const remove = useCallback(async (association: TaskAssociation) => {
    if (subjectKind === null || subjectID === null || key === null) return
		const taskID = association.task.id
    setPendingTaskIDs((current) => withTaskID(current, taskID, true))
    try {
      await unlinkTask(subjectKind, subjectID, association)
      setState((current) => ({
        ...current,
        associations: current.key === key
          ? current.associations.filter((item) => item.task.id !== taskID)
          : current.associations,
      }))
    } finally {
      setPendingTaskIDs((current) => withTaskID(current, taskID, false))
    }
  }, [key, subjectID, subjectKind])

  const includeTaskIDs = useCallback((taskIDs: string[]) => {
    if (key === null) return
    const linkedTasks = tasks.filter((task) => taskIDs.includes(task.id))
    includeAssociations(setState, key, linkedTasks)
  }, [key, tasks])

  const reload = useCallback(() => {
    if (key === null) return
    setState((current) => ({
      key,
      associations: current.key === key ? current.associations : [],
      loading: true,
      error: null,
    }))
    setReloadToken((current) => current + 1)
  }, [key])

  return useMemo(() => ({
    ...(state.key === key ? state : { key, associations: [], loading: key !== null, error: null }),
    pendingTaskIDs,
    add,
    remove,
    includeTaskIDs,
    reload,
  }), [add, includeTaskIDs, key, pendingTaskIDs, reload, remove, state])
}

function includeAssociations(
  setState: React.Dispatch<React.SetStateAction<AssociationState>>,
  key: string,
  tasks: Task[],
	relationType?: TaskRelationType,
) {
  setState((current) => {
    if (current.key !== key) return current
    const existing = new Set(current.associations.map((item) => item.task.id))
    const createdAt = new Date().toISOString()
    const additions = tasks
      .filter((task) => !existing.has(task.id))
		.map((task) => ({ task, created_at: createdAt, ...(relationType ? { type: relationType, direction: relationType === 'RELATES_TO' ? 'BIDIRECTIONAL' as const : 'OUTGOING' as const } : {}) }))
    return { ...current, associations: [...current.associations, ...additions] }
  })
}

function withTaskID(current: Set<string>, taskID: string, present: boolean): Set<string> {
  const updated = new Set(current)
  if (present) updated.add(taskID)
  else updated.delete(taskID)
  return updated
}
