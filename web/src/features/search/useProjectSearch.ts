import { useCallback, useState } from 'react'

import {
	addRetrievalEvalResult,
	getRetrievalEvalCases,
	getRetrievalEvalRuns,
	removeRetrievalEvalResult,
	runRetrievalEval,
	searchProject,
} from '../../api/client'
import type { CreateRetrievalEvalCaseInput, RetrievalEvalCase, RetrievalEvalRun, SearchItem, SearchQueryInput } from '../../types'

interface SearchState {
	items: SearchItem[]
	nextCursor?: string
	lastInput?: SearchQueryInput
	loading: boolean
	error: unknown
}

const initialState: SearchState = { items: [], loading: false, error: null }

export function useProjectSearch() {
	const [state, setState] = useState<SearchState>(initialState)
	const [evalCases, setEvalCases] = useState<RetrievalEvalCase[]>([])
	const [evalRuns, setEvalRuns] = useState<RetrievalEvalRun[]>([])
	const [evalLoading, setEvalLoading] = useState(false)
	const [evalError, setEvalError] = useState<unknown>(null)
	const search = useCallback(async (input: SearchQueryInput) => {
		setState((current) => ({ ...current, loading: true, error: null }))
		try {
			const page = await searchProject(input)
			setState({ items: page.items, nextCursor: page.next_cursor, lastInput: input, loading: false, error: null })
		} catch (error: unknown) {
			setState((current) => ({ ...current, loading: false, error }))
		}
	}, [])
	const loadMore = useCallback(async () => {
		if (state.loading || !state.nextCursor || !state.lastInput) return
		setState((current) => ({ ...current, loading: true, error: null }))
		try {
			const page = await searchProject({ ...state.lastInput, cursor: state.nextCursor })
			setState((current) => ({
				...current, items: mergeSearchItems(current.items, page.items),
				nextCursor: page.next_cursor, loading: false, error: null,
			}))
		} catch (error: unknown) {
			setState((current) => ({ ...current, loading: false, error }))
		}
	}, [state.lastInput, state.loading, state.nextCursor])
	const reset = useCallback(() => setState(initialState), [])
	const loadEvals = useCallback(async () => {
		setEvalLoading(true)
		setEvalError(null)
		try {
			const [cases, runs] = await Promise.all([getRetrievalEvalCases(), getRetrievalEvalRuns()])
			setEvalCases(cases)
			setEvalRuns(runs)
		} catch (error: unknown) {
			setEvalError(error)
		} finally {
			setEvalLoading(false)
		}
	}, [])
	const addEvalResult = useCallback(async (input: CreateRetrievalEvalCaseInput) => {
		setEvalLoading(true)
		setEvalError(null)
		try {
			const updated = await addRetrievalEvalResult(input)
			setEvalCases((current) => [updated, ...current.filter((item) => item.id !== updated.id)])
		} catch (error: unknown) {
			setEvalError(error)
		} finally {
			setEvalLoading(false)
		}
	}, [])
	const removeEvalResult = useCallback(async (caseID: string, documentID: string) => {
		setEvalLoading(true)
		setEvalError(null)
		try {
			await removeRetrievalEvalResult(caseID, documentID)
			setEvalCases(await getRetrievalEvalCases())
		} catch (error: unknown) {
			setEvalError(error)
		} finally {
			setEvalLoading(false)
		}
	}, [])
	const runEval = useCallback(async (k: number) => {
		setEvalLoading(true)
		setEvalError(null)
		try {
			const created = await runRetrievalEval(k)
			setEvalRuns((current) => [created, ...current])
		} catch (error: unknown) {
			setEvalError(error)
		} finally {
			setEvalLoading(false)
		}
	}, [])
	return {
		...state, search, loadMore, reset,
		evalCases, evalRuns, evalLoading, evalError, loadEvals, addEvalResult, removeEvalResult, runEval,
	}
}

function mergeSearchItems(current: SearchItem[], incoming: SearchItem[]): SearchItem[] {
	const ids = new Set(current.map((item) => item.document_id))
	return [...current, ...incoming.filter((item) => !ids.has(item.document_id))]
}
