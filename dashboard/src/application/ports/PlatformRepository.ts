import type {
  Platform,
  DeployPlatformInput,
  PlatformChangeResult,
  MembershipReport,
  UninstallPlatformOptions,
} from '@/domain/platform/types'
/**
 * Reads platforms and writes their specs. A platform's observed members are servers carrying the
 * membership axis, read through the ServerRepository, so there is no member call here.
 * Registration of an existing platform involves a credential and is done through the API by
 * a user, so it is not exposed to the dashboard.
 *
 * Callers invoke the generic spec writes (create and delete) only after the user confirmed
 * them, so each write approves the Change Plan it proposes with the request (change-plans.md,
 * "Approval On Submit"). The result says whether the plan still waits for a separate approval —
 * because of the Change Plan policy or its blockers — so the caller can send the user there.
 */
export interface PlatformRepository {
  /** All platforms, optionally scoped to one site. Small and bounded, so not paginated. */
  listPlatforms(siteId?: string): Promise<Platform[]>
  getPlatform(id: string): Promise<Platform | null>
  /**
   * Create a platform (Kubernetes/k0s or Slurm, per `input.type`) from the deployment it should
   * be; resolves with the Change Plan that installs it, approved with the request unless it waits.
   */
  createPlatform(input: DeployPlatformInput): Promise<PlatformChangeResult>
  /**
   * Delete a platform: its spec is marked absent and its uninstall is the returned Change Plan,
   * approved with the request unless it waits; the record is deleted once the uninstall succeeds.
   * A platform with nothing installed (its install never started) is deleted at once (`deleted`).
   * Deleting a platform already marked absent returns the plan it waits on, approved when the
   * policy allows, when `options` are the stored ones; different options, or no plan waiting (it
   * was rejected or expired, or the uninstall failed), propose the uninstall again. While the
   * uninstall runs the result has no plan and is not `deleted`. `options` chooses whether the
   * uninstall releases the members and cancels the Workflows holding them.
   */
  deletePlatform(id: string, options?: UninstallPlatformOptions): Promise<PlatformChangeResult>
  /** Delete only the swallow record and owned projections; hosts are untouched. */
  deletePlatformRecord(id: string): Promise<void>
  /** Read the platform's membership now instead of waiting for the background interval. */
  syncPlatform(id: string): Promise<MembershipReport>
}
