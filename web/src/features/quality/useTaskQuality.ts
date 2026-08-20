import { useCallback, useEffect, useMemo, useState } from 'react'

import { createTaskEstimate, createTaskTestCase, createTaskTestResult, getTaskQuality } from '../../api/client'
import type { CreateTaskEstimateInput, CreateTaskTestCaseInput, CreateTaskTestResultInput, TaskQuality } from '../../types'

interface QualityState {
	taskID: string | null
	quality: TaskQuality | null
	error: unknown
}

const emptyState: QualityState = { taskID: null, quality: null, error: null }

export function useTaskQuality(taskID: string | null) {
	const [state, setState] = useState<QualityState>(emptyState)
	const [reloadToken, setReloadToken] = useState(0)
	const [busy, setBusy] = useState(false)
	useEffect(() => {
		if (taskID === null) return
		const selectedTaskID = taskID
		const controller = new AbortController()
		async function load() {
			try {
				const quality = await getTaskQuality(selectedTaskID, controller.signal)
				if (!controller.signal.aborted) setState({ taskID: selectedTaskID, quality, error: null })
			} catch (error: unknown) {
				if (!controller.signal.aborted) setState({ taskID: selectedTaskID, quality: null, error })
			}
		}
		void load()
		return () => controller.abort()
	}, [reloadToken, taskID])
	const reload = useCallback(() => setReloadToken((current) => current + 1), [])
	const mutate = useCallback(async (operation: (selectedTaskID: string) => Promise<unknown>) => {
		if (taskID === null) return
		setBusy(true)
		try { await operation(taskID); reload() } finally { setBusy(false) }
	}, [reload, taskID])
	return useMemo(() => ({
		...(state.taskID === taskID ? state : { taskID, quality: null, error: null }),
		loading: taskID !== null && state.taskID !== taskID,
		busy, reload,
		createEstimate: (input: CreateTaskEstimateInput) => mutate((id) => createTaskEstimate(id, input)),
		createTestCase: (input: CreateTaskTestCaseInput) => mutate((id) => createTaskTestCase(id, input)),
		recordResult: (testCaseID: string, input: CreateTaskTestResultInput) => mutate((id) => createTaskTestResult(id, testCaseID, input)),
	}), [busy, mutate, reload, state, taskID])
}
