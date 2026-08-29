import type { Cluster, ClusterLifecycleState } from './types'

const LABELS: Record<ClusterLifecycleState, string> = {
  registered: 'Registered',
  deploying: 'Deploying',
  deploy_failed: 'Deployment failed',
  active: 'Active',
  uninstalling: 'Uninstalling',
  uninstall_failed: 'Uninstall failed',
  uninstalled: 'Uninstalled',
}

/** Operator-facing lifecycle label supplied by the backend read model. */
export function clusterLifecycleLabel(state: ClusterLifecycleState): string {
  return LABELS[state]
}

/** Shared semantic status family for lifecycle labels. */
export function clusterLifecycleStatus(state: ClusterLifecycleState): string {
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
export function clusterUninstallDisabledReason(cluster: Cluster): string | undefined {
  if (cluster.type !== 'kubernetes') return 'Only Kubernetes clusters can be uninstalled.'
  if (cluster.origin !== 'deployed') return 'Externally registered clusters can only be deleted.'
  switch (cluster.lifecycleState) {
    case 'deploying':
      return 'Deployment is still running.'
    case 'uninstalling':
      return 'Uninstall is already running.'
    case 'uninstalled':
      return 'This cluster is already uninstalled.'
    case 'registered':
      return 'This cluster has no Swallow deployment history.'
    default:
      return undefined
  }
}
