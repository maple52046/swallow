import { useCallback, useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Cluster } from '@/domain/cluster/types'

/** Discriminated union so the list is never both loading and loaded. */
export type ClustersState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; clusters: Cluster[] }

/**
 * Loads the cluster list, optionally scoped to one site.
 *
 * A stale-guard drops out-of-order responses, and `reload` refetches after an action such
 * as a deployment. The list is small and bounded, so it is fetched whole rather than paged.
 */
export function useClusters(siteId?: string): { state: ClustersState; reload: () => void } {
  const { clusters } = useApp()
  const [state, setState] = useState<ClustersState>({ status: 'loading' })
  const [nonce, setNonce] = useState(0)
  const reload = useCallback(() => setNonce((value) => value + 1), [])

  useEffect(() => {
    let cancelled = false
    clusters
      .listClusters(siteId)
      .then((result) => {
        if (!cancelled) setState({ status: 'ready', clusters: result })
      })
      .catch((err: Error) => {
        if (!cancelled) setState({ status: 'error', message: err.message })
      })
    return () => {
      cancelled = true
    }
  }, [clusters, siteId, nonce])

  return { state, reload }
}
