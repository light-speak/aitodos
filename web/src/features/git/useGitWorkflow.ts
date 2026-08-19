import { useCallback, useEffect, useMemo, useState } from 'react'

import { createRelease, getReleases, getRepositoryInfo } from '../../api/client'
import type { CreateReleaseInput, Release, RepositoryInfo } from '../../types'

interface GitWorkflowState {
  repository: RepositoryInfo | null
  releases: Release[]
  loading: boolean
  error: unknown
}

const initialState: GitWorkflowState = { repository: null, releases: [], loading: true, error: null }

export function useGitWorkflow() {
  const [state, setState] = useState(initialState)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    void loadGitWorkflow(controller.signal).then(
      ({ repository, releases }) => setState({ repository, releases, loading: false, error: null }),
      (error: unknown) => {
        if (!controller.signal.aborted) setState((current) => ({ ...current, loading: false, error }))
      },
    )
    return () => controller.abort()
  }, [])

  const create = useCallback(async (input: CreateReleaseInput) => {
    setSubmitting(true)
    try {
      const created = await createRelease(input)
      setState((current) => ({
				...current,
        releases: [created, ...current.releases.filter((item) => item.id !== created.id)],
        loading: false,
        error: null,
      }))
			try {
				const repository = await getRepositoryInfo()
				setState((current) => ({ ...current, repository }))
			} catch (refreshError: unknown) {
				setState((current) => ({ ...current, error: refreshError }))
			}
      return created
    } finally {
      setSubmitting(false)
    }
  }, [])

  return useMemo(() => ({ ...state, submitting, createRelease: create }), [create, state, submitting])
}

async function loadGitWorkflow(signal: AbortSignal) {
  const [repository, releases] = await Promise.all([getRepositoryInfo(signal), getReleases(signal)])
  return { repository, releases }
}
