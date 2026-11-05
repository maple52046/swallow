import type { PlatformMachinePreparation } from '@/domain/platform/types'
import type {
  DeployServersInput,
  DeploymentNetworkAssignment,
  DeploymentNetworkMode,
  DeploymentUserDataMode,
  DeployTarget,
  NetworkSubnet,
  NetworkTarget,
} from '@/domain/provisioning/types'
import type { Server } from '@/domain/server/types'
import type { OSImage } from '@/domain/site/types'

/** Where the shared OS deployment flow was launched and which choices are immutable. */
export type OSDeploymentLaunchContext =
  | { kind: 'fixed-targets'; siteId: string; targets: Server[] }
  | {
      kind: 'fixed-image'
      siteId: string
      integrationId: string
      image: OSImage
    }
  | { kind: 'platform-embedded'; siteId: string; targetIds: string[] }

/** Canonical UI state shared by operation and platform deployment adapters. */
export interface OSDeploymentConfiguration {
  serverIds: string[]
  templateId?: string
  customized: boolean
  imageId: string
  deployTarget: DeployTarget
  userData: {
    mode: DeploymentUserDataMode
    value?: string
  }
  network: {
    mode: DeploymentNetworkMode
    subnetId?: string
    defaultGateway: boolean
    assignments: DeploymentNetworkAssignment[]
  }
}

/** Returns whether the launch source owns the deployment target list. */
export function hasFixedDeploymentTargets(
  context: OSDeploymentLaunchContext,
): context is Extract<OSDeploymentLaunchContext, { kind: 'fixed-targets' | 'platform-embedded' }> {
  return context.kind === 'fixed-targets' || context.kind === 'platform-embedded'
}

/** Returns whether the launch source owns the selected OS image. */
export function hasFixedDeploymentImage(
  context: OSDeploymentLaunchContext,
): context is Extract<OSDeploymentLaunchContext, { kind: 'fixed-image' }> {
  return context.kind === 'fixed-image'
}

/** Mirrors deployment acceptance when presenting or defaulting an image's target modes. */
export function osImageSupportsDeployTarget(image: OSImage, target: DeployTarget): boolean {
  return image.status.providerOsSystem !== 'custom' || image.status.verifiedDeployTargets.includes(target)
}

/**
 * Chooses a deploy target for a newly selected image.
 *
 * A single available mode wins. Both available and both unavailable deliberately fall back to RAM,
 * which is the neutral Dashboard default until the user makes an explicit choice.
 */
export function defaultDeployTargetForImage(image: OSImage | undefined): DeployTarget {
  if (!image) return 'ram'
  const diskAvailable = osImageSupportsDeployTarget(image, 'disk')
  const ramAvailable = osImageSupportsDeployTarget(image, 'ram')
  if (diskAvailable !== ramAvailable) return diskAvailable ? 'disk' : 'ram'
  return 'ram'
}

/**
 * The addressing mode a deployment batch starts from, derived from the backend's per-target
 * suggestions (contract provisioning.md, Network Inspection); the user can change it. One
 * mode applies to the whole batch, so Static wins when any target has an explicit static binding
 * (its address must be kept), and Automatic is preselected otherwise, including for no targets.
 *
 * DHCP is never preselected, not even for a suggestion that says `dhcp`: on a subnet whose DHCP
 * the provisioner does not serve the user is warned instead (see `siteDhcpConflictSubnets`) and
 * chooses DHCP explicitly (decision 058).
 */
export function suggestedBatchNetworkMode(
  suggestions: readonly DeploymentNetworkMode[],
): DeploymentNetworkMode {
  return suggestions.includes('static') ? 'static' : 'automatic'
}

/** The NIC and subnet a deployment form currently assigns to one target Server. */
interface TargetSubnetChoice {
  readonly interfaceId: string
  readonly subnetId: string
}

/**
 * Lists the distinct subnets a batch would deploy with Automatic addressing although the
 * provisioner serves no DHCP on them (`provisionerDhcp` false, contract provisioning.md, Network
 * Inspection). Provider auto-assign does not know a site DHCP server's pool, so on these subnets
 * Automatic may hand out an address the site also leases; a non-empty result is what the
 * Deploy OS dialog and the Platform wizard show `SiteDhcpAddressingWarning` for (decision 058).
 *
 * The subnet is resolved through each target's currently chosen NIC, so a per-target subnet
 * override counts and the suggestion does not. Targets without a chosen subnet, targets missing
 * from `assignments`, and every mode other than Automatic yield nothing: DHCP lets the site DHCP
 * server assign the address, and Static addresses are the user's own choice. Order follows the
 * first target that uses each subnet.
 */
export function siteDhcpConflictSubnets(
  mode: DeploymentNetworkMode,
  targets: readonly NetworkTarget[],
  assignments: Readonly<Record<string, TargetSubnetChoice | undefined>>,
): NetworkSubnet[] {
  if (mode !== 'automatic') return []
  const subnets = new Map<string, NetworkSubnet>()
  for (const target of targets) {
    const assignment = assignments[target.serverId]
    if (!assignment?.subnetId) continue
    const iface = target.network.interfaces.find((candidate) => candidate.id === assignment.interfaceId)
    const subnet = iface?.availableSubnets.find((candidate) => candidate.id === assignment.subnetId)
    if (subnet && !subnet.provisionerDhcp && !subnets.has(subnet.id)) subnets.set(subnet.id, subnet)
  }
  return [...subnets.values()]
}

/** Maps shared UI state to the standalone provisioning operation contract. */
export function toDeployServersInput(
  configuration: OSDeploymentConfiguration,
): DeployServersInput {
  return {
    serverIds: configuration.serverIds,
    templateId: configuration.templateId,
    settings: configuration.customized
      ? {
          imageId: configuration.imageId,
          deployTarget: configuration.deployTarget,
        }
      : undefined,
    userData: configuration.userData,
    network: configuration.network,
  }
}

/** Maps shared UI state to the existing Platform machine-preparation contract. */
export function toPlatformMachinePreparation(
  configuration: OSDeploymentConfiguration,
): PlatformMachinePreparation {
  return {
    mode: 'provision_os',
    templateId: configuration.templateId,
    settings: configuration.customized
      ? {
          imageId: configuration.imageId,
          deployTarget: configuration.deployTarget,
        }
      : undefined,
    userData: configuration.userData,
    network: configuration.network,
  }
}
