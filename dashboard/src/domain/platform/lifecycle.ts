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
