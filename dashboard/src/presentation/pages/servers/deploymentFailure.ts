import type { DeploymentAxis } from '@/domain/server/types'

/**
 * Concise, operator-facing root cause for each stable deployment failure code.
 *
 * The executor's `statusReason` is a verbose recovery instruction meant for the Operation
 * troubleshooting view; on the Server detail page we want a single plain sentence. Keying
 * on the stable error `code` keeps this mapping robust (no message-string parsing) and lets
 * the full text stay available behind "Show details" / "View operation".
 */
const DEPLOYMENT_FAILURE_SUMMARIES: Record<string, string> = {
  deployment_address_unavailable:
    'The server did not obtain a network address (DHCP) after the OS was installed.',
  deployment_ssh_unreachable:
    'The OS was installed but the server’s SSH endpoint was not reachable.',
  deployment_failed: 'The provisioner reported that the OS deployment failed.',
  deployment_recovery_unavailable: 'Automated deployment recovery is unavailable.',
  deployment_readiness_unavailable: 'Swallow could not check the server’s readiness.',
  deployment_projection_unavailable: 'Swallow could not record the deployment state.',
  intent_snapshot_incomplete:
    'The deployment intent is incomplete; start a new deployment.',
  target_locked: 'The server is locked, so the deployment could not proceed.',
  provider_auth: 'The provider rejected the configured credentials.',
  provider_rejected: 'The provider rejected the deployment request.',
  provider_unavailable: 'The provider was unavailable during deployment.',
  provider_outcome_unknown:
    'The deployment outcome could not be confirmed and needs review.',
  secret_unavailable: 'The deployment’s cloud-init data could not be resolved.',
  secret_invalid: 'The deployment’s stored cloud-init data is invalid.',
}

/**
 * Returns a short root-cause line for a failed or attention-needing deployment axis, keyed
 * on its stable `code`. Falls back to the raw reason, then a generic line, so an unmapped
 * code still says something useful while the detail stays available separately.
 */
export function deploymentFailureSummary(axis: DeploymentAxis): string {
  const mapped = DEPLOYMENT_FAILURE_SUMMARIES[axis.code]
  if (mapped) return mapped
  const reason = axis.statusReason.trim()
  if (reason) return reason
  return 'Swallow could not verify this deployment.'
}
