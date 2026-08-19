import { useCallback, useEffect, useMemo, useState } from 'react'

import { createTask, createTopic, getProject, getTasks, getTopics, queueTask } from '../../api/client'
import type { CreateTaskInput, CreateTopicInput, ProjectInfo, Task, Topic } from '../../types'

interface BoardState {
  project: ProjectInfo | null
  topics: Topic[]
  tasks: Task[]
  loading: boolean
  error: unknown
}

const initialState: BoardState = { project: null, topics: [], tasks: [], loading: true, error: null }

export function useTaskBoard() {
  const [state, setState] = useState<BoardState>(initialState)
  const [reloadToken, setReloadToken] = useState(0)
  const [pendingTaskIDs, setPendingTaskIDs] = useState<Set<string>>(() => new Set())

  useEffect(() => {
    const controller = new AbortController()
    void loadBoard(controller.signal).then(
      ({ project, topics, tasks }) => setState({ project, topics, tasks, loading: false, error: null }),
      (error: unknown) => {
        if (!controller.signal.aborted) {
          setState((current) => ({ ...current, loading: false, error }))
        }
      },
    )
    return () => controller.abort()
  }, [reloadToken])

  const create = useCallback(async (input: CreateTaskInput) => {
    const created = await createTask(input)
    setState((current) => ({ ...current, tasks: [...current.tasks, created] }))
  }, [])

  const createNewTopic = useCallback(async (input: CreateTopicInput) => {
    const created = await createTopic(input)
    setState((current) => ({ ...current, topics: [created, ...current.topics] }))
  }, [])

  const queue = useCallback(async (currentTask: Task) => {
    setPendingTaskIDs((current) => withTaskID(current, currentTask.id, true))
    try {
      const updated = await queueTask(currentTask)
      setState((current) => ({ ...current, tasks: replaceTask(current.tasks, updated) }))
    } finally {
      setPendingTaskIDs((current) => withTaskID(current, currentTask.id, false))
    }
  }, [])

  const reload = useCallback(() => {
    setState((current) => ({ ...current, loading: true, error: null }))
    setReloadToken((current) => current + 1)
  }, [])

  return useMemo(
    () => ({
      ...state,
      pendingTaskIDs,
      createTask: create,
      createTopic: createNewTopic,
      queueTask: queue,
      reload,
    }),
    [create, createNewTopic, pendingTaskIDs, queue, reload, state],
  )
}

async function loadBoard(signal: AbortSignal) {
  const [project, topics, tasks] = await Promise.all([getProject(signal), getTopics(signal), getTasks(signal)])
  return { project, topics, tasks }
}

function replaceTask(tasks: Task[], updated: Task): Task[] {
  return tasks.map((item) => (item.id === updated.id ? updated : item))
}

function withTaskID(current: Set<string>, taskID: string, present: boolean): Set<string> {
  const updated = new Set(current)
  if (present) {
    updated.add(taskID)
  } else {
    updated.delete(taskID)
  }
  return updated
}
