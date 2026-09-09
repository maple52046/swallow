import { useCallback, useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { ServerRepository } from '@/application/ports/ServerRepository'
import type { Platform } from '@/domain/platform/types'
import type { Operation } from '@/domain/operation/types'
import type { Server } from '@/domain/server/types'

const LIFECYCLE_POLL_INTERVAL_MS = 5000

/**
 * Merges a Slurm deployment's controller (manager) nodes into the membership-scoped member
 * list. slurmrestd reports only compute (slurmd) nodes, so the platform-scoped Server list omits
 * controller-only nodes; without this the Members table and manager count would be missing them.
 * Controllers are looked up by the deployment intent's control-plane assignment ids and appended
 * only when not already present (a k0s control-plane node is already a member, so this is a
 * no-op there). A failed lookup is skipped so one unreadable Server cannot blank the page.
 */
async function withDeploymentControllers(
  platform: Platform,
  members: Server[],
  servers: ServerRepository,
): Promise<Server[]> {
  const present = new Set(members.map((server) => server.id))
  const missingControllerIds = (platform.deployment?.roleAssignments ?? [])
    .filter((assignment) => assignment.role === 'control-plane' && !present.has(assignment.serverId))
    .map((assignment) => assignment.serverId)
  if (missingControllerIds.length === 0) return members
  const fetched = await Promise.all(
    missingControllerIds.map((id) => servers.getServer(id).catch(() => null)),
  )
  const controllers = fetched.filter((server): server is Server => server !== null)
  return controllers.length === 0 ? members : [...members, ...controllers]
}

/** Combined Platform page projection; members and Operations may degrade to empty independently. */
export interface PlatformDetailData {
  platform: Platform
  /** Servers the platform's own API reports as members, read through the membership axis. */
  members: Server[]
  /** Operations concerning this platform, most recent first. */
  operations: Operation[]
  reload: () => void
}

/** Explicit loading, missing, failure, and usable states for the Platform route. */
export type PlatformDetailState =
  | { status: 'loading' }
  | { status: 'not-found' }
  | { status: 'error'; message: string }
  | { status: 'ready'; data: PlatformDetailData }

/** Only accepted automation states continue polling without an operator action. */
function lifecycleCanChangeWithoutInput(platform: Platform): boolean {
  return platform.lifecycleState === 'deploying' || platform.lifecycleState === 'uninstalling'
}

/**
 * Loads one Platform with members and related Operations, polling active lifecycle work.
 *
 * Membership and Operation reads degrade independently to empty collections so a provider
 * outage cannot hide the durable Platform record. While deploy or uninstall is active, the
 * full projection is refreshed every five seconds and the timer is always cleared when the
 * route unmounts or lifecycle reaches a terminal state.
 */
export function usePlatformDetail(id: string | undefined): PlatformDetailState {
  const { platforms, servers, operations } = useApp()
  const [state, setState] = useState<PlatformDetailState>(
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
        platforms.getPlatform(id),
        servers.listServers({ platformId: id, includeAbsent: true, pageSize: 200 }).then(
          (page) => page.items,
          () => [] as Server[],
        ),
        operations.listOperations({ platformId: id, pageSize: 50 }).then(
          (page) => page.items,
          () => [] as Operation[],
        ),
      ])
        .then(async ([platform, members, relatedOperations]) => {
          if (cancelled) return
          if (platform === null) {
            setState({ status: 'not-found' })
            return
          }
          // Slurm controllers are not scheduler members, so merge them in from the deployment
          // intent before publishing state; k0s is unaffected.
          const mergedMembers = await withDeploymentControllers(platform, members, servers)
          if (cancelled) return
          setState({
            status: 'ready',
            data: { platform, members: mergedMembers, operations: relatedOperations, reload },
          })
          if (lifecycleCanChangeWithoutInput(platform)) {
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
  }, [platforms, servers, operations, id, nonce, reload])

  return state
}
