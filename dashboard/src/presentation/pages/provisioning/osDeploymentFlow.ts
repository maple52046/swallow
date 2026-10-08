import type { PlatformMachinePreparation } from '@/domain/platform/types'
import type {
  DeployServersInput,
  DeploymentNetworkAssignment,
  DeploymentNetworkMode,
  DeploymentUserDataMode,
  DeployTarget,
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
  return image.providerOsSystem !== 'custom' || image.verifiedDeployTargets.includes(target)
}

/**
 * Chooses a deploy target for a newly selected image.
 *
 * A single available mode wins. Both available and both unavailable deliberately fall back to RAM,
 * which is the neutral Dashboard default until the operator makes an explicit choice.
 */
export function defaultDeployTargetForImage(image: OSImage | undefined): DeployTarget {
  if (!image) return 'ram'
  const diskAvailable = osImageSupportsDeployTarget(image, 'disk')
  const ramAvailable = osImageSupportsDeployTarget(image, 'ram')
  if (diskAvailable !== ramAvailable) return diskAvailable ? 'disk' : 'ram'
  return 'ram'
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
          ephemeral: configuration.deployTarget === 'ram',
        }
      : undefined,
    userData: configuration.userData,
    network: configuration.network,
  }
}
