import { useCallback, useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Cluster } from '@/domain/cluster/types'
import type { Operation } from '@/domain/operation/types'
import type { Server } from '@/domain/server/types'

const LIFECYCLE_POLL_INTERVAL_MS = 5000

/** Combined Cluster page projection; members and Operations may degrade to empty independently. */
export interface ClusterDetailData {
  cluster: Cluster
  /** Servers the cluster's own API reports as members, read through the membership axis. */
  members: Server[]
  /** Operations concerning this cluster, most recent first. */
  operations: Operation[]
  reload: () => void
}

/** Explicit loading, missing, failure, and usable states for the Cluster route. */
export type ClusterDetailState =
  | { status: 'loading' }
  | { status: 'not-found' }
  | { status: 'error'; message: string }
  | { status: 'ready'; data: ClusterDetailData }

/** Only accepted automation states continue polling without an operator action. */
function lifecycleCanChangeWithoutInput(cluster: Cluster): boolean {
  return cluster.lifecycleState === 'deploying' || cluster.lifecycleState === 'uninstalling'
}

/**
 * Loads one Cluster with members and related Operations, polling active lifecycle work.
 *
 * Membership and Operation reads degrade independently to empty collections so a provider
 * outage cannot hide the durable Cluster record. While deploy or uninstall is active, the
 * full projection is refreshed every five seconds and the timer is always cleared when the
 * route unmounts or lifecycle reaches a terminal state.
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
    let timer: ReturnType<typeof setTimeout> | undefined

    const load = () => {
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
        .then(([cluster, members, relatedOperations]) => {
          if (cancelled) return
          if (cluster === null) {
            setState({ status: 'not-found' })
            return
          }
          setState({
            status: 'ready',
            data: { cluster, members, operations: relatedOperations, reload },
          })
          if (lifecycleCanChangeWithoutInput(cluster)) {
            timer = setTimeout(load, LIFECYCLE_POLL_INTERVAL_MS)
          }
        })
        .catch((err: Error) => {
          if (!cancelled) setState({ status: 'error', message: err.message })
        })
    }

    load()

    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [clusters, servers, operations, id, nonce, reload])

  return state
}
