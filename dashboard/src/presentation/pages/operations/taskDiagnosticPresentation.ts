import type {
  OperationFailureClass,
  OperationResourceReference,
  OperationStepWaiting,
  OperationTimelineEvent,
} from '@/domain/operation/types'
import { formatDuration } from '@/shared/utils/time'

/**
 * First line of a possibly multi-line message, for a one-line headline or table cell. The Ansible
 * executor emits one failure line per host separated by newlines; a compact view shows the first,
 * and the Stderr tab carries the rest.
 */
export function firstLine(message: string): string {
  const line = message.split('\n', 1)[0]?.trim() ?? ''
  return line || message.trim()
}

/**
 * The text a Workflow detail shows for a Failure Class: a short label and what it means for the
 * user's next move. A class this build does not know (a newer API) is shown as its raw value with
 * no claim about retrying, so an unknown value never reads as advice.
 */
export function failureClassText(failureClass: OperationFailureClass | string): { label: string; meaning: string } {
  switch (failureClass) {
    case 'transient':
      return { label: 'Transient', meaning: 'Expected to clear by itself; waiting or retrying can succeed without a change.' }
    case 'permanent':
      return { label: 'Permanent', meaning: 'Repeats until something changes; fix the cause before retrying.' }
    case 'unknown':
      return { label: 'Unknown', meaning: 'Swallow could not classify this failure.' }
    default:
      return { label: failureClass, meaning: '' }
  }
}

/**
 * The user-facing name of a diagnostic's affected resource, for example "Server srv-1". Only the
 * kinds the workflows contract documents get a capitalized label; any other kind is shown as sent.
 */
export function affectedTargetText(target: OperationResourceReference): string {
  const kind = target.kind === 'server' ? 'Server' : target.kind === 'platform' ? 'Platform' : target.kind
  return `${kind} ${target.id}`
}

/**
 * Seconds as a compact duration ("4m 10s"), or `undefined` for an absent or zero value: the API
 * omits a count or duration it did not measure, and zero would otherwise read as "instant".
 */
export function secondsText(seconds: number | undefined): string | undefined {
  return seconds ? formatDuration(seconds * 1000) : undefined
}

/**
 * How long a wait ran against its budget, for example "4m 10s of a 10m 0s budget (5m 50s left)".
 * A wait without a budget reports only its elapsed time; one past its budget says so instead of a
 * negative remainder. The numbers are those the Runner published at its last observation, not a
 * live clock, so the text never runs ahead of what swallow observed.
 */
export function waitBudgetText(elapsedSeconds: number | undefined, budgetSeconds: number | undefined): string | undefined {
  const elapsed = secondsText(elapsedSeconds) ?? '0s'
  const budget = secondsText(budgetSeconds)
  if (!budget) return elapsedSeconds ? elapsed : undefined
  const remaining = (budgetSeconds ?? 0) - (elapsedSeconds ?? 0)
  return remaining > 0
    ? `${elapsed} of a ${budget} budget (${secondsText(remaining)} left)`
    : `${elapsed} of a ${budget} budget (budget used up)`
}

/** The one-line progress of a published wait: observations so far and elapsed time against its budget. */
export function waitingProgressText(waiting: OperationStepWaiting): string {
  const observations = `${waiting.attempts} ${waiting.attempts === 1 ? 'observation' : 'observations'}`
  const budget = waitBudgetText(waiting.elapsedSeconds, waiting.budgetSeconds)
  return budget ? `Waited ${budget} · ${observations}` : observations
}

/** The failure an earlier attempt recorded, read from a `step_status` timeline event. */
export interface TimelineAttemptFailure {
  attempt?: number
  code: string
  message?: string
  failureClass?: string
  endpoint?: string
}

/**
 * Reads the failure a `step_status` event carries in its `details` (workflows contract, Timeline):
 * `code`, `message`, `attempt`, and, since 2026-11-05, the diagnostic's `failureClass` and
 * `endpoint`. `details` is free-form on the wire, so every field is narrowed by type and a missing
 * or mistyped one is left out; an event of another type, or one without a `code`, has no failure.
 */
export function timelineAttemptFailure(event: OperationTimelineEvent): TimelineAttemptFailure | null {
  if (event.type !== 'step_status' || !event.details) return null
  const { code, message, attempt, failureClass, endpoint } = event.details
  if (typeof code !== 'string' || code === '') return null
  return {
    code,
    attempt: typeof attempt === 'number' ? attempt : undefined,
    message: typeof message === 'string' && message !== '' ? message : undefined,
    failureClass: typeof failureClass === 'string' && failureClass !== '' ? failureClass : undefined,
    endpoint: typeof endpoint === 'string' && endpoint !== '' ? endpoint : undefined,
  }
}
