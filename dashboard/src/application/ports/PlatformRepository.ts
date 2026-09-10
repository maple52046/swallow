import type {
  Platform,
  DeployPlatformInput,
  DeployPlatformResult,
  MembershipReport,
  SlurmCluster,
  UninstallPlatformOptions,
} from '@/domain/platform/types'

/**
 * Reads platforms and deploys new ones. A platform's members are servers carrying the
 * membership axis, read through the ServerRepository, so there is no member call here.
 * Registration of an existing platform involves a credential and is done through the API by
 * an operator, so it is not exposed to the dashboard.
 */
export interface PlatformRepository {
  /** All platforms, optionally scoped to one site. Small and bounded, so not paginated. */
  listPlatforms(siteId?: string): Promise<Platform[]>
  getPlatform(id: string): Promise<Platform | null>
  /**
   * Deploy a new platform (Kubernetes/k0s or Slurm, per `input.type`); resolves once the
   * platform and its operation are accepted.
   */
  deployPlatform(input: DeployPlatformInput): Promise<DeployPlatformResult>
  /**
   * Remove k0s from the original deployment targets and retain the record. When
   * `options.releaseServers` is set, the same durable operation also releases each member
   * server back to the provider using `options.releaseOptions`.
   */
  uninstallPlatform(id: string, options?: UninstallPlatformOptions): Promise<DeployPlatformResult>
  /** Delete only the Swallow record and owned projections; hosts are untouched. */
  deletePlatform(id: string): Promise<void>
  /** Read the platform's membership now instead of waiting for the background interval. */
  syncPlatform(id: string): Promise<MembershipReport>
  /**
   * Read a Slurm platform's live cluster state (controllers, partitions, compute node
   * scheduler state) on demand. Resolves to null when there is no live view to show - a
   * non-Slurm platform, a Slurm platform whose slurmrestd integration is not recorded yet,
   * or slurmrestd being unreachable - so the Slurm view degrades to deployment intent plus
   * membership instead of surfacing an error.
   */
  getSlurmCluster(id: string): Promise<SlurmCluster | null>
}
