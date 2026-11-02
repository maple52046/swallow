/**
 * A failed request to swallow's API as the inner layers see it. The message is safe to show an
 * user; `requestId`, when the server answered, is the opaque correlation ID that finds the
 * failure in the server log. Every API adapter error implements it, so the UI can show both
 * without knowing the transport.
 */
export interface RequestFailure extends Error {
  readonly requestId?: string
}
