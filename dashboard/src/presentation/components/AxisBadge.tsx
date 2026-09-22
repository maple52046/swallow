import { Badge, Box, HStack, type BadgeProps } from '@chakra-ui/react'
import { Lock, MemoryStick } from 'lucide-react'
import type { ReactNode } from 'react'
import type { DeploymentAxis, DeploymentState, HealthAxis, MembershipAxis, ProvisioningAxis } from '@/domain/server/types'
import { POWER_PRESENTATION } from './axisBadgeUtils'
import { Tooltip } from '@/presentation/components/ui/tooltip'

/** Chakra colour palette used for axis badges; text always carries the state, colour supplements. */
type AxisColor = 'green' | 'red' | 'orange' | 'blue' | 'gray' | 'purple'

/**
 * CSS class (in `src/index.css`) overlaying animated diagonal stripes on a badge to
 * signal a step that is still running. Applied only to the in-progress branches of
 * {@link DeploymentBadge}; the badge text and tooltip stay the authoritative state,
 * so the motion is a supplementary cue only.
 */
const IN_PROGRESS_LABEL_CLASS = 'sw-label-progress'

/** Deployment axis states that mean work is still in flight (vs. a terminal outcome). */
const IN_PROGRESS_DEPLOYMENT_STATES: ReadonlySet<DeploymentState> = new Set(['deploying', 'verifying'])

const PROVISIONING_COLORS: Record<string, AxisColor> = {
  deployed: 'green', ready: 'blue', allocated: 'blue', deploying: 'orange', releasing: 'orange',
  commissioning: 'orange', testing: 'orange', new: 'gray', retired: 'gray', rescue: 'purple',
  broken: 'red', failed: 'red', unknown: 'gray',
}

const DEPLOYMENT_PRESENTATION: Record<DeploymentState, { color: AxisColor; label: string }> = {
  deploying: { color: 'blue', label: 'Deploying' },
  verifying: { color: 'blue', label: 'Verifying' },
  succeeded: { color: 'green', label: 'Deployed' },
  failed: { color: 'red', label: 'Failed' },
  requires_attention: { color: 'orange', label: 'Attention' },
  canceled: { color: 'gray', label: 'Canceled' },
}

/** Icon tint per power state; only running/error are tinted, the rest stay neutral. */
const POWER_TINT: Record<ProvisioningAxis['powerState'], string> = {
  on: 'green.fg',
  off: 'fg.muted',
  error: 'red.fg',
  unknown: 'fg.muted',
}

interface AxisLabelProps extends Pick<BadgeProps, 'className'> {
  color: AxisColor
  tooltip: string
  icon?: ReactNode
  children: ReactNode
}

/** Shared tooltip-wrapped badge for every axis; keeps colour, text, and hover detail consistent. */
function AxisLabel({ color, tooltip, icon, className, children }: AxisLabelProps) {
  return (
    <Tooltip content={tooltip}>
      <Badge colorPalette={color} variant="subtle" className={className} gap="1">
        {icon}
        {children}
      </Badge>
    </Tooltip>
  )
}

function UnknownBadge({ tooltip }: { tooltip: string }) {
  return <AxisLabel color="gray" tooltip={tooltip}>unknown</AxisLabel>
}

function observedAtLabel(observedAt: string): string {
  return `Observed ${new Date(observedAt).toLocaleString()}`
}

/**
 * Appends the provisioner's machine-level failure reason to a failure tooltip when one is present,
 * so a failed/broken/rescue badge carries the "why" (e.g. "Failed to erase disks.") inline instead
 * of only the coarse provider lifecycle label. Returns an empty string when there is no reason.
 */
function reasonSuffix(errorDescription?: string): string {
  const reason = errorDescription?.trim()
  return reason ? ` Reason: ${reason}.` : ''
}

/**
 * Marks an ephemeral (run-from-RAM) deployment with a memory-stick glyph beside the
 * state badge. Meaning is carried by the tooltip and `aria-label`, not colour alone;
 * the warning tint is only a supplementary cue.
 */
function EphemeralIndicator() {
  return (
    <Tooltip content="Ephemeral deployment; root filesystem changes are lost on reboot">
      <Box as="span" role="img" aria-label="Ephemeral deployment" display="inline-flex" color="orange.fg">
        <MemoryStick size={14} />
      </Box>
    </Tooltip>
  )
}

