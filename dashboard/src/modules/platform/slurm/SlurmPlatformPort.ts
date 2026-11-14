import type { MinimumResources, PlatformChangeResult, SlurmCluster, SlurmDeploymentRequirement } from '@/domain/platform/types'
import type { SlurmTopologyChange } from './slurmTopology'

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
  /**
   * Applies one change to a Slurm Platform's members, node groups, or partitions (platforms.md "Node
   * groups and partitions" and "Remove members") by writing the current spec, read at its generation,
   * with only that change. Every write carries `approve=true`: the caller confirmed it in a dialog that
   * named its consequences, so a destructive change starts unless the Change Plan policy or a blocker
   * keeps it waiting, which the result reports. The result carries the API's `warnings`. Rejects with
   * the request failure: `conflict` when the spec changed in between or the change is one swallow
   * refuses (the message says why), `validation_error` for an invalid topology or a Server below its
   * template's minimum hardware.
   */
  changeTopology(platformId: string, change: SlurmTopologyChange): Promise<PlatformChangeResult>
  /**
   * Re-detects one compute node's GPUs and reconfigures the cluster (platforms.md "Rescan A Slurm
   * Node") and resolves to the Workflow id. Rejects with `conflict` when the Server is not an applied
   * compute member or another change of the Platform is running.
   */
  rescanNode(platformId: string, serverId: string): Promise<string>
}
