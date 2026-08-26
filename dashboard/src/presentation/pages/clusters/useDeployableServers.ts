import { useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Site } from '@/domain/site/types'
import type { Server } from '@/domain/server/types'

export interface DeployableData {
  sites: Site[]
  /** Deployed servers at the selected site; empty until a site is chosen. */
  servers: Server[]
}

export type DeployableState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; data: DeployableData }

/**
 * Loads the sites and the deployable servers for the deployment wizard.
 *
 * Only `deployed` servers can join a cluster, so the server read filters on that state; the
 * backend enforces the same rule, this just avoids offering ineligible servers. Servers are
 * re-read whenever the selected site changes. A stale-guard drops out-of-order responses.
 */
export function useDeployableServers(siteId: string | undefined): DeployableState {
  const { sites, servers } = useApp()
  const [state, setState] = useState<DeployableState>({ status: 'loading' })

  useEffect(() => {
    let cancelled = false

    const loadSites = sites.listSites()
    const loadServers = siteId
      ? servers
          .listServers({ siteId, provisioningState: 'deployed', pageSize: 200 })
          .then((page) => page.items)
      : Promise.resolve([] as Server[])

    Promise.all([loadSites, loadServers])
      .then(([siteList, serverList]) => {
        if (cancelled) return
        setState({ status: 'ready', data: { sites: siteList, servers: serverList } })
      })
      .catch((err: Error) => {
        if (!cancelled) setState({ status: 'error', message: err.message })
      })

    return () => {
      cancelled = true
    }
  }, [sites, servers, siteId])

  return state
}
