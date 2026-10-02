/**
 * Resolves the API address, preferring an explicit VITE_API_BASE_URL.
 *
 * The default is deliberately empty: both Vite in development and Nginx in production
 * proxy /api on the dashboard origin.
 */
function resolveApiBaseUrl(): string {
  const configured = import.meta.env.VITE_API_BASE_URL as string | undefined
  if (configured) {
    return configured.replace(/\/+$/, '')
  }
  return ''
}

/**
 * The resolved API origin, shared by fetch-based adapters and the SSE adapter. Empty means
 * same-origin (the dev proxy and production Nginx both serve /api on the dashboard origin).
 */
export const API_BASE_URL = resolveApiBaseUrl()
