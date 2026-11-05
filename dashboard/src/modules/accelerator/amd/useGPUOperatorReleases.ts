import { useCallback, useEffect, useState } from 'react'
import { useAMDPlatform } from './AMDModuleProvider'
import type { GPUOperatorRelease } from '@/domain/platform/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'

/**
 * Loading contract for the GPU Operator release catalog. Until it is ready a form cannot offer a
 * version, so callers keep the GPU Operator options unsubmittable rather than guess one. `error`
 * carries the failed read's message and request ID.
 */
export type GPUOperatorReleasesState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; releases: GPUOperatorRelease[] }

/**
 * Loads the AMD GPU Operator releases swallow ships (`GET /platforms/gpu-operator/releases`) for
 * the deploy wizard and the Platform's GPU Operator dialog. `enabled` defers the request until the
 * options are shown: the dialog is open, or the wizard's operator is on, by the user or by its
 * proposal. `reload` retries after an error. A stale response from an
 * earlier mount is ignored on cleanup.
 */
export function useGPUOperatorReleases(enabled = true): { state: GPUOperatorReleasesState; reload: () => void } {
  const amd = useAMDPlatform()
  const [state, setState] = useState<GPUOperatorReleasesState>({ status: 'loading' })
  const [revision, setRevision] = useState(0)
  const reload = useCallback(() => {
    setState({ status: 'loading' })
    setRevision((current) => current + 1)
  }, [])

  useEffect(() => {
    if (!enabled) return
    let cancelled = false
    amd.listGPUOperatorReleases()
      .then((releases) => {
        if (!cancelled) setState({ status: 'ready', releases })
      })
      .catch((error: Error) => {
        if (!cancelled) setState({ status: 'error', ...failureOf(error, 'GPU Operator releases could not be loaded.') })
      })
    return () => {
      cancelled = true
    }
  }, [amd, enabled, revision])

  return { state, reload }
}
