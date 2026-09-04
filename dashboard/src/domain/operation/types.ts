/**
 * An operation: an operator's intent and the swallow-owned run that carries it out.
 *
 * swallow persists the intent, dispatches an ansible-runner process, retains the logs, and
 * exposes task-level progress. A failed operation is never retried automatically; an
 * operator creates a new one linked to the original. See
 * docs/development/glossaries/terms/operation.md.
 */

export type OperationStatus =
  | 'pending'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'canceled'
  | 'indeterminate'

/** The swallow-owned run: no external-controller identifiers. */
export interface OperationExecution {
  runId: string
  playbook: string
  status: OperationStatus
  statusReason: string | null
  startedAt: string | null
  finishedAt: string | null
}

export interface Operation {
  id: string
  kind: string
  intent: string
  siteId: string
  platformId: string | null
  targetServerIds: string[]
  /** The operation this one retried, or null when requested directly. */
  retryOfOperationId: string | null
  execution: OperationExecution
  requestedBy: string
  requestedAt: string
  updatedAt: string
}

/** One task result on one host. Carries no task output, which could contain a secret. */
export interface TaskEvent {
  play: string
  task: string
  host: string
  status: string
  changed: boolean
  startedAt: string | null
  endedAt: string | null
}

/** A run's task-level progress, derived from the runner's own event stream. */
export interface OperationEvents {
  runId: string
  status: string
  okCount: number
  changedCount: number
  failedCount: number
  events: TaskEvent[]
}

export interface ListOperationsFilters {
  siteId?: string
  platformId?: string
  serverId?: string
  kind?: string
  status?: OperationStatus
  active?: boolean
  page?: number
  pageSize?: number
}

/**
 * Whether an operation's run has reached a state that will not change on its own. Drives
 * whether a detail view keeps polling and whether Retry is offered.
 */
export function isTerminalStatus(status: OperationStatus): boolean {
  return (
    status === 'succeeded' ||
    status === 'failed' ||
    status === 'canceled' ||
    status === 'indeterminate'
  )
}
