import { useCallback, useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { ListOperationsFilters, Operation } from '@/domain/operation/types'

/** Discriminated union so a list cannot be simultaneously loading and errored. */
export type OperationsState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; operations: Operation[]; total: number }

/**
 * Loads a page of operations for the list screen.
 *
 * A stale-guard drops out-of-order responses when the filters change quickly, and `reload`
 * refetches after an action such as a retry. Filters are passed through to the backend,
 * which owns filtering; the hook adds no client-side narrowing of its own.
 */
export function useOperations(filters: ListOperationsFilters): {
  state: OperationsState
  reload: () => void
} {
  const { operations } = useApp()
  const [state, setState] = useState<OperationsState>({ status: 'loading' })
  const [nonce, setNonce] = useState(0)

  const reload = useCallback(() => setNonce((value) => value + 1), [])

  // Filters are spread into the dependency list by value so a new object identity with the
  // same contents does not refetch.
  const { siteId, platformId, serverId, kind, status, active, page, pageSize } = filters

  useEffect(() => {
    let cancelled = false

    operations
      .listOperations({ siteId, platformId, serverId, kind, status, active, page, pageSize })
      .then((result) => {
        if (cancelled) return
        setState({ status: 'ready', operations: result.items, total: result.total })
      })
      .catch((err: Error) => {
        if (!cancelled) setState({ status: 'error', message: err.message })
      })

    return () => {
      cancelled = true
    }
  }, [operations, siteId, platformId, serverId, kind, status, active, page, pageSize, nonce])

  return { state, reload }
}
