import { useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Platform } from '@/domain/platform/types'
import type { Site } from '@/domain/site/types'
import type { Server } from '@/domain/server/types'
import { loadServerWorkingSet } from '@/application/usecases/servers/loadServerWorkingSet'

export interface DeployableData {
  sites: Site[]
  /**
   * Deployed Servers stay visible even when an existing Platform makes them unavailable: each
   * carries its Platform Claim, which marks it as another Platform's target.
   */
  servers: Server[]
  /** Site Platforms resolve Platform Claim and observed membership IDs into operator-readable context. */
  platforms: Platform[]
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
 * ones (reused as-is). Claimed or member Servers remain visible with context but cannot be
 * assigned a role; the Site's Platforms are loaded to name them. A stale-guard drops
 * out-of-order responses whenever Site scope changes.
 *
 * `provisioningStates` must be a stable reference (e.g. a module constant) because it keys the
 * load effect; an inline array literal would refetch on every render.
 * `refreshKey` intentionally invalidates the load after a stale-policy deployment rejection.
 */
export function useDeployableServers(
  siteId: string | undefined,
  provisioningStates: readonly string[] = ['deployed'],
  refreshKey = 0,
): DeployableState {
  const { sites, servers, platforms } = useApp()
  const [state, setState] = useState<DeployableState>({ status: 'loading' })

  useEffect(() => {
    let cancelled = false

    const loadSites = sites.listSites()
    const loadCandidates = async (): Promise<Omit<DeployableData, 'sites'>> => {
      if (!siteId) {
        return { servers: [], platforms: [] }
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
      return {
        servers: [...byId.values()].filter((server) => !server.status.provisioning?.locked),
        platforms: sitePlatforms,
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
  }, [sites, servers, platforms, siteId, provisioningStates, refreshKey])

  return state
}
