import type { BulkAction } from './serverActions'

/** Identifies a Server in operator-facing action results. */
export interface ServerActionTarget {
  serverId: string
  serverName: string
}

/** One provider action outcome, including client-safe diagnostics on failure. */
export interface ServerActionOutcome extends ServerActionTarget {
  accepted: boolean
  code?: string
  httpStatus?: number
  requestId?: string
  message?: string
  taskId?: string
}

/** Complete result of one single- or multi-Server action run. */
export interface ServerActionRunResult {
  action: BulkAction
  total: number
  succeeded: number
  outcomes: ServerActionOutcome[]
  completedAt: string
}

interface ErrorDetails {
  code?: unknown
  status?: unknown
  requestId?: unknown
}

/**
 * Converts an unknown repository failure into presentation-safe diagnostics without
 * importing the infrastructure-layer ApiRequestError class.
 */
export function rejectedServerActionOutcome(
  target: ServerActionTarget,
  error: unknown,
): ServerActionOutcome {
  const details = typeof error === 'object' && error !== null ? error as ErrorDetails : {}
  return {
    ...target,
    accepted: false,
    code: typeof details.code === 'string' ? details.code : 'unknown_error',
    httpStatus: typeof details.status === 'number' ? details.status : undefined,
    requestId: typeof details.requestId === 'string' ? details.requestId : undefined,
    message: error instanceof Error ? error.message : 'Unknown error',
  }
}

/** Builds the result shape shared by list and detail Server actions. */
export function serverActionRunResult(
  action: BulkAction,
  outcomes: ServerActionOutcome[],
): ServerActionRunResult {
  return {
    action,
    total: outcomes.length,
    succeeded: outcomes.filter((outcome) => outcome.accepted).length,
    outcomes,
    completedAt: new Date().toISOString(),
  }
}

/** Returns the failed outcomes from a completed run. */
export function failedServerActionOutcomes(
  result: ServerActionRunResult,
): ServerActionOutcome[] {
  return result.outcomes.filter((outcome) => !outcome.accepted)
}

const ACTION_RESULTS_KEY = 'swallow.server-action-results.v1'
export const SERVER_ACTION_RESULT_RECORDED_EVENT = 'swallow:server-action-result-recorded'
const MAX_STORED_RUNS = 100

function isServerActionRunResult(value: unknown): value is ServerActionRunResult {
  if (typeof value !== 'object' || value === null) return false
  const candidate = value as Partial<ServerActionRunResult>
  return typeof candidate.action === 'string'
    && typeof candidate.total === 'number'
    && typeof candidate.succeeded === 'number'
    && typeof candidate.completedAt === 'string'
    && Array.isArray(candidate.outcomes)
    && candidate.outcomes.every((outcome) => (
      typeof outcome === 'object'
      && outcome !== null
      && typeof outcome.serverId === 'string'
      && typeof outcome.serverName === 'string'
      && typeof outcome.accepted === 'boolean'
    ))
}

function readStoredServerActionResults(): ServerActionRunResult[] {
  try {
    const raw = sessionStorage.getItem(ACTION_RESULTS_KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed.filter(isServerActionRunResult) : []
  } catch {
    return []
  }
}

/**
 * Retains client-safe action outcomes for the current browser tab. This bridges list and
 * detail navigation, but deliberately does not present browser storage as durable audit.
 */
export function persistServerActionResult(result: ServerActionRunResult): void {
  try {
    const next = [result, ...readStoredServerActionResults()].slice(0, MAX_STORED_RUNS)
    sessionStorage.setItem(ACTION_RESULTS_KEY, JSON.stringify(next))
    window.dispatchEvent(new CustomEvent(SERVER_ACTION_RESULT_RECORDED_EVENT))
  } catch {
    // Diagnostics must never make the underlying provider action appear to fail.
  }
}

/** Returns newest-first outcomes for one Server from the current browser tab only. */
export function listStoredServerActionResults(serverId: string): ServerActionRunResult[] {
  return readStoredServerActionResults().flatMap((result) => {
    const outcomes = result.outcomes.filter((outcome) => outcome.serverId === serverId)
    if (outcomes.length === 0) return []
    return [{
      ...result,
      total: outcomes.length,
      succeeded: outcomes.filter((outcome) => outcome.accepted).length,
      outcomes,
    }]
  })
}
