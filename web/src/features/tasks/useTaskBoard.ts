import { useCallback, useEffect, useMemo, useState } from 'react'

import { archiveTask, cancelTask, commandObjective, createObjective, createTask, createTopic, getCurrentObjective, getProject, getTasks, getTopics, updateTaskDetails, updateTaskTargetBranch, updateWorkerSettings } from '../../api/client'
import type { CreateObjectiveInput, CreateTaskInput, CreateTopicInput, ObjectiveCommand, ObjectiveView, ProjectInfo, Task, Topic, UpdateTaskDetailsInput } from '../../types'

interface BoardState {
  project: ProjectInfo | null
  topics: Topic[]
  tasks: Task[]
	objective: ObjectiveView | null
  loading: boolean
  error: unknown
}

const initialState: BoardState = { project: null, topics: [], tasks: [], objective: null, loading: true, error: null }
const workerRefreshIntervalMs = 2_000

export function useTaskBoard() {
  const [state, setState] = useState<BoardState>(initialState)
  const [reloadToken, setReloadToken] = useState(0)
	const [updatingWorkers, setUpdatingWorkers] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    void loadBoard(controller.signal).then(
		({ project, topics, tasks, objective }) => setState({ project, topics, tasks, objective, loading: false, error: null }),
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
		void Promise.all([getProject(controller.signal), getTopics(controller.signal), getTasks(controller.signal), getCurrentObjective(controller.signal)]).then(
			([project, topics, tasks, objective]) => setState((current) => ({ ...current, project, topics, tasks, objective })),
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
		return created
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

	const createLongObjective = useCallback(async (input: CreateObjectiveInput) => {
		const objective = await createObjective(input)
		setState((current) => ({ ...current, objective }))
		return objective
	}, [])

	const controlObjective = useCallback(async (command: ObjectiveCommand) => {
		if (state.objective === null) return null
		const updated = await commandObjective(state.objective.objective.id, command, state.objective.objective.version)
		setState((current) => ({ ...current, objective: updated.objective.status === 'ACTIVE' || updated.objective.status === 'PAUSED' ? updated : null }))
		return updated
	}, [state.objective])

  const reload = useCallback(() => {
    setState((current) => ({ ...current, loading: true, error: null }))
    setReloadToken((current) => current + 1)
  }, [])

	const updateTask = useCallback((updated: Task) => {
		setState((current) => ({ ...current, tasks: replaceTask(current.tasks, updated) }))
	}, [])

	const updateTopic = useCallback((updated: Topic) => {
		setState((current) => ({ ...current, topics: replaceTopic(current.topics, updated) }))
	}, [])

	const changeTargetBranch = useCallback(async (current: Task, targetBranch: string) => {
		const updated = await updateTaskTargetBranch(current.id, targetBranch, current.version)
		setState((state) => ({ ...state, tasks: replaceTask(state.tasks, updated) }))
		return updated
	}, [])

	const changeTaskDetails = useCallback(async (current: Task, input: UpdateTaskDetailsInput) => {
		const updated = await updateTaskDetails(current.id, input, current.version)
		setState((state) => ({ ...state, tasks: replaceTask(state.tasks, updated) }))
		return updated
	}, [])

	const cancelCurrentTask = useCallback(async (current: Task) => {
		const updated = await cancelTask(current.id, current.version)
		setState((state) => ({ ...state, tasks: replaceTask(state.tasks, updated) }))
		return updated
	}, [])

	const archiveCurrentTask = useCallback(async (current: Task) => {
		const updated = await archiveTask(current.id, current.version)
		setState((state) => ({ ...state, tasks: replaceTask(state.tasks, updated) }))
		return updated
	}, [])

  return useMemo(
    () => ({
      ...state,
		updatingWorkers,
      createTask: create,
      createTopic: createNewTopic,
		updateWorkers,
			createObjective: createLongObjective,
			commandObjective: controlObjective,
			updateTask,
			updateTopic,
			updateTargetBranch: changeTargetBranch,
			updateTaskDetails: changeTaskDetails,
			cancelTask: cancelCurrentTask,
			archiveTask: archiveCurrentTask,
      reload,
    }),
		[archiveCurrentTask, cancelCurrentTask, changeTargetBranch, changeTaskDetails, controlObjective, create, createLongObjective, createNewTopic, reload, state, updateTask, updateTopic, updateWorkers, updatingWorkers],
  )
}

function replaceTopic(items: Topic[], updated: Topic): Topic[] {
	return items.map((item) => item.id === updated.id ? updated : item)
}

async function loadBoard(signal: AbortSignal) {
	const [project, topics, tasks, objective] = await Promise.all([getProject(signal), getTopics(signal), getTasks(signal), getCurrentObjective(signal)])
	return { project, topics, tasks, objective }
}

function replaceTask(tasks: Task[], updated: Task): Task[] {
	if (updated.archived_at) return tasks.filter((item) => item.id !== updated.id)
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
