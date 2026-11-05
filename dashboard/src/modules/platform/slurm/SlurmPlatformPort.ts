import type { MinimumResources, SlurmCluster, SlurmDeploymentRequirement } from '@/domain/platform/types'

/** Slurm-only live reads and deployment policy, isolated from the generic Platform repository. */
export interface SlurmPlatformPort {
  getCluster(platformId: string): Promise<SlurmCluster>
  getDeploymentRequirement(): Promise<SlurmDeploymentRequirement>
  replaceDeploymentRequirement(minimum: MinimumResources | null): Promise<SlurmDeploymentRequirement>
}
