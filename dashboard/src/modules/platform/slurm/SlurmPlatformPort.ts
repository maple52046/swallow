import type { MinimumResources, PlatformChangeResult, SlurmCluster, SlurmDeploymentRequirement } from '@/domain/platform/types'

/** Slurm-only live reads and deployment policy, isolated from the generic Platform repository. */
export interface SlurmPlatformPort {
  getCluster(platformId: string): Promise<SlurmCluster>
  getDeploymentRequirement(): Promise<SlurmDeploymentRequirement>
  replaceDeploymentRequirement(minimum: MinimumResources | null): Promise<SlurmDeploymentRequirement>
  /**
   * Turns Omnistat's hardware counters on or off on a deployed Slurm Platform by writing
   * `settings.omnistat.hardwareCounters` into its current spec at the generation it was read at,
   * leaving every other part of the spec as stored (platforms.md "Change A Platform"). The write is
   * non-destructive, so it does not approve a Change Plan; a result that still awaits approval means
   * the API classified it otherwise and the caller must say so. Rejects with the request failure (for
   * example `conflict` when the spec changed in between, or the Platform is not adopted yet) and with
   * an `Error` when the stored spec has no enabled Omnistat to switch.
   */
  setHardwareCounters(platformId: string, wanted: boolean): Promise<PlatformChangeResult>
}
