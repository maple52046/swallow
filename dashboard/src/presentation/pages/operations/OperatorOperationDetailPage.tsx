import { useParams } from 'react-router-dom'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DurableOperationDetail } from './DurableOperationDetail'
import { useOperation } from './useOperation'

/** One Workflow: its Jobs and Tasks, their diagnostics, and its controls. */
export function OperatorOperationDetailPage() {
  const { id } = useParams<{ id: string }>()
  const state = useOperation(id)

  if (state.status === 'loading') return <LoadingState rows={7} />
  if (state.status === 'error') return <ErrorState message={state.message} />
  if (state.status === 'not-found') return <EmptyState title="Operation not found" />
  const { operation, reload, refreshError } = state.data
  return <DurableOperationDetail operation={operation} reload={reload} refreshError={refreshError} />
}
