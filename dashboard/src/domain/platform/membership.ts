import type { Platform } from './types'

/**
 * The Servers the Platform's deployment intent names as controllers: its `control-plane` role
 * assignments (platforms.md "Platform Resource": a Slurm controller projects to one too).
 *
 * The Platform's own API does not report every controller as a member: slurmrestd lists only nodes
 * that run slurmd, so a controller-only Slurm node never appears in membership, while k0s reports
 * dedicated controllers through its lease discovery. Every view that counts or lists members adds
 * these controllers to what the API reports, exactly once, so a Platform reads the same everywhere.
 */
export function platformDeploymentControllerIds(platform: Platform): string[] {
  return (platform.deployment?.roleAssignments ?? [])
    .filter((assignment) => assignment.role === 'control-plane')
    .map((assignment) => assignment.serverId)
}

/** A Platform's member totals as the Platforms list shows them, controllers included. */
export interface PlatformMemberCounts {
  /** Members that are swallow Servers: matched reported members plus unreported controllers. */
  matched: number
  /** Every member: reported members (matched or not) plus unreported controllers. */
  total: number
}

/**
 * Counts a Platform's members the way the Platform detail page lists them, from the list response
 * alone (no Server reads).
 *
 * The detail page shows the Servers whose membership names the Platform plus each deployment
 * controller (`platformDeploymentControllerIds`) that is not among them. The list response carries
 * only the counts `sync.matchedCount` and `sync.memberCount`, not which Servers they are, so this
 * helper assumes the matched members are drawn from the deployment's role assignments (a matched
 * member is a swallow Server the deployment put there) and attributes the assignments that were not
 * reported to its controllers first: at most every controller, and never more than the assignments
 * minus the matched members. For a Slurm Platform with one controller-only node and two compute
 * nodes that is 2 matched + 1 controller = 3; for k0s, whose API already reports its controllers,
 * nothing is added. Before the first sync both pages show just the controllers. A Platform without
 * a deployment projection (registered or legacy) shows the sync counts unchanged.
 */
export function platformMemberCounts(platform: Platform): PlatformMemberCounts {
  const assignments = platform.deployment?.roleAssignments.length ?? 0
  const unreportedAssignments = Math.max(0, assignments - platform.sync.matchedCount)
  const unreportedControllers = Math.min(platformDeploymentControllerIds(platform).length, unreportedAssignments)
  return {
    matched: platform.sync.matchedCount + unreportedControllers,
    total: platform.sync.memberCount + unreportedControllers,
  }
}
