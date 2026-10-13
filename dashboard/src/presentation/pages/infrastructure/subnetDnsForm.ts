import { MAX_SUBNET_DNS_SERVERS } from '@/domain/provisioning/types'

/** The outcome of reading the DNS servers field: the list to send, or why it cannot be sent. */
export type DnsServerListParse = { servers: string[]; problem: null } | { servers: null; problem: string }

const IPV4 = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/

/**
 * The canonical form of one address, or null when it is not one. IPv4 must be dotted decimal
 * without leading zeros (the API refuses them too); IPv6 is canonicalized by the URL parser, which
 * lower-cases and compresses it the way the API reports it. A zone (`%eth0`) is not an address
 * here: it names an interface of the operator's own host.
 */
function canonicalAddress(token: string): string | null {
  if (IPV4.test(token)) return token
  if (!token.includes(':') || token.includes('%')) return null
  try {
    const host = new URL(`http://[${token}]/`).hostname
    return host.slice(1, -1)
  } catch {
    // The URL parser throws for anything that is not an IPv6 literal, which is exactly "not one".
    return null
  }
}

/**
 * Reads the DNS servers field of one subnet: addresses separated by commas or whitespace, in the
 * order deployed machines should use them. An empty field is valid and clears the Subnet DNS.
 *
 * It mirrors the contract's rules (provisioner-subnet-dns.md) — IP addresses only, no repeats, at
 * most eight — only so the form can flag a typo before the request; the API stays the validator of
 * record, and MAAS validates again.
 */
export function parseDnsServerList(value: string): DnsServerListParse {
  const tokens = value.split(/[\s,]+/).filter(Boolean)
  if (tokens.length > MAX_SUBNET_DNS_SERVERS) {
    return { servers: null, problem: `List at most ${MAX_SUBNET_DNS_SERVERS} DNS servers.` }
  }
  const servers: string[] = []
  for (const token of tokens) {
    const address = canonicalAddress(token)
    if (!address) return { servers: null, problem: `${token} is not an IP address.` }
    if (servers.includes(address)) return { servers: null, problem: `${address} is listed twice.` }
    servers.push(address)
  }
  return { servers, problem: null }
}

/** The field text for a list read from the provisioner, the form `parseDnsServerList` reads back. */
export function formatDnsServerList(servers: readonly string[]): string {
  return servers.join(', ')
}
