import { useEffect, useMemo, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Pool } from '@/domain/infrastructure/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'

/** The choice value for "in no Pool", which a caller sends as `poolId: null`. */
export const NO_POOL_CHOICE = 'no-pool'

/** The Pool catalog read behind {@link usePoolChoices}. */
export type PoolChoicesState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; pools: Pool[] }

/** One choice of a Pool select: a Pool id, or {@link NO_POOL_CHOICE}. */
export interface PoolChoice {
  value: string
  label: string
}

/**
 * The Pools a dialog that sets Servers' Pool offers: "No pool (admins only)" followed by every Pool
 * of the installation, which may hold Servers of any Site. The catalog is a reference read any
 * signed-in user may make; it runs once per mount, and the flag drops an answer that arrives after
 * the dialog closed. Until it answers, `options` holds only the no-pool choice, so a caller renders
 * its select once `state` is ready.
 */
export function usePoolChoices(): { state: PoolChoicesState; options: PoolChoice[] } {
  const { infrastructure } = useApp()
  const [state, setState] = useState<PoolChoicesState>({ status: 'loading' })

  useEffect(() => {
    let cancelled = false
    infrastructure.listPools().then(
      (pools) => {
        if (!cancelled) setState({ status: 'ready', pools })
      },
      (caught: unknown) => {
        if (!cancelled) setState({ status: 'error', ...failureOf(caught, 'The pools could not be loaded.') })
      },
    )
    return () => {
      cancelled = true
    }
  }, [infrastructure])

  const options = useMemo(() => [
    { value: NO_POOL_CHOICE, label: 'No pool (admins only)' },
    ...(state.status === 'ready' ? state.pools : []).map((pool) => ({ value: pool.id, label: pool.name })),
  ], [state])
  return { state, options }
}