/**
 * Swallow-owned deployment outcome; this is the primary deployability result.
 *
 * Two presentations. The default "merged" mode (used in the dense Server list) folds the
 * deployed OS image name and the ephemeral qualifier into the one badge, so an operator scans
 * both the state and the image in a single column. Pass `stateOnly` for the detail page, where
 * deployment state, deployed OS, and ephemeral each get their own field: the badge then shows
 * only the state and never the image name or the ephemeral icon.
 */
export function DeploymentBadge({
  axis,
  provider,
  stateOnly = false,
}: {
  axis: DeploymentAxis | null
  provider: ProvisioningAxis | null
  stateOnly?: boolean
}) {
  let state: ReactNode
  // The releasing / commissioning / testing / deploying provider states and the in-progress
  // deployment axis states are all "work still running", so each carries the stripe animation;
  // every terminal branch below stays static.
  if (provider?.state === 'releasing') {
    state = <AxisLabel color="orange" className={IN_PROGRESS_LABEL_CLASS} tooltip="The provider is returning this Server to its available pool.">Releasing</AxisLabel>
  } else if (provider?.state === 'commissioning') {
    state = <AxisLabel color="blue" className={IN_PROGRESS_LABEL_CLASS} tooltip="The provider is commissioning this Server.">Commissioning</AxisLabel>
  } else if (provider?.state === 'testing') {
    state = <AxisLabel color="blue" className={IN_PROGRESS_LABEL_CLASS} tooltip="The provider is testing this Server.">Testing</AxisLabel>
  } else if (axis && axis.state !== 'succeeded') {
    // A swallow deployment that is still running or ended in a non-success outcome keeps its
    // own state label; only a succeeded deployment shows the image name (handled below).
    const presentation = DEPLOYMENT_PRESENTATION[axis.state]
    const detail = axis.statusReason || `Operation ${axis.operationId}, attempt ${axis.attempt}`
    const stripes = IN_PROGRESS_DEPLOYMENT_STATES.has(axis.state) ? IN_PROGRESS_LABEL_CLASS : undefined
    state = <AxisLabel color={presentation.color} className={stripes} tooltip={detail}>{presentation.label}</AxisLabel>
  } else if (provider?.state === 'deployed') {
    // A deployed machine. In state-only mode the badge shows just the deployment state; in
    // merged mode it shows the OS image name (the effective name mirrored by reconcile: provider
    // catalog title overlaid with any swallow custom name), falling back to OS + release so the
    // list cell is never blank. Either way a swallow-verified deployment reads green while an
    // externally deployed machine reads neutral, since swallow has no verification result for it.
    const swallowVerified = axis?.state === 'succeeded'
    if (stateOnly) {
      state = swallowVerified ? (
        <AxisLabel color="green" tooltip="Swallow deployed and verified this OS.">Deployed</AxisLabel>
      ) : (
        <AxisLabel color="gray" tooltip="The OS is installed, but no Swallow deployment result exists. See the Deployed OS field for the installed image.">Unknown</AxisLabel>
      )
    } else {
      const fallback = [provider.osSystem, provider.distroSeries].filter(Boolean).join(' ')
      const label = provider.deployedImageName || fallback || 'Deployed'
      const tooltip = swallowVerified
        ? 'Swallow deployed and verified this OS image.'
        : 'Operating system reported by the provider; not deployed by swallow.'
      state = <AxisLabel color={swallowVerified ? 'green' : 'gray'} tooltip={tooltip}>{label}</AxisLabel>
    }
  } else if (provider?.state === 'deploying') {
    state = <AxisLabel color="blue" className={IN_PROGRESS_LABEL_CLASS} tooltip="The provider is installing an operating system; no verified Swallow result exists yet.">Deploying</AxisLabel>
  } else if (provider?.state === 'broken') {
    // Broken and Failed are distinct recovery cases (decision 033) and must stay
    // distinguishable at a glance: Broken is a provider-marked unusable Machine (cleared with
    // Mark fixed or Recover), Failed is a last-lifecycle failure (Recover or Release).
    state = <AxisLabel color="red" tooltip={`Provider marked this Machine broken. Provider lifecycle: ${provider.providerState}.${reasonSuffix(provider.errorDescription)} Recover or Release returns it to Ready.`}>Broken</AxisLabel>
  } else if (provider?.state === 'failed') {
    state = <AxisLabel color="red" tooltip={`Provider lifecycle failed: ${provider.providerState}.${reasonSuffix(provider.errorDescription)} Recover or Release returns it to Ready.`}>Failed</AxisLabel>
  } else if (provider?.state === 'rescue') {
    state = <AxisLabel color="purple" tooltip={`Diagnostic rescue environment. Provider lifecycle: ${provider.providerState}.${reasonSuffix(provider.errorDescription)} Exit rescue restores the previous state; Recover returns it to Ready.`}>Rescue</AxisLabel>
  } else if (provider?.state === 'ready') {
    // A released machine is back in the provider's available pool. Show it as "Ready"
    // (the provider's own term) rather than "Not deployed", which reads like a fault.
    state = <AxisLabel color="blue" tooltip="The Server is in the provider's available pool, ready to be deployed.">Ready</AxisLabel>
  } else if (provider?.state === 'allocated') {
    // Reserved but not deployed. This is a live provider state, so it must be shown here —
    // before the succeeded fallback below — otherwise a leftover succeeded deployment record
    // (for example a reservation that never finished deploying) would paint a stale "Deployed"
    // on a Machine that the provider is not actually running, which then reads as un-releasable.
    state = <AxisLabel color="blue" tooltip="The Server is reserved (allocated) but not deployed. Recover or Release returns it to the ready pool.">Allocated</AxisLabel>
  } else if (axis?.state === 'succeeded') {
    // A verified swallow deployment with no current provisioning projection still reads as
    // deployed; the image name is unavailable without the provider axis, so fall back to a label.
    const label = stateOnly ? 'Deployed' : provider?.deployedImageName || 'Deployed'
    state = <AxisLabel color="green" tooltip="Swallow deployed and verified this OS image.">{label}</AxisLabel>
  } else {
    state = <AxisLabel color="gray" tooltip="No operating system deployment is active or verified.">Not deployed</AxisLabel>
  }

  // In state-only mode the ephemeral qualifier is shown as its own field by the caller, so the
  // badge never appends the icon; the merged list badge keeps it as a compact supplementary cue.
  if (stateOnly || !provider?.ephemeral) return state
  return (
    <HStack gap="1" flexWrap="nowrap">
      {state}
      <EphemeralIndicator />
    </HStack>
  )
}

