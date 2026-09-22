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
  deployment_install_failed:
    'The OS installation failed on the server — the image or its install failed, not a transient error. Check the image.',
  deployment_image_unusable:
    'The provider could not use this OS image for the deployment (missing boot or kernel resources). Check the image.',
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
 * Coarser fallback keyed on the failed Step's `stage`, used when the precise `code` is
 * absent — notably for deployments that failed before the code was recorded. Less specific
 * than the code map (a readiness failure could be "no address" or "SSH down"), but still far
 * more readable than the verbose reason.
 */
const DEPLOYMENT_FAILURE_SUMMARIES_BY_STAGE: Record<string, string> = {
  ssh_readiness:
    'The server did not become reachable after OS installation (no network address or SSH).',
  deployment: 'The provisioner reported that OS deployment failed.',
  deployment_preflight: 'The deployment was rejected before it could start.',
  deployment_projection: 'Swallow could not record the deployment state.',
  network_cleanup: 'Static network cleanup after release did not complete.',
  lock_precheck: 'The server is locked, so the operation could not proceed.',
}

/**
 * Returns a short root-cause line for a failed or attention-needing deployment axis. Prefers
 * the precise stable `code`, then the coarser `stage` (so deployments that predate the code
 * still get a concise line), then the raw reason, then a generic line — always leaving the
 * verbose detail to be shown separately.
 */
export function deploymentFailureSummary(axis: DeploymentAxis): string {
  const byCode = DEPLOYMENT_FAILURE_SUMMARIES[axis.code]
  if (byCode) return byCode
  const stageKey = axis.stage.startsWith('deployment_recovery')
    ? 'deployment'
    : axis.stage
  const byStage = DEPLOYMENT_FAILURE_SUMMARIES_BY_STAGE[stageKey]
  if (byStage) return byStage
  const reason = axis.statusReason.trim()
  if (reason) return reason
  return 'Swallow could not verify this deployment.'
}
