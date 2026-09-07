import { useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Platform } from '@/domain/platform/types'
import type { Site } from '@/domain/site/types'
import type { Server } from '@/domain/server/types'
import { loadServerWorkingSet } from '@/application/usecases/servers/loadServerWorkingSet'

export interface DeployableData {
  sites: Site[]
  /** Deployed Servers stay visible even when an existing Platform makes them unavailable. */
  servers: Server[]
  /** Site Platforms resolve observed membership IDs into operator-readable context. */
  platforms: Platform[]
  /** Durable non-uninstalled deployment targets keyed by Server ID. */
  deploymentClaims: Record<string, Platform>
}

export type DeployableState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; data: DeployableData }

/**
 * Loads the sites and the deployable servers for the deployment wizard.
 *
 * Servers in any of the requested preparation states are listed and merged (deduped by id),
 * so a convergent deploy can mix pool-ready Servers (provisioned first) with already-deployed
 * ones (reused as-is). Non-uninstalled deployment target snapshots and observed membership are
 * also loaded so occupied Servers remain visible with context but cannot be assigned a role.
 * Claim reads fail closed: without durable target history the wizard must not offer a Server
 * that may contain a partial platform. A stale-guard drops out-of-order responses whenever
 * Site scope changes.
 *
 * `provisioningStates` must be a stable reference (e.g. a module constant) because it keys the
 * load effect; an inline array literal would refetch on every render.
 */
export function useDeployableServers(
  siteId: string | undefined,
  provisioningStates: readonly string[] = ['deployed'],
): DeployableState {
  const { sites, servers, platforms, operations } = useApp()
  const [state, setState] = useState<DeployableState>({ status: 'loading' })

  useEffect(() => {
    let cancelled = false

    const loadSites = sites.listSites()
    const loadCandidates = async (): Promise<Omit<DeployableData, 'sites'>> => {
      if (!siteId) {
        return { servers: [], platforms: [], deploymentClaims: {} }
      }
      const [workingSets, sitePlatforms] = await Promise.all([
        Promise.all(
          provisioningStates.map((provisioningState) =>
            loadServerWorkingSet(servers, { siteId, provisioningState }),
          ),
        ),
        platforms.listPlatforms(siteId),
      ])
      // Merge the per-state working sets, deduping by id so a Server that changed state
      // between the parallel reads is listed once. Locked Servers are protected from mutation
      // and can never be a deployment target, so they are excluded from the candidate list
      // entirely rather than shown as an unusable row.
      const byId = new Map<string, Server>()
      for (const workingSet of workingSets) {
        for (const server of workingSet.servers) byId.set(server.id, server)
      }
      const mergedServers = [...byId.values()].filter((server) => !server.provisioning?.locked)
      const claimedPlatforms = sitePlatforms.filter((platform) => (
        platform.origin === 'deployed' &&
        platform.lifecycleState !== 'uninstalled' &&
        platform.lifecycleOperationId
      ))
      const deploymentClaims: Record<string, Platform> = {}
      const claimSources = await Promise.all(claimedPlatforms.map(async (platform) => {
        const operationId = platform.lifecycleOperationId
        if (!operationId) {
          throw new Error(`Deployment lifecycle for ${platform.name} has no Operation.`)
        }
        const operation = await operations.getOperation(operationId)
        if (!operation) {
          throw new Error(`Deployment target history for ${platform.name} is unavailable.`)
        }
        return { platform, operation }
      }))
      for (const source of claimSources) {
        for (const serverId of source.operation.targetServerIds) {
          // Preserve Platform list order if corrupt history claims one Server twice; backend
          // preflight still refuses the target, while the UI gives one stable explanation.
          if (!deploymentClaims[serverId]) {
            deploymentClaims[serverId] = source.platform
          }
        }
      }
      return {
        servers: mergedServers,
        platforms: sitePlatforms,
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
  }, [sites, servers, platforms, operations, siteId, provisioningStates])

  return state
}
