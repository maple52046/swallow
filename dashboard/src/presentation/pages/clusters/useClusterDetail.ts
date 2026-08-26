import { useCallback, useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Cluster } from '@/domain/cluster/types'
import type { Operation } from '@/domain/operation/types'
import type { Server } from '@/domain/server/types'

export interface ClusterDetailData {
  cluster: Cluster
  /** Servers the cluster's own API reports as members, read through the membership axis. */
  members: Server[]
  /** Operations concerning this cluster, most recent first. */
  operations: Operation[]
  reload: () => void
}

export type ClusterDetailState =
  | { status: 'loading' }
  | { status: 'not-found' }
  | { status: 'error'; message: string }
  | { status: 'ready'; data: ClusterDetailData }

/**
 * Loads one cluster with its members and related operations.
 *
 * A cluster's members are servers carrying the membership axis, so they come from the
 * server list filtered by cluster rather than a members endpoint; that keeps the cluster's
 * own API authoritative about membership. Members and operations fail independently of the
 * cluster read and degrade to empty rather than failing the page. `reload` refetches all
 * three after a sync or a deployment.
 */
export function useClusterDetail(id: string | undefined): ClusterDetailState {
  const { clusters, servers, operations } = useApp()
  const [state, setState] = useState<ClusterDetailState>(
    id ? { status: 'loading' } : { status: 'not-found' },
  )
  const [nonce, setNonce] = useState(0)
  const reload = useCallback(() => setNonce((value) => value + 1), [])

  useEffect(() => {
    if (!id) return
    let cancelled = false

    Promise.all([
      clusters.getCluster(id),
      servers.listServers({ clusterId: id, includeAbsent: true, pageSize: 200 }).then(
        (page) => page.items,
        () => [] as Server[],
      ),
      operations.listOperations({ clusterId: id, pageSize: 50 }).then(
        (page) => page.items,
        () => [] as Operation[],
      ),
    ])
      .then(([cluster, members, ops]) => {
        if (cancelled) return
        if (cluster === null) {
          setState({ status: 'not-found' })
          return
        }
        setState({ status: 'ready', data: { cluster, members, operations: ops, reload } })
      })
      .catch((err: Error) => {
        if (!cancelled) setState({ status: 'error', message: err.message })
      })

    return () => {
      cancelled = true
    }
  }, [clusters, servers, operations, id, nonce, reload])

  return state
}
