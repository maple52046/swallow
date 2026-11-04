import type { PlatformType } from '@/domain/platform/types'
import { amdModule } from './accelerator/amd'
import { kubernetesModule } from './platform/kubernetes'
import { slurmModule } from './platform/slurm'
import type { AcceleratorModuleUI, PlatformDeployFeatureContribution, PlatformModuleUI, PlatformSectionContribution } from './types'

export type {
  PlatformDeployFeature,
  PlatformDeployFeatureContext,
  PlatformDeployFeatureContribution,
  PlatformFact,
  PlatformModuleUI,
} from './types'

/**
 * The compiled-in module UIs, the dashboard's counterpart of the api-server module registry. This
 * is the only file outside `src/modules/**` may import (lint rule); adding a module means adding
 * its manifest here.
 */
const platformModules: readonly PlatformModuleUI[] = [kubernetesModule, slurmModule]
const acceleratorModules: readonly AcceleratorModuleUI[] = [amdModule]

/** The module that owns a Platform type, or undefined for a type no module ships a UI for. */
export function platformModuleUI(type: PlatformType): PlatformModuleUI | undefined {
  return platformModules.find((module) => module.id === type)
}

/** The Overview sections other modules contribute to Platforms of `type`, in registry order. */
export function platformSections(type: PlatformType): PlatformSectionContribution[] {
  return acceleratorModules.flatMap((module) => module.platformSections.filter((section) => section.platformType === type))
}

/** The deploy features other modules contribute to Platforms of `type`, in registry order. */
export function platformDeployFeatures(type: PlatformType): PlatformDeployFeatureContribution[] {
  return acceleratorModules.flatMap((module) => module.deployFeatures.filter((feature) => feature.platformType === type))
}
