import type { Operation, OperationStatus, OperationStepStatus } from '@/domain/operation/types'
import { operationStatus } from '@/domain/operation/types'

const CHANGING_STATUSES: ReadonlySet<OperationStatus> = new Set([
  'pending',
  'running',
  'waiting_external',
  'waiting_dependency',
  'canceling',
])

const REVIEW_STATUSES: ReadonlySet<OperationStatus> = new Set([
  'requires_attention',
  'failed',
  'partially_succeeded',
  'indeterminate',
])

const STATUS_LABELS: Record<OperationStepStatus, string> = {
  pending: 'pending',
  running: 'running',
  waiting_external: 'waiting externally',
  waiting_dependency: 'waiting on dependencies',
  succeeded: 'succeeded',
  failed: 'failed',
  canceled: 'canceled',
  skipped: 'skipped',
  requires_attention: 'need attention',
}

/** Presentation states that can advance without a new operator repair command. */
export function isWorkflowChangingStatus(status: OperationStatus): boolean {
  return CHANGING_STATUSES.has(status)
}

/** Presentation states whose next useful list action is inspection or repair. */
export function isWorkflowReviewStatus(status: OperationStatus): boolean {
  return REVIEW_STATUSES.has(status)
}

/** Native-link label that reflects why an operator would open a Workflow. */
export function workflowActionLabel(status: OperationStatus): string {
  if (isWorkflowChangingStatus(status)) return 'Monitor workflow'
  if (isWorkflowReviewStatus(status)) return 'Review workflow'
  return 'View workflow'
}

/** Render-ready, non-authoritative context derived from one Workflow list item. */
export interface WorkflowActivitySummary {
  primary: string
  secondary?: string
  taskTotal: number
  taskCounts: string
}

/**
 * Derives compact list context from the existing Workflow projection.
 *
 * Modern payloads expose Tasks under the compatibility `steps` field. The
 * selector prioritizes blocked work for review states and live Runner activity
 * otherwise. Legacy records safely fall back to their playbook and reason.
 */
export function workflowActivitySummary(operation: Operation): WorkflowActivitySummary {
  const status = operationStatus(operation)
  const tasks = operation.steps ?? []
  if (tasks.length === 0) {
    return {
      primary: operation.execution.playbook || 'Legacy execution',
      secondary: operation.statusReason ?? operation.execution.statusReason ?? undefined,
      taskTotal: 0,
      taskCounts: 'Legacy execution',
    }
  }

  const priority: OperationStepStatus[] = isWorkflowReviewStatus(status)
    ? ['requires_attention', 'failed', 'running', 'waiting_external', 'waiting_dependency', 'pending']
    : ['running', 'waiting_external', 'waiting_dependency', 'pending', 'requires_attention', 'failed']
  const focus = priority
    .map((candidate) => tasks.find((task) => task.status === candidate))
    .find((task) => task !== undefined)

  const counts = tasks.reduce<Partial<Record<OperationStepStatus, number>>>((result, task) => {
    result[task.status] = (result[task.status] ?? 0) + 1
    return result
  }, {})
  const taskCounts = (Object.keys(STATUS_LABELS) as OperationStepStatus[])
    .filter((taskStatus) => (counts[taskStatus] ?? 0) > 0)
    .map((taskStatus) => `${counts[taskStatus]} ${STATUS_LABELS[taskStatus]}`)
    .join(' · ')

  if (!focus) {
    return {
      primary: status === 'succeeded' ? 'All Tasks completed' : operation.statusReason || 'Workflow completed',
      taskTotal: tasks.length,
      taskCounts,
    }
  }

  const runnerActivity = focus.live?.currentTask
  return {
    primary: runnerActivity || focus.name,
    secondary: runnerActivity
      ? focus.name
      : focus.error?.message || focus.waitingReason || operation.statusReason || operation.execution.statusReason || undefined,
    taskTotal: tasks.length,
    taskCounts,
  }
}
