import { useCallback, useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import {
  loadServerWorkingSet,
  type ServerWorkingSet,
  type WorkingSetQuery,
} from '@/application/usecases/servers/loadServerWorkingSet'

/** Discriminated state so loading/ready/error cannot contradict each other. */
export type WorkingSetState =
  | { status: 'loading' }
  | { status: 'ready'; data: ServerWorkingSet }
  | { status: 'error'; message: string }

/**
 * Loads the servers working set for the list and re-runs when the coarse query changes.
 *
 * The coarse query (`keyword`, `includeAbsent`) is what the API filters server-side; the
 * page applies grouping and the multi-dimension filter to the returned set. A stale-guard
 * ignores out-of-order responses so fast typing cannot let an earlier result overwrite a
 * later one, and `reload` refetches after an action changes the fleet.
 */
export function useServerWorkingSet(query: WorkingSetQuery): {
  state: WorkingSetState
  reload: () => void
} {
  const { servers } = useApp()
  const [state, setState] = useState<WorkingSetState>({ status: 'loading' })
  const [nonce, setNonce] = useState(0)

  const reload = useCallback(() => setNonce((value) => value + 1), [])

  // Destructured so the effect depends on the primitive query fields, not a fresh object
  // identity each render.
  const { siteId, keyword, includeAbsent } = query

  useEffect(() => {
    let cancelled = false

    loadServerWorkingSet(servers, { siteId, keyword, includeAbsent })
      .then((data) => {
        if (!cancelled) setState({ status: 'ready', data })
      })
      .catch((err: Error) => {
        if (!cancelled) setState({ status: 'error', message: err.message })
      })

    return () => {
      cancelled = true
    }
  }, [servers, siteId, keyword, includeAbsent, nonce])

  return { state, reload }
}
