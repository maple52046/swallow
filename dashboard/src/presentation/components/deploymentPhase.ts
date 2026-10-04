import type { DeploymentAxis, DeploymentState, ProvisioningAxis } from '@/domain/server/types'

/** Chakra colour palette used for axis badges; text always carries the state, colour supplements. */
export type AxisColor = 'green' | 'red' | 'orange' | 'blue' | 'gray' | 'purple'

/**
 * What a Server's Deployment cell presents, resolved from two independent facts: Swallow's own
 * result of its latest OS deployment (`deployment`) and the swallow-defined OS Provisioning State
 * (`provisioning.state`, decision 048).
 *
 * The resolution is shared by `DeploymentBadge` (list row, list card, detail header) and the list's
 * OS deployment grouping and sorting, so a group header always reads like the cells inside it.
 * Kept in this `.ts` module so `AxisBadge.tsx` only exports components and Fast Refresh keeps working.
 */
export interface DeploymentPhase {
  /** Display label, also used as the OS deployment group header. */
  label: string
  color: AxisColor
  tooltip: string
  /**
   * Work is running and the state will change without operator action. The badge shows a
   * spinner after the label; the label still names the state, so the motion is only a cue.
   */
  inProgress: boolean
  /**
   * Set only when the provider reports an installed OS. `imageName` is the effective image or
   * OS/release text ('' when none can be derived); `verified` means Swallow deployed and
   * verified it. The list cell shows the image as plain text instead of a lifecycle badge.
   */
  installed?: { imageName: string; verified: boolean }
}

const SWALLOW_DEPLOYMENT: Record<Exclude<DeploymentState, 'succeeded'>, { color: AxisColor; label: string }> = {
  deploying: { color: 'blue', label: 'Deploying' },
  verifying: { color: 'blue', label: 'Verifying' },
  failed: { color: 'red', label: 'Failed' },
  requires_attention: { color: 'orange', label: 'Attention' },
  canceled: { color: 'gray', label: 'Canceled' },
}

/**
 * Appends the provisioner's machine-level failure reason to a failure tooltip when one is present,
 * so a failed/broken/rescue badge carries the "why" (e.g. "Failed to erase disks.") inline instead
 * of only the coarse lifecycle label. Returns an empty string when there is no reason.
 */
function reasonSuffix(errorDescription?: string): string {
  const reason = errorDescription?.trim()
  return reason ? ` Reason: ${reason}.` : ''
}

/**
 * Resolves the Deployment cell for one Server.
 *
 * Precedence, highest first:
 * 1. Provider work that is running but is not an OS installation (Releasing, Inspecting, Testing).
 *    It outranks any leftover Swallow result, because that result describes an installation the
 *    running work is replacing or erasing.
 * 2. A Swallow deployment that is still running or did not succeed (Deploying, Verifying, Failed,
 *    Attention, Canceled), so the operator sees Swallow's own reason.
 * 3. The installed OS when the provider reports `deployed`.
 * 4. Every other OS Provisioning State by its own name (Deploying, Broken, Failed, Rescue, Ready,
 *    Allocated, New, Retired, Unknown). A live provider state outranks a stale Swallow success:
 *    an `allocated` Machine must never read as a stale "Deployed" (decision 036).
 * 5. A Swallow success with no provider observation at all, then Unknown.
 *
 * Callers decide which inputs are current: the Server list passes no provider axis for an absent
 * Server and drops a `succeeded` result once the OS is no longer installed.
 */
export function resolveDeploymentPhase(
  axis: DeploymentAxis | null,
  provider: ProvisioningAxis | null,
): DeploymentPhase {
  switch (provider?.state) {
    case 'releasing':
      return { label: 'Releasing', color: 'orange', inProgress: true, tooltip: 'The provisioner is returning this Server to its available pool.' }
    case 'inspecting':
      return { label: 'Inspecting', color: 'blue', inProgress: true, tooltip: 'The provisioner is inventorying this Server\'s hardware.' }
    case 'testing':
      return { label: 'Testing', color: 'blue', inProgress: true, tooltip: 'The provisioner is running hardware tests on this Server.' }
  }

  if (axis && axis.state !== 'succeeded') {
    const { color, label } = SWALLOW_DEPLOYMENT[axis.state]
    return {
      label,
      color,
      inProgress: axis.state === 'deploying' || axis.state === 'verifying',
      tooltip: axis.statusReason || `Operation ${axis.operationId}, attempt ${axis.attempt}`,
    }
  }

  if (provider?.state === 'deployed') {
    const verified = axis?.state === 'succeeded'
    const fallback = [provider.osSystem, provider.distroSeries].filter(Boolean).join(' ')
    return {
      label: 'Deployed',
      color: verified ? 'green' : 'gray',
      inProgress: false,
      tooltip: verified
        ? 'Swallow deployed and verified this OS image.'
        : 'Operating system reported by the provisioner; not deployed by Swallow.',
      installed: { imageName: provider.deployedImageName || fallback, verified },
    }
  }

  if (provider) {
    switch (provider.state) {
      case 'deploying':
        return { label: 'Deploying', color: 'blue', inProgress: true, tooltip: 'The provisioner is installing an operating system; no verified Swallow result exists yet.' }
      case 'broken':
        // Broken and Failed are distinct recovery cases (decision 033) and must stay
        // distinguishable at a glance: Broken is a provider-marked unusable Machine (cleared with
        // Mark fixed or Recover), Failed is a last-lifecycle failure (Recover or Release).
        return { label: 'Broken', color: 'red', inProgress: false, tooltip: `Provider marked this Machine broken. Provider lifecycle: ${provider.providerState}.${reasonSuffix(provider.errorDescription)} Recover or Release returns it to Ready.` }
      case 'failed':
        return { label: 'Failed', color: 'red', inProgress: false, tooltip: `Provider lifecycle failed: ${provider.providerState}.${reasonSuffix(provider.errorDescription)} Recover or Release returns it to Ready.` }
      case 'rescue':
        return { label: 'Rescue', color: 'purple', inProgress: false, tooltip: `Diagnostic rescue environment. Provider lifecycle: ${provider.providerState}.${reasonSuffix(provider.errorDescription)} Exit rescue restores the previous state; Recover returns it to Ready.` }
      case 'ready':
        return { label: 'Ready', color: 'blue', inProgress: false, tooltip: 'The Server is in the provisioner\'s available pool, ready to be deployed.' }
      case 'allocated':
        return { label: 'Allocated', color: 'blue', inProgress: false, tooltip: 'The Server is reserved (allocated) but not deployed. Recover or Release returns it to the ready pool.' }
      case 'new':
        return { label: 'New', color: 'gray', inProgress: false, tooltip: 'The provisioner discovered this Server but has not inspected its hardware, so it cannot be deployed yet.' }
      case 'retired':
        return { label: 'Retired', color: 'gray', inProgress: false, tooltip: 'The Server is withdrawn from service.' }
      default:
        // Only `unknown` reaches here: every other state returned above.
        return { label: 'Unknown', color: 'gray', inProgress: false, tooltip: `The provisioner reported a state Swallow does not recognise: ${provider.providerState}.` }
    }
  }

  if (axis?.state === 'succeeded') {
    return { label: 'Deployed', color: 'green', inProgress: false, tooltip: 'Swallow deployed and verified this OS image.' }
  }
  return { label: 'Unknown', color: 'gray', inProgress: false, tooltip: 'The provisioner is not reporting a provisioning state for this Server.' }
}
