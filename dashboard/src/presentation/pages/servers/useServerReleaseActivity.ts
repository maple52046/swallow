import { useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import { findServerReleaseWorkflow, type Operation, type ServerReleaseWorkflow } from '@/domain/operation/types'

/**
 * How often a releasing Server's Workflows are re-read. A release (with disk erasure) takes
 * minutes, and the Workflow changes phase rarely (it parks for attention only after its observation
 * timeout), so this is slower than the detail page's 2-second projection refresh.
 */
const RELEASE_ACTIVITY_POLL_MS = 10_000
/** Newest-first Workflows read per poll; the release behind the current state is among the latest. */
const RELEASE_ACTIVITY_PAGE_SIZE = 50

/**
 * What Server detail knows about the Swallow work behind a `releasing` Server:
 *
 * - `idle`: the Server is not releasing, so nothing was read.
 * - `loading`: the first read for this release is in flight.
 * - `error`: the Workflows could not be read and there is no earlier result to keep showing.
 * - `ready`: `workflow` is the Workflow behind the release, or `null` when none covers it.
 */
export type ServerReleaseActivity =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; workflow: ServerReleaseWorkflow | null }

interface ServerReleaseActivityScope {
  serverId: string
  /** Reads only while true, that is while the Server's OS Provisioning State is `releasing`. */
  releasing: boolean
  /** When Swallow first observed `releasing`; it separates this release from earlier ones. */
  stateSince?: string
}

type ReleaseRead =
  | { scopeKey: string; status: 'ready'; operations: readonly Operation[] }
  | { scopeKey: string; status: 'error'; message: string }

/**
 * Finds the Workflow behind a Server's `releasing` state for the Server detail banner.
 *
 * The Server projection carries no release Operation id, so while `releasing` is true this reads
 * the Server's Workflows (admin-scoped Workflows API) on mount, whenever the Server or the observed
 * release start changes, and every 10 seconds; `findServerReleaseWorkflow` then picks the
 * relevant one. The timer and any in-flight response are discarded on unmount, on a scope change,
 * or when the Server leaves `releasing`. A failed poll keeps the last good result for the same
 * release so the banner does not flicker; only a release with no good read reports `error`.
 */
export function useServerReleaseActivity({ serverId, releasing, stateSince }: ServerReleaseActivityScope): ServerReleaseActivity {
  const { operations } = useApp()
  const scopeKey = `${serverId}|${stateSince ?? ''}`
  const [read, setRead] = useState<ReleaseRead | null>(null)

  useEffect(() => {
    if (!releasing) return
    let cancelled = false
    const load = () => {
      operations.listOperations({ serverId, pageSize: RELEASE_ACTIVITY_PAGE_SIZE }).then(
        (page) => {
          if (!cancelled) setRead({ scopeKey, status: 'ready', operations: page.items })
        },
        (error: Error) => {
          if (cancelled) return
          setRead((current) =>
            current?.scopeKey === scopeKey && current.status === 'ready'
              ? current
              : { scopeKey, status: 'error', message: error.message },
          )
        },
      )
    }
    load()
    const timer = window.setInterval(load, RELEASE_ACTIVITY_POLL_MS)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [operations, releasing, scopeKey, serverId])

  if (!releasing) return { status: 'idle' }
  // A read of an earlier release (or another Server) is never shown for this one.
  if (!read || read.scopeKey !== scopeKey) return { status: 'loading' }
  if (read.status === 'error') return { status: 'error', message: read.message }
  return { status: 'ready', workflow: findServerReleaseWorkflow(read.operations, serverId, stateSince) ?? null }
}
