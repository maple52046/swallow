import type { Platform, PlatformLifecycleState } from './types'

const LABELS: Record<PlatformLifecycleState, string> = {
  registered: 'Registered',
  deploying: 'Deploying',
  deploy_failed: 'Deployment failed',
  active: 'Active',
  uninstalling: 'Uninstalling',
  uninstall_failed: 'Uninstall failed',
  uninstalled: 'Uninstalled',
}

/** Operator-facing lifecycle label supplied by the backend read model. */
export function platformLifecycleLabel(state: PlatformLifecycleState): string {
  return LABELS[state]
}

/**
 * Whether the lifecycle has durable work running (a deployment or an uninstall), so the state
 * changes without operator action. Drives the in-progress cue, polling, and filtering.
 */
export function isPlatformLifecycleInProgress(state: PlatformLifecycleState): boolean {
  return state === 'deploying' || state === 'uninstalling'
}

/** Shared semantic status family for lifecycle labels. */
export function platformLifecycleStatus(state: PlatformLifecycleState): string {
  switch (state) {
    case 'active':
      return 'active'
    case 'deploy_failed':
    case 'uninstall_failed':
      return 'failed'
    case 'deploying':
    case 'uninstalling':
      return 'running'
    case 'uninstalled':
      return 'archived'
    default:
      return 'pending'
  }
}

/**
 * Disabled reason for the destructive host-side uninstall command. A Platform still deploying
 * stays uninstallable: its dialog lists the running deployment, and the operator confirms that
 * the backend cancels it first (`cancelActiveWorkflows`, decision 061).
 */
export function platformUninstallDisabledReason(platform: Platform): string | undefined {
  // Both Kubernetes and Slurm platforms deployed by Swallow can be uninstalled; only
  // externally registered platforms (no deployment history) are delete-only.
  if (platform.origin !== 'deployed') return 'Externally registered platforms can only be deleted.'
  switch (platform.lifecycleState) {
    case 'uninstalling':
      return 'Uninstall is already running.'
    case 'uninstalled':
      return 'This platform is already uninstalled.'
    case 'registered':
      return 'This platform has no Swallow deployment history.'
    default:
      return undefined
  }
}

/**
 * What the Platform removal control leads with (decision 010, 2026-10-29 amendment): host-side
 * Uninstall, with record-only Delete as a secondary choice, or Delete alone.
 */
export type PlatformRemovalAction = 'uninstall' | 'delete'

/**
 * The live Server Lock read for a Platform's deployment targets, as a React-free value so the
 * availability rule stays testable. `checking` and `error` fail closed for Uninstall.
 */
export interface PlatformTargetProtection {
  status: 'checking' | 'ready' | 'error'
  lockedNames: readonly string[]
}

/** Whether Uninstall can be submitted now, with the operator-facing reason when it cannot. */
export interface PlatformUninstallAvailability {
  enabled: boolean
  reason?: string
}

/**
 * Which action the Platform removal control leads with. A Platform Swallow deployed leads with
 * Uninstall until its uninstall succeeds (including while one runs, when Uninstall itself is
 * disabled and only the secondary record-only Delete can be used); an `uninstalled` or
 * never-deployed Platform can only lose its record, so it leads with Delete.
 */
export function platformPrimaryRemovalAction(platform: Platform): PlatformRemovalAction {
  if (platform.origin !== 'deployed') return 'delete'
  return platform.lifecycleState === 'uninstalled' || platform.lifecycleState === 'registered' ? 'delete' : 'uninstall'
}

/**
 * Whether one Platform can be uninstalled now: the lifecycle gate of
 * `platformUninstallDisabledReason` first, then the target lock gate. Record-only Delete has no
 * such gate, because it never mutates hosts (it stays allowed for locked targets).
 */
export function platformUninstallAvailability(
  platform: Platform,
  protection: PlatformTargetProtection,
): PlatformUninstallAvailability {
  const reason = platformUninstallDisabledReason(platform) ?? targetProtectionReason(protection)
  return reason ? { enabled: false, reason } : { enabled: true }
}

function targetProtectionReason(protection: PlatformTargetProtection): string | undefined {
  if (protection.status === 'checking') return 'Checking target protection.'
  if (protection.status === 'error') return 'Target protection could not be checked. Refresh and try again.'
  const names = protection.lockedNames
  if (names.length === 0) return undefined
  if (names.length === 1) return `Server “${names[0]}” is locked. Unlock it before changing hosts.`
  return `Servers ${names.map((name) => `“${name}”`).join(', ')} are locked. Unlock them before changing hosts.`
}

/**
 * The Servers a Platform was deployed onto (role assignments plus Slurm login nodes), without
 * duplicates. An uninstall acts on exactly these, so they are what a Workflow can hold against it.
 */
export function platformDeploymentServerIds(platform: Platform): string[] {
  const ids = [
    ...(platform.deployment?.roleAssignments ?? []).map((assignment) => assignment.serverId),
    ...(platform.deployment?.loginServerIds ?? []),
  ]
  return [...new Set(ids)]
}
