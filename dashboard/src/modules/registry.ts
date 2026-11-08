import type { ComponentType } from 'react'
import type { Platform, PlatformType } from '@/domain/platform/types'
import { amdModule } from './accelerator/amd'
import { kubernetesModule } from './platform/kubernetes'
import { slurmModule } from './platform/slurm'
import type { AcceleratorModuleUI, PlatformDeployFeatureContribution, PlatformModuleUI, PlatformMonitoringEntry, PlatformSectionProps } from './types'

export type {
  PlatformDeployFeature,
  PlatformDeployFeatureContext,
  PlatformDeployFeatureContribution,
  PlatformFact,
  PlatformModuleUI,
  PlatformMonitoringServerStates,
} from './types'

/**
 * The compiled-in module UIs, the dashboard's counterpart of the api-server module registry. This
 * is the only file outside `src/modules/**` may import (lint rule); adding a module means adding
 * its manifest here.
 */
const platformModules: readonly PlatformModuleUI[] = [kubernetesModule, slurmModule]
const acceleratorModules: readonly AcceleratorModuleUI[] = [amdModule]

/** Finds the compiled-in UI manifest for one canonical Platform type. */
export function platformModuleUI(type: PlatformType): PlatformModuleUI | undefined {
  return platformModules.find((module) => module.id === type)
}

/** The global settings surface declared by one Platform module, when it has one. */
export function platformSettingsUI(type: PlatformType) {
  return platformModuleUI(type)?.Settings
}

/** Module-owned Configuration sections for `type`, in stable registry order. */
export function platformConfigurationSections(type: PlatformType): ComponentType<PlatformSectionProps>[] {
  return [
    ...(platformModuleUI(type)?.configurationSections ?? []),
    ...acceleratorModules.flatMap((module) => module.configurationSections
      .filter((section) => section.platformType === type)
      .map((section) => section.Section)),
  ]
}

/** Platform-type and accelerator Monitoring entries, composed in stable registry order. */
export function platformMonitoringEntries(type: PlatformType, platform: Platform): PlatformMonitoringEntry[] {
  return [
    ...(platformModuleUI(type)?.monitoringEntries?.(platform) ?? []),
    ...acceleratorModules.flatMap((module) => module.monitoringEntries
      .filter((contribution) => contribution.platformType === type)
      .flatMap((contribution) => contribution.entries(platform))),
  ]
}

/** The deploy features other modules contribute to Platforms of `type`, in registry order. */
export function platformDeployFeatures(type: PlatformType): PlatformDeployFeatureContribution[] {
  return acceleratorModules.flatMap((module) => module.deployFeatures.filter((feature) => feature.platformType === type))
}
