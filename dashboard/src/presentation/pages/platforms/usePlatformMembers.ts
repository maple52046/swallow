import { useCallback, useEffect, useState } from 'react'
import type { ServerRepository } from '@/application/ports/ServerRepository'
import { useApp } from '@/di/AppProvider'
import { platformDeploymentControllerIds } from '@/domain/platform/membership'
import type { Platform } from '@/domain/platform/types'
import type { Server } from '@/domain/server/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'

/** Route-owned Platform members plus non-member Slurm login nodes. */
export interface PlatformMembersData {
  members: Server[]
  loginNodes: Server[]
  warning?: DisplayedFailure
  reload: () => void
}

/** Explicit members loading state; a failed primary list is never represented as no members. */
export type PlatformMembersState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure & { reload: () => void })
  | { status: 'ready'; data: PlatformMembersData }

async function resolveServers(ids: readonly string[], servers: ServerRepository): Promise<{
  items: Server[]
  failure?: DisplayedFailure
}> {
  const settled = await Promise.allSettled(ids.map((id) => servers.getServer(id)))
  const items = settled
    .filter((result): result is PromiseFulfilledResult<Server | null> => result.status === 'fulfilled')
    .map((result) => result.value)
    .filter((server): server is Server => server !== null)
  const rejected = settled.find((result): result is PromiseRejectedResult => result.status === 'rejected')
  return rejected
    ? { items, failure: failureOf(rejected.reason, 'Some Platform Servers could not be loaded.') }
    : { items }
}

/**
 * Loads membership only for routes that render it. Controller/login lookups publish successful
 * rows with a warning when one lookup fails, rather than silently dropping it or blanking the
 * durable membership list.
 */
export function usePlatformMembers(platform: Platform, siteId: string | undefined): PlatformMembersState {
  const { servers } = useApp()
  const [revision, setRevision] = useState(0)
  const reload = useCallback(() => setRevision((value) => value + 1), [])
  const [state, setState] = useState<PlatformMembersState>({ status: 'loading' })
  const lookupSignature = JSON.stringify({
    controllerIds: platformDeploymentControllerIds(platform),
    loginIds: platform.deployment?.loginServerIds ?? [],
  })

  useEffect(() => {
    let cancelled = false
    queueMicrotask(() => { if (!cancelled) setState({ status: 'loading' }) })

    const load = async () => {
      try {
        const lookup = JSON.parse(lookupSignature) as { controllerIds: string[]; loginIds: string[] }
        const page = await servers.listServers({ platformId: platform.id, includeAbsent: true, pageSize: 200 })
        const members = page.items
        const present = new Set(members.map((server) => server.id))
        const controllerIds = lookup.controllerIds.filter((id) => !present.has(id))
        const [controllers, loginNodes] = await Promise.all([
          resolveServers(controllerIds, servers),
          resolveServers(lookup.loginIds, servers),
        ])
        if (cancelled) return
        setState({
          status: 'ready',
          data: {
            members: [...members, ...controllers.items],
            loginNodes: loginNodes.items,
            warning: controllers.failure ?? loginNodes.failure,
            reload,
          },
        })
      } catch (error) {
        if (!cancelled) setState({ status: 'error', ...failureOf(error, 'Platform members could not be loaded.'), reload })
      }
    }

    void load()
    return () => {
      cancelled = true
    }
  }, [platform.id, lookupSignature, reload, revision, servers, siteId])

  return state
}
