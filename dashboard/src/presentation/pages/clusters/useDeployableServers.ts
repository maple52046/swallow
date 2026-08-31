import { useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Cluster } from '@/domain/cluster/types'
import type { Site } from '@/domain/site/types'
import type { Server } from '@/domain/server/types'
import { loadServerWorkingSet } from '@/application/usecases/servers/loadServerWorkingSet'

export interface DeployableData {
  sites: Site[]
  /** Deployed Servers stay visible even when an existing Cluster makes them unavailable. */
  servers: Server[]
  /** Site Clusters resolve observed membership IDs into operator-readable context. */
  clusters: Cluster[]
  /** Durable non-uninstalled deployment targets keyed by Server ID. */
  deploymentClaims: Record<string, Cluster>
}

export type DeployableState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; data: DeployableData }

/**
 * Loads the sites and the deployable servers for the deployment wizard.
 *
 * Only `deployed` servers are listed. Non-uninstalled deployment target snapshots and
 * observed membership are also loaded so occupied Servers remain visible with context but
 * cannot be assigned a role. Claim reads fail closed: without durable target history the
 * wizard must not offer a Server that may contain a partial cluster. A stale-guard drops
 * out-of-order responses whenever Site scope changes.
 */
export function useDeployableServers(siteId: string | undefined): DeployableState {
  const { sites, servers, clusters, operations } = useApp()
  const [state, setState] = useState<DeployableState>({ status: 'loading' })

  useEffect(() => {
    let cancelled = false

    const loadSites = sites.listSites()
    const loadCandidates = async (): Promise<Omit<DeployableData, 'sites'>> => {
      if (!siteId) {
        return { servers: [], clusters: [], deploymentClaims: {} }
      }
      const [workingSet, siteClusters] = await Promise.all([
        loadServerWorkingSet(servers, { siteId, provisioningState: 'deployed' }),
        clusters.listClusters(siteId),
      ])
      const claimedClusters = siteClusters.filter((cluster) => (
        cluster.origin === 'deployed' &&
        cluster.lifecycleState !== 'uninstalled' &&
        cluster.lifecycleOperationId
      ))
      const deploymentClaims: Record<string, Cluster> = {}
      const claimSources = await Promise.all(claimedClusters.map(async (cluster) => {
        const operationId = cluster.lifecycleOperationId
        if (!operationId) {
          throw new Error(`Deployment lifecycle for ${cluster.name} has no Operation.`)
        }
        const operation = await operations.getOperation(operationId)
        if (!operation) {
          throw new Error(`Deployment target history for ${cluster.name} is unavailable.`)
        }
        return { cluster, operation }
      }))
      for (const source of claimSources) {
        for (const serverId of source.operation.targetServerIds) {
          // Preserve Cluster list order if corrupt history claims one Server twice; backend
          // preflight still refuses the target, while the UI gives one stable explanation.
          if (!deploymentClaims[serverId]) {
            deploymentClaims[serverId] = source.cluster
          }
        }
      }
      return {
        servers: workingSet.servers,
        clusters: siteClusters,
        deploymentClaims,
      }
    }

    Promise.all([loadSites, loadCandidates()])
      .then(([siteList, candidates]) => {
        if (cancelled) return
        setState({ status: 'ready', data: { sites: siteList, ...candidates } })
      })
      .catch((err: Error) => {
        if (!cancelled) setState({ status: 'error', message: err.message })
      })

    return () => {
      cancelled = true
    }
  }, [sites, servers, clusters, operations, siteId])

  return state
}
