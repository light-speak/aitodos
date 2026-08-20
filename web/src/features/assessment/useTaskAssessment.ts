import { useCallback, useEffect, useMemo, useState } from 'react'

import { getTaskAssessment, updateTaskTitle } from '../../api/client'
import type { Task, TaskAssessmentState } from '../../types'

interface AssessmentState {
	taskID: string | null
	assessment: TaskAssessmentState | null
	error: unknown
}

const emptyState: AssessmentState = { taskID: null, assessment: null, error: null }

export function useTaskAssessment(taskID: string | null) {
	const [state, setState] = useState<AssessmentState>(emptyState)
	const [reloadToken, setReloadToken] = useState(0)
	const [busy, setBusy] = useState(false)
	useEffect(() => {
		if (taskID === null) return
		const selectedTaskID = taskID
		const controller = new AbortController()
		void getTaskAssessment(selectedTaskID, controller.signal).then(
			(assessment) => {
				if (!controller.signal.aborted) setState({ taskID: selectedTaskID, assessment, error: null })
			},
			(error: unknown) => {
				if (!controller.signal.aborted) setState({ taskID: selectedTaskID, assessment: null, error })
			},
		)
		return () => controller.abort()
	}, [reloadToken, taskID])
	const reload = useCallback(() => setReloadToken((current) => current + 1), [])
	const updateTitle = useCallback(async (task: Task, title: string) => {
		setBusy(true)
		try {
			const updated = await updateTaskTitle(task.id, title, task.version)
			reload()
			return updated
		} finally {
			setBusy(false)
		}
	}, [reload])
	return useMemo(() => ({
		...(state.taskID === taskID ? state : { taskID, assessment: null, error: null }),
		loading: taskID !== null && state.taskID !== taskID,
		busy, reload, updateTitle,
	}), [busy, reload, state, taskID, updateTitle])
}
