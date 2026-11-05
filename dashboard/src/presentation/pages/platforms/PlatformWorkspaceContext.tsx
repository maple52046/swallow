import { useOutletContext } from 'react-router-dom'
import type { Operation } from '@/domain/operation/types'
import type { Platform } from '@/domain/platform/types'
import type { DisplayedFailure } from '@/presentation/components/failure'

/** A confirmed spec write whose Workflow has not yet appeared in the Platform projection. */
export type PlatformTransition = 'install' | 'uninstall'

/** Shared, generic runtime data made available to every Platform workspace child route. */
export interface PlatformWorkspaceContextValue {
  platform: Platform
  lifecycleOperation?: Operation
  lifecycleFailure?: DisplayedFailure
  transition?: PlatformTransition
  reload: (transition?: PlatformTransition) => void
}

/** Reads the Platform workspace shell context from a nested route. */
export function usePlatformWorkspaceContext(): PlatformWorkspaceContextValue {
  return useOutletContext<PlatformWorkspaceContextValue>()
}
