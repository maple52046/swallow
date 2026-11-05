import { useCallback, useEffect, useRef, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Operation } from '@/domain/operation/types'
import { isGPUOperatorInProgress } from '@/domain/platform/gpuOperator'
import { isPlatformLifecycleInProgress } from '@/domain/platform/lifecycle'
import { platformConvergesWithoutInput } from '@/domain/platform/spec'
import type { Platform } from '@/domain/platform/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import type { PlatformTransition } from './PlatformWorkspaceContext'

const LIFECYCLE_POLL_INTERVAL_MS = 5000
const FOLLOW_CHANGE_MS = 30_000

/** Usable generic shell projection; route-specific data is intentionally absent. */
export interface PlatformWorkspaceData {
  platform: Platform
  lifecycleOperation?: Operation
  lifecycleFailure?: DisplayedFailure
  transition?: PlatformTransition
  reload: (transition?: PlatformTransition) => void
}

/** Initial and background states of the Platform workspace shell. */
export type PlatformWorkspaceState =
  | { status: 'loading' }
  | { status: 'not-found' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; data: PlatformWorkspaceData }

function lifecycleCanChangeWithoutInput(platform: Platform): boolean {
  return isPlatformLifecycleInProgress(platform.lifecycleState)
    || (platform.gpuOperator !== null && isGPUOperatorInProgress(platform.gpuOperator.state))
    || platformConvergesWithoutInput(platform)
}

function transitionStillPending(platform: Platform, transition: PlatformTransition | undefined): boolean {
  if (!transition) return false
  if (transition === 'install') {
    return platform.lifecycleState === 'planned' && platform.status.observedGeneration < platform.generation
  }
  return platform.spec?.desired === 'absent'
    && platform.lifecycleState !== 'uninstalling'
    && platform.lifecycleState !== 'uninstall_failed'
    && platform.lifecycleState !== 'uninstalled'
    && platform.status.observedGeneration < platform.generation
}

/**
 * Loads only the durable Platform and its current lifecycle Workflow. Changing lifecycle state is
 * polled every five seconds; secondary Workflow failure leaves the Platform usable and exposes a
 * correlated warning. A request generation discards responses from an older Platform or Site.
 */
export function usePlatformWorkspace(
  id: string | undefined,
  siteId: string | undefined,
  initialTransition?: PlatformTransition,
): PlatformWorkspaceState {
  const { platforms, operations } = useApp()
  const [state, setState] = useState<PlatformWorkspaceState>(id ? { status: 'loading' } : { status: 'not-found' })
  const [nonce, setNonce] = useState(0)
  const [followUntil, setFollowUntil] = useState(0)
  const transitionRef = useRef<PlatformTransition | undefined>(initialTransition)

  const reload = useCallback((transition?: PlatformTransition) => {
    if (transition) transitionRef.current = transition
    setFollowUntil(Date.now() + FOLLOW_CHANGE_MS)
    setNonce((value) => value + 1)
  }, [])

  useEffect(() => {
    if (!id) return
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    queueMicrotask(() => { if (!cancelled) setState({ status: 'loading' }) })

    const load = async () => {
      try {
        const platform = await platforms.getPlatform(id)
        if (cancelled) return
        if (!platform) {
          setState({ status: 'not-found' })
          return
        }

        let lifecycleOperation: Operation | undefined
        let lifecycleFailure: DisplayedFailure | undefined
        if (platform.lifecycleOperationId) {
          try {
            lifecycleOperation = (await operations.getOperation(platform.lifecycleOperationId)) ?? undefined
          } catch (error) {
            lifecycleFailure = failureOf(error, 'The lifecycle Workflow could not be loaded.')
          }
        }
        if (cancelled) return

        const transition = transitionStillPending(platform, transitionRef.current)
          ? transitionRef.current
          : undefined
        if (!transition) transitionRef.current = undefined
        setState({ status: 'ready', data: { platform, lifecycleOperation, lifecycleFailure, transition, reload } })

        if (lifecycleCanChangeWithoutInput(platform) || transition || Date.now() < followUntil) {
          timer = setTimeout(load, LIFECYCLE_POLL_INTERVAL_MS)
        }
      } catch (error) {
        if (!cancelled) setState({ status: 'error', ...failureOf(error, 'The Platform could not be loaded.') })
      }
    }

    void load()
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [followUntil, id, nonce, operations, platforms, reload, siteId])

  return state
}
