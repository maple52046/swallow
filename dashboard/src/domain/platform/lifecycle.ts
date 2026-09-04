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

/** Disabled reason for the destructive host-side uninstall command. */
export function platformUninstallDisabledReason(platform: Platform): string | undefined {
  if (platform.type !== 'kubernetes') return 'Only Kubernetes clusters can be uninstalled.'
  if (platform.origin !== 'deployed') return 'Externally registered platforms can only be deleted.'
  switch (platform.lifecycleState) {
    case 'deploying':
      return 'Deployment is still running.'
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
