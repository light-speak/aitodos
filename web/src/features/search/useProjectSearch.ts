import { useCallback, useState } from 'react'

import { searchProject } from '../../api/client'
import type { SearchItem, SearchQueryInput } from '../../types'

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
	return { ...state, search, loadMore, reset }
}

function mergeSearchItems(current: SearchItem[], incoming: SearchItem[]): SearchItem[] {
	const ids = new Set(current.map((item) => item.document_id))
	return [...current, ...incoming.filter((item) => !ids.has(item.document_id))]
}
