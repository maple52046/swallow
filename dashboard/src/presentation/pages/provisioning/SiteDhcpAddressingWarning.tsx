import type { NetworkSubnet } from '@/domain/provisioning/types'
import { Alert } from '@/presentation/components/ui/alert'
import { formatSubnetOptionLabel } from '@/presentation/utils/network'

interface SiteDhcpAddressingWarningProps {
  /** The non-empty result of `siteDhcpConflictSubnets`; each subnet is named in the warning. */
  subnets: readonly NetworkSubnet[]
  className?: string
}

/**
 * Warns on the networking step of the Deploy OS dialog and the Platform deploy wizard that
 * Automatic addressing on subnets without provisioner DHCP may conflict with the site DHCP pool,
 * and that DHCP is the alternative (contract provisioning.md, Network Inspection; decision 058).
 *
 * swallow keeps Automatic preselected on these subnets by decision, so this warning, not a changed
 * default, is how the risk reaches the user; it never changes the mode and never blocks the step.
 * Callers render it only while the batch mode is Automatic, so it disappears once the user picks
 * DHCP or Static. The warning status is carried by the alert's indicator and text, not by colour,
 * and the affected subnets are named so a mixed batch shows which networks are at risk.
 */
export function SiteDhcpAddressingWarning({ subnets, className }: SiteDhcpAddressingWarningProps) {
  const subnetNames = subnets.map((subnet) => formatSubnetOptionLabel(subnet)).join(', ')
  return (
    <Alert
      status="warning"
      title="Automatic addressing may conflict with the site DHCP pool"
      className={className}
    >
      The provisioner runs no DHCP on {subnetNames}, so any DHCP there is the site&apos;s. Automatic
      assigns an address the site DHCP server does not know about and may also lease to another
      machine. Choose DHCP to let the site DHCP server assign the address, gateway, and DNS instead.
    </Alert>
  )
}
