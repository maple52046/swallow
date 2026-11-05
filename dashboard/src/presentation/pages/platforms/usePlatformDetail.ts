import { useCallback, useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { ServerRepository } from '@/application/ports/ServerRepository'
import { isGPUOperatorInProgress } from '@/domain/platform/gpuOperator'
import { isPlatformLifecycleInProgress } from '@/domain/platform/lifecycle'
import { platformDeploymentControllerIds } from '@/domain/platform/membership'
import { platformConvergesWithoutInput } from '@/domain/platform/spec'
import type { Platform } from '@/domain/platform/types'
import type { Operation } from '@/domain/operation/types'
import type { Server } from '@/domain/server/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'

const LIFECYCLE_POLL_INTERVAL_MS = 5000
// How long the page keeps re-reading after a reload that follows a change on it (an uninstall or a
// GPU Operator change that started). The controller acts within seconds of the write, but the
// first re-read can come before it, when the Platform still reads as it did; this window covers
// that gap without polling a settled Platform for long.
const FOLLOW_CHANGE_MS = 30_000

/**
 * Merges a Slurm deployment's controller (manager) nodes into the membership-scoped member
 * list. slurmrestd reports only compute (slurmd) nodes, so the platform-scoped Server list omits
 * controller-only nodes; without this the Members table and manager count would be missing them.
 * Controllers are looked up by `platformDeploymentControllerIds` (the same rule the Platforms list
 * counts with) and appended only when not already present (a k0s control-plane node is already a
 * member, so this is a no-op there). A failed lookup is skipped so one unreadable Server cannot
 * blank the page.
 */
async function withDeploymentControllers(
  platform: Platform,
  members: Server[],
  servers: ServerRepository,
): Promise<Server[]> {
  const present = new Set(members.map((server) => server.id))
  const missingControllerIds = platformDeploymentControllerIds(platform).filter((serverId) => !present.has(serverId))
  if (missingControllerIds.length === 0) return members
  const fetched = await Promise.all(
    missingControllerIds.map((id) => servers.getServer(id).catch(() => null)),
  )
  const controllers = fetched.filter((server): server is Server => server !== null)
  return controllers.length === 0 ? members : [...members, ...controllers]
}

/**
 * Fetches the Slurm login (submission) hosts named by the deployment intent. Login hosts run no
 * cluster daemon and carry no membership, so they are not in the member list; the Slurm view
 * shows their address as the cluster access point. A failed lookup is skipped so one unreadable
 * Server cannot blank the page, and a non-Slurm platform yields an empty list.
 */
async function fetchLoginNodes(platform: Platform, servers: ServerRepository): Promise<Server[]> {
  const ids = platform.deployment?.loginServerIds ?? []
  if (ids.length === 0) return []
  const fetched = await Promise.all(ids.map((id) => servers.getServer(id).catch(() => null)))
  return fetched.filter((server): server is Server => server !== null)
}

/** Combined Platform page projection; members and Operations may degrade to empty independently. */
export interface PlatformDetailData {
  platform: Platform
  /** Servers the platform's own API reports as members, read through the membership axis. */
  members: Server[]
  /** Slurm login (submission) hosts from the deployment intent; empty otherwise. */
  loginNodes: Server[]
  /** Operations concerning this platform, most recent first. */
  operations: Operation[]
  reload: () => void
}

/**
 * Explicit loading, missing, failure, and usable states for the Platform route; a failure carries
 * the failed read's message and request ID.
 */
export type PlatformDetailState =
  | { status: 'loading' }
  | { status: 'not-found' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; data: PlatformDetailData }

/**
 * Only accepted automation states continue polling without a user action: a running
 * deployment or uninstall, a running GPU Operator change, or a newer spec the controller is about
 * to carry out (an approved Change Plan whose Workflow is not written yet).
 */
function lifecycleCanChangeWithoutInput(platform: Platform): boolean {
  return isPlatformLifecycleInProgress(platform.lifecycleState) ||
    (platform.gpuOperator !== null && isGPUOperatorInProgress(platform.gpuOperator.state)) ||
    platformConvergesWithoutInput(platform)
}

/**
 * Loads one Platform with members and related Operations, polling active lifecycle work.
 *
 * Membership and Operation reads degrade independently to empty collections so a provider
 * outage cannot hide the durable Platform record. While deploy, uninstall, or a GPU Operator
 * change is active, or an approved change is about to start, the full projection is refreshed every
 * five seconds and the timer is always cleared when the route unmounts or that work reaches a
 * terminal state. `reload` also keeps the page re-reading for a short window, because it follows a
 * change made on the page whose Workflow the controller may not have written yet; a Platform that
 * is gone by then (an uninstall that already deleted the record) reads as not found.
 */
export function usePlatformDetail(id: string | undefined): PlatformDetailState {
  const { platforms, servers, operations } = useApp()
  const [state, setState] = useState<PlatformDetailState>(
    id ? { status: 'loading' } : { status: 'not-found' },
  )
  const [nonce, setNonce] = useState(0)
  // Until when a reload keeps polling a Platform that does not read as changing (epoch ms).
  const [followUntil, setFollowUntil] = useState(0)
  const reload = useCallback(() => {
    setFollowUntil(Date.now() + FOLLOW_CHANGE_MS)
    setNonce((value) => value + 1)
  }, [])

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
          // intent before publishing state; k0s is unaffected. Login hosts are fetched
          // separately (they carry no membership) for the cluster access point.
          const [mergedMembers, loginNodes] = await Promise.all([
            withDeploymentControllers(platform, members, servers),
            fetchLoginNodes(platform, servers),
          ])
          if (cancelled) return
          setState({
            status: 'ready',
            data: { platform, members: mergedMembers, loginNodes, operations: relatedOperations, reload },
          })
          if (lifecycleCanChangeWithoutInput(platform) || Date.now() < followUntil) {
            timer = setTimeout(load, LIFECYCLE_POLL_INTERVAL_MS)
          }
        })
        .catch((err: Error) => {
          if (!cancelled) setState({ status: 'error', ...failureOf(err, 'The Platform could not be loaded.') })
        })
    }

    load()

    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [platforms, servers, operations, id, nonce, followUntil, reload])

  return state
}
