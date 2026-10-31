import { useEffect } from 'react'

/** How often a view re-reads a resource whose saved change is still being applied. */
export const SETTLE_REFRESH_MS = 5000

/**
 * Re-reads a migrated resource every few seconds while `settling` is true — its saved change is
 * still being applied by the backend (see `isSettling`) — so its status settles in place without
 * the operator reloading. Polling stops as soon as the view stops settling or unmounts.
 *
 * `refresh` must be stable across renders (wrap it in `useCallback`) and should update the view's
 * own state. A failed re-read is ignored: the view keeps what it last showed, and the next tick
 * tries again.
 */
export function useSettlingRefresh(settling: boolean, refresh: () => Promise<void>, intervalMs = SETTLE_REFRESH_MS): void {
  useEffect(() => {
    if (!settling) return
    const timer = window.setInterval(() => {
      refresh().catch(() => undefined)
    }, intervalMs)
    return () => window.clearInterval(timer)
  }, [settling, refresh, intervalMs])
}
