import { useCallback, useEffect, useMemo, useState } from 'react'

import { createTask, createTopic, getProject, getTasks, getTopics, updateWorkerSettings } from '../../api/client'
import type { CreateTaskInput, CreateTopicInput, ProjectInfo, Task, Topic } from '../../types'

interface BoardState {
  project: ProjectInfo | null
  topics: Topic[]
  tasks: Task[]
  loading: boolean
  error: unknown
}

const initialState: BoardState = { project: null, topics: [], tasks: [], loading: true, error: null }
const workerRefreshIntervalMs = 2_000

export function useTaskBoard() {
  const [state, setState] = useState<BoardState>(initialState)
  const [reloadToken, setReloadToken] = useState(0)
	const [updatingWorkers, setUpdatingWorkers] = useState(false)

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

  useEffect(() => {
    if (!state.project?.workers_enabled) return
    const controller = new AbortController()
    let loading = false
    const timer = window.setInterval(() => {
      if (loading) return
      loading = true
      void getTasks(controller.signal).then(
        (tasks) => setState((current) => ({ ...current, tasks })),
        (error: unknown) => {
          if (!controller.signal.aborted) {
            setState((current) => ({ ...current, error }))
          }
        },
      ).finally(() => { loading = false })
    }, workerRefreshIntervalMs)
    return () => {
      controller.abort()
      window.clearInterval(timer)
    }
  }, [state.project?.workers_enabled])

  const create = useCallback(async (input: CreateTaskInput) => {
    const created = await createTask(input)
    setState((current) => ({ ...current, tasks: [...current.tasks, created] }))
  }, [])

  const createNewTopic = useCallback(async (input: CreateTopicInput) => {
    const created = await createTopic(input)
    setState((current) => ({ ...current, topics: [created, ...current.topics] }))
  }, [])

	const updateWorkers = useCallback(async (enabled: boolean, maxWorkers: number) => {
		setUpdatingWorkers(true)
		try {
			const project = await updateWorkerSettings(enabled, maxWorkers)
			setState((current) => ({ ...current, project }))
		} finally {
			setUpdatingWorkers(false)
		}
	}, [])

  const reload = useCallback(() => {
    setState((current) => ({ ...current, loading: true, error: null }))
    setReloadToken((current) => current + 1)
  }, [])

	const updateTask = useCallback((updated: Task) => {
		setState((current) => ({ ...current, tasks: replaceTask(current.tasks, updated) }))
	}, [])

  return useMemo(
    () => ({
      ...state,
		updatingWorkers,
      createTask: create,
      createTopic: createNewTopic,
		updateWorkers,
			updateTask,
      reload,
    }),
		[create, createNewTopic, reload, state, updateTask, updateWorkers, updatingWorkers],
  )
}

async function loadBoard(signal: AbortSignal) {
  const [project, topics, tasks] = await Promise.all([getProject(signal), getTopics(signal), getTasks(signal)])
  return { project, topics, tasks }
}

function replaceTask(tasks: Task[], updated: Task): Task[] {
	return tasks.map((item) => {
		if (item.id !== updated.id) return item
		return {
			...updated,
			...(updated.assessment === undefined && item.assessment !== undefined ? { assessment: item.assessment } : {}),
			...(updated.assessment_stale === undefined && item.assessment_stale !== undefined
				? { assessment_stale: item.assessment_stale }
				: {}),
		}
	})
}
