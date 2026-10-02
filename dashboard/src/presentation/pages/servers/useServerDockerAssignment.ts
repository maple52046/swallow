import { useCallback, useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { SoftwareAssignment } from '@/domain/software/types'

/**
 * The Server's swallow-owned Docker CE Software Assignment, which decides whether the Server detail
 * shows its Containers tab and what that tab offers (decision 043). `assignment` is null when
 * swallow has no non-absent Docker CE record for the Server.
 */
export type ServerDockerAssignment =
  | { status: 'loading'; reload: () => void }
  | { status: 'error'; message: string; reload: () => void }
  | { status: 'ready'; assignment: SoftwareAssignment | null; reload: () => void }

// A re-apply (enabling or disabling the Engine API) is a durable Workflow that usually takes tens of
// seconds; polling at this pace lets the tab flip to the explorer soon after it lands without
// hammering the assignment list.
const IN_FLIGHT_POLL_MS = 5_000

/**
 * Loads the Docker CE assignment for one Server through the SoftwareRepository port.
 *
 * - The read is independent of the Server projection: a failure degrades to `error` for the
 *   Containers tab only, never the whole detail page.
 * - While the assignment is `pending` or `uninstalling` it re-reads every few seconds, so a
 *   Workflow started from the tab is reflected without a manual refresh; the timer stops as soon
 *   as the state settles or the component unmounts.
 * - Responses for a previous Server id (or a superseded reload) are dropped.
 */
export function useServerDockerAssignment(serverId: string | undefined): ServerDockerAssignment {
  const { software } = useApp()
  const [state, setState] = useState<
    { status: 'loading' } | { status: 'error'; message: string } | { status: 'ready'; assignment: SoftwareAssignment | null }
  >({ status: 'loading' })
  const [nonce, setNonce] = useState(0)
  const reload = useCallback(() => setNonce((value) => value + 1), [])

  useEffect(() => {
    if (!serverId) return
    let cancelled = false
    software
      .listAssignments({ serverId, kind: 'docker-ce' })
      .then((items) => {
        if (!cancelled) setState({ status: 'ready', assignment: items[0] ?? null })
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setState({ status: 'error', message: error instanceof Error ? error.message : 'Docker CE status could not be read.' })
        }
      })
    return () => {
      cancelled = true
    }
  }, [software, serverId, nonce])

  const inFlight = state.status === 'ready' && (state.assignment?.state === 'pending' || state.assignment?.state === 'uninstalling')
  useEffect(() => {
    // Poll only while a Workflow is changing the record; cleared on settle and on unmount.
    if (!inFlight) return
    const timer = window.setTimeout(reload, IN_FLIGHT_POLL_MS)
    return () => window.clearTimeout(timer)
  }, [inFlight, reload, state])

  return { ...state, reload }
}