/** Provider-owned OS installation fact, deliberately independent from deployment success. */
export function OperatingSystemBadge({ axis }: { axis: ProvisioningAxis | null }) {
  if (!axis) return <UnknownBadge tooltip="The provisioner has not reported an operating system state." />
  if (axis.state === 'deployed') {
    const image = [axis.osSystem, axis.distroSeries].filter(Boolean).join(' ')
    return <AxisLabel color="blue" tooltip={image || 'The provisioner reports an installed operating system.'}>Installed</AxisLabel>
  }
  if (axis.state === 'deploying') {
    return <AxisLabel color="blue" tooltip={`Provider lifecycle: ${axis.providerState}`}>Installing</AxisLabel>
  }
  if (axis.state === 'releasing') {
    return <AxisLabel color="orange" tooltip={`Provider lifecycle: ${axis.providerState}`}>Removing</AxisLabel>
  }
  if (['new', 'ready', 'allocated', 'retired'].includes(axis.state)) {
    return <AxisLabel color="gray" tooltip={`Provider lifecycle: ${axis.providerState}`}>Not installed</AxisLabel>
  }
  return <UnknownBadge tooltip={`Provider lifecycle: ${axis.providerState}`} />
}

/** Reachability proven by the latest Swallow OS deployment, not physical NIC link state. */
export function DeploymentNetworkBadge({ axis }: { axis: DeploymentAxis | null }) {
  if (!axis) return <UnknownBadge tooltip="No Swallow deployment has recorded a network reachability result." />
  if (axis.state === 'succeeded') {
    return <AxisLabel color="green" tooltip="Swallow observed an address and reached the configured SSH endpoint.">Reachable</AxisLabel>
  }
  if (axis.state === 'verifying') {
    return <AxisLabel color="blue" tooltip="Swallow is checking the observed address and SSH endpoint.">Checking</AxisLabel>
  }
  if (axis.state === 'deploying') {
    return <AxisLabel color="gray" tooltip="Network reachability is checked after OS installation.">Pending</AxisLabel>
  }
  if (axis.state === 'failed' && axis.stage === 'ssh_readiness') {
    return <AxisLabel color="red" tooltip={axis.statusReason || 'The deployment address or SSH endpoint was not reachable.'}>Unreachable</AxisLabel>
  }
  if (axis.state === 'failed' && axis.stage.includes('network')) {
    return <AxisLabel color="red" tooltip={axis.statusReason || 'Network configuration failed.'}>Configuration failed</AxisLabel>
  }
  if (axis.state === 'requires_attention' && axis.stage === 'ssh_readiness') {
    return <AxisLabel color="orange" tooltip={axis.statusReason || 'Swallow could not complete network verification.'}>Unknown</AxisLabel>
  }
  return <AxisLabel color="gray" tooltip="This deployment did not complete network reachability verification.">Not checked</AxisLabel>
}

