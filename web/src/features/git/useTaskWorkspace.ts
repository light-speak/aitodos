import { useCallback, useEffect, useMemo, useState } from 'react'

import { createTaskWorkspace, getTaskWorkspace } from '../../api/client'
import type { Workspace } from '../../types'

interface WorkspaceState {
  taskID: string | null
  workspace: Workspace | null
  loading: boolean
  error: unknown
}

const emptyState: WorkspaceState = { taskID: null, workspace: null, loading: false, error: null }

export function useTaskWorkspace(taskID: string | null) {
	const [state, setState] = useState<WorkspaceState>(emptyState)
  const [creating, setCreating] = useState(false)

  useEffect(() => {
		if (taskID === null) return
		const currentTaskID = taskID
    const controller = new AbortController()
    void getTaskWorkspace(taskID, controller.signal).then(
      (loaded) => {
				setState({ taskID: currentTaskID, workspace: loaded, loading: false, error: null })
      },
      (loadError: unknown) => {
        if (!controller.signal.aborted) {
					setState({ taskID: currentTaskID, workspace: null, loading: false, error: loadError })
        }
      },
    )
    return () => controller.abort()
  }, [taskID])

  const create = useCallback(async () => {
    if (taskID === null) return
    setCreating(true)
    try {
			const created = await createTaskWorkspace(taskID)
			setState({ taskID, workspace: created, loading: false, error: null })
    } catch (createError: unknown) {
			setState({ taskID, workspace: null, loading: false, error: createError })
      throw createError
    } finally {
      setCreating(false)
    }
  }, [taskID])

	return useMemo(() => ({
		...(state.taskID === taskID ? state : { taskID, workspace: null, loading: taskID !== null, error: null }),
		creating,
		create,
	}), [create, creating, state, taskID])
}
