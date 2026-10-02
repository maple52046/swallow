import { useCallback, useEffect, useState } from 'react'

/**
 * Loading/error/ready states for an on-demand async read, plus the "unavailable" outcome a live
 * explorer degrades to when its target is not eligible — the Kubernetes cluster explorer for a
 * Platform that is not an eligible deployed cluster, or the Docker Host Explorer for a Server
 * without an API-enabled Docker CE.
 *
 * `unavailable` is a normal (non-error) result produced by an eligibility API response (404 or
 * 409 per the explorer contracts); the view shows a notice rather than an error banner.
 */
export type AsyncData<T> =
  | { status: 'loading' }
  | { status: 'unavailable'; message: string }
  | { status: 'error'; message: string }
  | { status: 'ready'; data: T }

/** API error codes that mean "the explorer is not available for this Platform", not a fault. */
const UNAVAILABLE_STATUSES = new Set([404, 409])

/**
 * Structurally extracts an HTTP status from a rejected request without importing the
 * infrastructure error class (which the presentation layer must not depend on). The API adapter
 * rejects with an error carrying a numeric `status`, which is all this needs.
 */
function httpStatusOf(error: unknown): number | null {
  if (error && typeof error === 'object' && 'status' in error) {
    const status = (error as { status?: unknown }).status
    if (typeof status === 'number') return status
  }
  return null
}

/**
 * Runs an async loader and tracks its state, mapping an eligibility API error to `unavailable`
 * so a caller can degrade instead of showing an error. `reload` re-runs the loader; the loader
 * identity (via `deps`) resets the state. Effects are cancellation-guarded so a slow response
 * for a previous Platform cannot overwrite the current one.
 */
export function useAsyncData<T>(loader: () => Promise<T>, deps: readonly unknown[]): AsyncData<T> & { reload: () => void } {
  const [state, setState] = useState<AsyncData<T>>({ status: 'loading' })
  const [nonce, setNonce] = useState(0)

  // The loader closes over the caller's deps; we intentionally key the effect on deps + nonce
  // rather than the loader identity, which would change every render.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const run = useCallback(loader, deps)

  useEffect(() => {
    let cancelled = false
    setState({ status: 'loading' })
    run()
      .then((data) => {
        if (!cancelled) setState({ status: 'ready', data })
      })
      .catch((error) => {
        if (cancelled) return
        const status = httpStatusOf(error)
        const message = error instanceof Error ? error.message : 'Request failed.'
        if (status !== null && UNAVAILABLE_STATUSES.has(status)) {
          setState({ status: 'unavailable', message })
          return
        }
        setState({ status: 'error', message })
      })
    return () => {
      cancelled = true
    }
  }, [run, nonce])

  const reload = useCallback(() => setNonce((value) => value + 1), [])
  return { ...state, reload }
}