/** Provisioner-owned lifecycle state, including its Ephemeral deployment qualifier. */
export function ProvisioningBadge({ axis }: { axis: ProvisioningAxis | null }) {
  if (!axis) return <UnknownBadge tooltip="Provisioning state has never been observed" />
  const state = (
    <AxisLabel color={PROVISIONING_COLORS[axis.state] ?? 'gray'} tooltip={`${axis.providerState} - ${observedAtLabel(axis.observedAt)}`}>
      {axis.state}
    </AxisLabel>
  )
  if (!axis.ephemeral) return state
  return (
    <HStack gap="1" flexWrap="nowrap">
      {state}
      <EphemeralIndicator />
    </HStack>
  )
}

/**
 * Provisioner-owned power state shown as a compact icon in dense Server tables,
 * replacing raw on/off text so fleet power is scannable at a glance.
 *
 * Pass `powerState` as `null` when no provisioning projection exists — that renders
 * a neutral dash rather than implying a real "off". By default the icon shape, its
 * tooltip, and `aria-label` convey the state and the green/red tint is only a
 * supplementary cue. Set `decorative` when nested inside an interactive control that
 * already owns the accessible name/tooltip; the icon then renders `aria-hidden`.
 */
export function PowerBadge({
  powerState,
  decorative = false,
}: {
  powerState: ProvisioningAxis['powerState'] | null
  decorative?: boolean
}) {
  // No provisioning projection means no power fact at all; a neutral dash reads as
  // "no data" instead of a misleading powered-off icon.
  if (powerState === null) {
    return (
      <Box as="span" aria-hidden color="fg.muted">
        -
      </Box>
    )
  }
  const { Icon, label } = POWER_PRESENTATION[powerState]
  const tint = POWER_TINT[powerState]
  if (decorative) {
    return (
      <Box as="span" aria-hidden display="inline-flex" color={tint}>
        <Icon size={16} />
      </Box>
    )
  }
  return (
    <Tooltip content={label}>
      <Box as="span" role="img" aria-label={label} display="inline-flex" color={tint}>
        <Icon size={16} aria-hidden />
      </Box>
    </Tooltip>
  )
}

/** Provider-owned mutation protection, deliberately separate from lifecycle state. */
export function LockBadge({ locked }: { locked: boolean }) {
  if (!locked) return null
  return (
    <AxisLabel color="orange" tooltip="This Server is protected. Unlock it before making changes." icon={<Lock size={12} />}>
      Locked
    </AxisLabel>
  )
}

/** Platform-owned membership state; null means no platform currently claims this Server. */
export function MembershipBadge({ axis }: { axis: MembershipAxis | null }) {
  if (!axis) return <UnknownBadge tooltip="No platform reports this Server as a member" />
  const role = axis.role ? ` - ${axis.role}` : ''
  return (
    <AxisLabel color="purple" tooltip={`${axis.nodeName}${role} - ${observedAtLabel(axis.observedAt)}`}>
      {axis.state || 'member'}
    </AxisLabel>
  )
}

/** Metrics-owned liveness state resolved at query time rather than persisted. */
export function HealthBadge({ axis }: { axis: HealthAxis | null }) {
  if (!axis) return <UnknownBadge tooltip="Health has never been observed" />
  return (
    <AxisLabel color={axis.state === 'up' ? 'green' : 'red'} tooltip={observedAtLabel(axis.observedAt)}>
      {axis.state}
    </AxisLabel>
  )
}
