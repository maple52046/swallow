import type { NetworkSubnet } from '@/domain/provisioning/types'

/** Shows a provider subnet name only when it adds information beyond the CIDR. */
export function formatSubnetOptionLabel(subnet: Pick<NetworkSubnet, 'name' | 'cidr'>): string {
  const name = subnet.name.trim()
  const cidr = subnet.cidr.trim()

  if (!name) return cidr || '-'
  if (!cidr || name === cidr) return name

  return `${name} (${cidr})`
}
