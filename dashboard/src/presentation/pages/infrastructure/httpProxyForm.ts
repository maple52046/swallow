import type { ProvisionerHttpProxyMode } from '@/domain/provisioning/types'

/**
 * Operator labels and one-line explanations of the Provisioner HTTP Proxy modes, worded after the
 * MAAS UI choices an operator already knows. The domain value (`none`, `builtin`, …) is what flows
 * to the API; these strings are display only.
 */
export const HTTP_PROXY_MODE_TEXT: Record<ProvisionerHttpProxyMode, { label: string; help: string }> = {
  none: { label: "Don't use a proxy", help: 'Deployed machines reach package repositories directly.' },
  builtin: { label: 'MAAS built-in', help: 'Deployed machines use the MAAS proxy (port 8000), which goes out directly.' },
  external: { label: 'External', help: 'Deployed machines and MAAS use the proxy below directly.' },
  peer: { label: 'Peer', help: 'Deployed machines use the MAAS proxy, which forwards to the proxy below.' },
}

/**
 * Why `value` cannot be sent as the proxy URL of an `external` or `peer` mode, or null when it can.
 *
 * It mirrors the contract's rules (provisioner-http-proxy.md) only so the form can flag a typo
 * before the request: an absolute `http`/`https` URL with a host and optional port, and nothing
 * else — no user or password, query, fragment, or path beyond `/`. The API stays the validator of
 * record, and MAAS validates again.
 */
export function httpProxyUrlProblem(value: string): string | null {
  const trimmed = value.trim()
  if (!trimmed) return 'Enter the proxy URL.'
  let url: URL
  try {
    url = new URL(trimmed)
  } catch {
    // Not parseable at all, typically a missing scheme ("proxy:3128").
    return 'Use http://host:port or https://host:port.'
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return 'Use http://host:port or https://host:port.'
  if (!url.hostname) return 'Name the proxy host.'
  if (url.username || url.password) return 'Leave the user and password out; the URL is readable back.'
  if (url.search || url.hash || (url.pathname !== '' && url.pathname !== '/')) {
    return 'Use only the scheme, host, and port, without a path, query, or fragment.'
  }
  return null
}
