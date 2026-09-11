import { useCallback, useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { LogViewer } from '@/presentation/components/LogViewer'

type LogsState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; text: string }

/**
 * Retained-output workspace for a legacy run or one durable Step.
 *
 * `variant` selects the stream: "stdout" is the full runner output; "stderr" is
 * the focused error-only report (failed/unreachable tasks and their
 * stderr/stdout), so a failing Step's cause is readable without scrolling the
 * whole play. The stderr stream is per-Step only. Fetching is guarded against
 * stale responses, and the display (search, copy, download, refresh) is delegated
 * to the shared `LogViewer`.
 */
export function OperationLogWorkspace({
  operationId,
  stepId,
  variant = 'stdout',
}: {
  operationId: string
  stepId?: string
  variant?: 'stdout' | 'stderr'
}) {
  const { operations } = useApp()
  const [state, setState] = useState<LogsState>({ status: 'loading' })
  const [nonce, setNonce] = useState(0)
  const reload = useCallback(() => setNonce((value) => value + 1), [])
  const noun = variant === 'stderr' ? 'stderr' : 'stdout'

  useEffect(() => {
    let cancelled = false
    const request =
      variant === 'stderr' && stepId
        ? operations.getStepStderr(operationId, stepId)
        : stepId
          ? operations.getStepLogs(operationId, stepId)
          : operations.getLogs(operationId)
    request
      .then((text) => {
        if (!cancelled) setState({ status: 'ready', text })
      })
      .catch((error: Error) => {
        if (!cancelled) setState({ status: 'error', message: error.message })
      })
    return () => {
      cancelled = true
    }
  }, [nonce, operationId, operations, stepId, variant])

  if (state.status === 'loading') return <LoadingState rows={5} />
  if (state.status === 'error') return <ErrorState message={state.message} onRetry={reload} />
  if (!state.text.trim()) {
    return variant === 'stderr' ? (
      <EmptyState title="No errors" message="This Step recorded no failed or unreachable tasks." />
    ) : (
      <EmptyState title="No stdout yet" message="Output appears after the executor starts producing stdout." />
    )
  }

  return (
    <LogViewer
      text={state.text}
      label={noun}
      downloadName={`swallow-operation-${operationId}${stepId ? `-${stepId}` : ''}-${noun}`}
      onRefresh={reload}
    />
  )
}
