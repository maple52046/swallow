import { useCallback, useEffect, useState } from 'react'
import { useSlurmPlatform } from './SlurmModuleProvider'
import type { SlurmCluster } from '@/domain/platform/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'

// The live Slurm read is a lightweight observability widget, not lifecycle state, so it polls
// slowly and independently of the platform detail hook's faster lifecycle polling.
const SLURM_CLUSTER_POLL_INTERVAL_MS = 15000

/**
 * Loading, degraded, and ready states for the live Slurm cluster read. `unavailable` is a
 * normal, non-error outcome only when no slurmrestd Integration is recorded. Provider failures are
 * explicit errors; a refresh failure preserves the last successful cluster as a warning.
 */
type SlurmClusterSnapshot =
  | { status: 'loading' }
  | { status: 'unavailable' }
  | ({ status: 'error' } & DisplayedFailure)
  | ({ status: 'warning'; cluster: SlurmCluster } & DisplayedFailure)
  | { status: 'ready'; cluster: SlurmCluster }

interface SlurmClusterCache {
  key: string
  snapshot: SlurmClusterSnapshot
}

export type SlurmClusterState = SlurmClusterSnapshot & { reload: () => void }

/**
 * Reads a Slurm platform's live cluster state on demand and polls it slowly.
 *
 * It never throws to the caller. A missing Integration resolves to `unavailable`; request failures
 * resolve to `error`, or `warning` when last-good data exists. Polling is cleared on unmount and
 * when the platform changes.
 */
export function useSlurmCluster(platformId: string, integrationId: string | null): SlurmClusterState {
  const slurm = useSlurmPlatform()
  const [revision, setRevision] = useState(0)
  const reload = useCallback(() => setRevision((value) => value + 1), [])
  const key = `${platformId}\u0000${integrationId ?? ''}`
  const [cache, setCache] = useState<SlurmClusterCache>(() => ({
    key,
    snapshot: integrationId ? { status: 'loading' } : { status: 'unavailable' },
  }))
  // A route change never presents the previous Platform's cluster while the new request runs.
  const state: SlurmClusterSnapshot = cache.key === key
    ? cache.snapshot
    : integrationId ? { status: 'loading' } : { status: 'unavailable' }

  useEffect(() => {
    // No integration means slurmrestd is not recorded (for example an image without
    // slurm-smd-slurmrestd), so there is nothing live to read; the seeded state already
    // reflects that and there is nothing to poll.
    if (!integrationId) {
      return
    }
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined

    const load = () => {
      slurm.getCluster(platformId)
        .then((cluster) => {
          if (cancelled) return
          setCache({ key, snapshot: { status: 'ready', cluster } })
        })
        .catch((error) => {
          if (cancelled) return
          const failure = failureOf(error, 'Live Slurm state could not be loaded.')
          setCache((current) => {
            const snapshot: SlurmClusterSnapshot = current.key === key ? current.snapshot : { status: 'loading' }
            if (snapshot.status === 'ready' || snapshot.status === 'warning') {
              return { key, snapshot: { status: 'warning', cluster: snapshot.cluster, ...failure } }
            }
            return { key, snapshot: { status: 'error', ...failure } }
          })
        })
        .finally(() => {
          if (!cancelled) timer = setTimeout(load, SLURM_CLUSTER_POLL_INTERVAL_MS)
        })
    }

    load()
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [slurm, key, platformId, integrationId, revision])

  return { ...state, reload }
}
