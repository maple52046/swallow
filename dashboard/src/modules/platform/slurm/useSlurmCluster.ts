import { useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { SlurmCluster } from '@/domain/platform/types'

// The live Slurm read is a lightweight observability widget, not lifecycle state, so it polls
// slowly and independently of the platform detail hook's faster lifecycle polling.
const SLURM_CLUSTER_POLL_INTERVAL_MS = 15000

/**
 * Loading, degraded, and ready states for the live Slurm cluster read. `unavailable` is a
 * normal, non-error outcome (no slurmrestd integration yet, or slurmrestd unreachable); the
 * Slurm view degrades to deployment intent plus membership when it sees it.
 */
export type SlurmClusterState =
  | { status: 'loading' }
  | { status: 'unavailable' }
  | { status: 'ready'; cluster: SlurmCluster }

/**
 * Reads a Slurm platform's live cluster state on demand and polls it slowly.
 *
 * It never throws to the caller: any failure (including a platform with no slurmrestd
 * integration, where `integrationId` is null) resolves to `unavailable` so the view can
 * degrade rather than error. Polling is cleared on unmount and when the platform changes.
 */
export function useSlurmCluster(platformId: string, integrationId: string | null): SlurmClusterState {
  const { platforms } = useApp()
  // Seed from integrationId so the no-integration case (no slurmrestd recorded yet) starts
  // degraded without a synchronous setState inside the effect.
  const [state, setState] = useState<SlurmClusterState>(() =>
    integrationId ? { status: 'loading' } : { status: 'unavailable' },
  )

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
      platforms
        .getSlurmCluster(platformId)
        .then((cluster) => {
          if (cancelled) return
          setState(cluster ? { status: 'ready', cluster } : { status: 'unavailable' })
        })
        .catch(() => {
          if (!cancelled) setState({ status: 'unavailable' })
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
  }, [platforms, platformId, integrationId])

  return state
}
