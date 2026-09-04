import { useCallback, useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Platform } from '@/domain/platform/types'

/** Discriminated union so the list is never both loading and loaded. */
export type PlatformsState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; platforms: Platform[] }

/**
 * Loads the platform list, optionally scoped to one site.
 *
 * A stale-guard drops out-of-order responses, and `reload` refetches after an action such
 * as a deployment. The list is small and bounded, so it is fetched whole rather than paged.
 */
export function usePlatforms(siteId?: string): { state: PlatformsState; reload: () => void } {
  const { platforms } = useApp()
  const [state, setState] = useState<PlatformsState>({ status: 'loading' })
  const [nonce, setNonce] = useState(0)
  const reload = useCallback(() => setNonce((value) => value + 1), [])

  useEffect(() => {
    let cancelled = false
    platforms
      .listPlatforms(siteId)
      .then((result) => {
        if (!cancelled) setState({ status: 'ready', platforms: result })
      })
      .catch((err: Error) => {
        if (!cancelled) setState({ status: 'error', message: err.message })
      })
    return () => {
      cancelled = true
    }
  }, [platforms, siteId, nonce])

  return { state, reload }
}
