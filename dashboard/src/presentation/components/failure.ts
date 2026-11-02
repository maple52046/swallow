import type { RequestFailure } from '@/application/ports/RequestFailure'

/** What the UI shows for a failure: the operator-facing message and, when known, the request ID. */
export interface DisplayedFailure {
  message: string
  requestId?: string
}

/**
 * Reads a caught value as a displayed failure. An Error keeps its message and, when it carries one
 * (a `RequestFailure` from an API adapter), its request ID; anything else shows `fallback`. The
 * check is structural because presentation never imports the adapters' error classes.
 */
export function failureOf(caught: unknown, fallback: string): DisplayedFailure {
  if (!(caught instanceof Error)) return { message: fallback }
  const requestId = (caught as Partial<RequestFailure>).requestId
  return typeof requestId === 'string' && requestId !== ''
    ? { message: caught.message, requestId }
    : { message: caught.message }
}

/**
 * Renders a failure as one line for channels that take plain text, such as a toast description:
 * the message, then the request ID when there is one.
 */
export function failureText(failure: DisplayedFailure): string {
  return failure.requestId ? `${failure.message} Request ID: ${failure.requestId}` : failure.message
}
