import { useCallback, useEffect, useState } from 'react'

import { approvePlan, createTopicPlan, getTopicPlan, requestPlanChanges } from '../../api/client'
import type { CreatePlanRevisionInput, PlanView, Task, Topic } from '../../types'

export function useTopicPlan(topicID: string | null) {
	const [state, setState] = useState<{ topicID: string | null; plan: PlanView | null; error: unknown }>({ topicID: null, plan: null, error: null })
	const [reloadToken, setReloadToken] = useState(0)
	const [submitting, setSubmitting] = useState(false)

	useEffect(() => {
		if (topicID === null) return
		const selectedTopicID = topicID
		const controller = new AbortController()
		async function load() {
			try {
				const plan = await getTopicPlan(selectedTopicID, controller.signal)
				if (!controller.signal.aborted) setState({ topicID: selectedTopicID, plan, error: null })
			} catch (error: unknown) {
				if (!controller.signal.aborted) setState({ topicID: selectedTopicID, plan: null, error })
			}
		}
		void load()
		return () => controller.abort()
	}, [reloadToken, topicID])

	const reload = useCallback(() => setReloadToken((current) => current + 1), [])
	const plan = state.topicID === topicID ? state.plan : null
	const error = state.topicID === topicID ? state.error : null
	const loading = topicID !== null && state.topicID !== topicID

	async function submit(topic: Topic, input: CreatePlanRevisionInput): Promise<PlanView> {
		return mutate(() => createTopicPlan(topic, input))
	}

	async function reject(topic: Topic, comment: string): Promise<PlanView> {
		if (plan === null) throw new Error('当前没有可审核的 Plan')
		return mutate(() => requestPlanChanges(topic, plan, comment))
	}

	async function approve(topic: Topic, comment: string): Promise<{ plan: PlanView; tasks: Task[] }> {
		if (plan === null) throw new Error('当前没有可审核的 Plan')
		setSubmitting(true)
		try {
			const result = await approvePlan(topic, plan, comment)
			setState({ topicID: topic.id, plan: result.plan, error: null })
			return result
		} catch (cause) {
			setState({ topicID: topic.id, plan, error: cause })
			throw cause
		} finally {
			setSubmitting(false)
		}
	}

	async function mutate(action: () => Promise<PlanView>): Promise<PlanView> {
		setSubmitting(true)
		try {
			const result = await action()
			setState({ topicID, plan: result, error: null })
			return result
		} catch (cause) {
			setState({ topicID, plan, error: cause })
			throw cause
		} finally {
			setSubmitting(false)
		}
	}

	return { plan, loading, submitting, error, reload, submit, reject, approve }
}
