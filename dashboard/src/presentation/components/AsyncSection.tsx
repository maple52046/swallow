import type { ReactNode } from 'react'
import type { AsyncData } from '@/presentation/hooks/useAsyncData'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { Alert } from '@/presentation/components/ui/alert'

interface AsyncSectionProps<T> {
  /** The state from `useAsyncData`; `ready` renders `children` with the loaded data. */
  state: AsyncData<T>
  /** Headline of the eligibility notice, naming the explorer that is unavailable. */
  unavailableTitle: string
  /** Sentence appended to the API's reason, telling the operator when the view becomes available. */
  unavailableHint: string
  /** Skeleton rows while loading. */
  loadingRows?: number
  children: (data: T) => ReactNode
}

/**
 * The standard loading / error / unavailable envelope around a live explorer read.
 *
 * Shared by the Kubernetes cluster explorer and the Docker Host Explorer so the three non-ready
 * states look identical: a skeleton while loading, the shared error state for a fault, and an
 * informational notice (not an error) when the API reports the target ineligible. The notice keeps
 * the API's human message and adds the caller's hint about how the view becomes available.
 */
export function AsyncSection<T>({ state, unavailableTitle, unavailableHint, loadingRows = 4, children }: AsyncSectionProps<T>) {
  if (state.status === 'loading') return <LoadingState rows={loadingRows} />
  if (state.status === 'error') return <ErrorState message={state.message} />
  if (state.status === 'unavailable') {
    return (
      <Alert status="info" title={unavailableTitle}>
        {state.message} {unavailableHint}
      </Alert>
    )
  }
  return <>{children(state.data)}</>
}
