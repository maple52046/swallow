import { Badge, Box, HStack, VisuallyHidden } from '@chakra-ui/react'
import { Lock, MemoryStick } from 'lucide-react'
import type { ReactNode } from 'react'
import type { DeploymentAxis, HealthAxis, MembershipAxis, ProvisioningAxis } from '@/domain/server/types'
import { POWER_PRESENTATION } from './axisBadgeUtils'
import { resolveDeploymentPhase, type AxisColor } from './deploymentPhase'
import { ElapsedTime } from './ElapsedTime'
import { InProgressSpinner } from './InProgressSpinner'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useExperimentalFeature } from '@/presentation/contexts/ExperimentalFeaturesContext'
import { NOT_AVAILABLE_IN_RELEASE } from './releaseAvailability'

const PROVISIONING_COLORS: Record<string, AxisColor> = {
  deployed: 'green', ready: 'blue', allocated: 'blue', deploying: 'orange', releasing: 'orange',
  inspecting: 'orange', testing: 'orange', new: 'gray', retired: 'gray', rescue: 'purple',
  broken: 'red', failed: 'red', unknown: 'gray',
}

/** Icon tint per power state; only running/error are tinted, the rest stay neutral. */
const POWER_TINT: Record<ProvisioningAxis['powerState'], string> = {
  on: 'green.fg',
  off: 'fg.muted',
  error: 'red.fg',
  unknown: 'fg.muted',
}

interface AxisLabelProps {
  color: AxisColor
  tooltip: string
  icon?: ReactNode
  /**
   * Work is still running: a spinner follows the label, matching the OS Images Deploy Mode
   * tags. The label stays the state; the spinner is hidden from assistive technology.
   */
  inProgress?: boolean
  children: ReactNode
}

/** Shared tooltip-wrapped badge for every axis; keeps colour, text, and hover detail consistent. */
function AxisLabel({ color, tooltip, icon, inProgress = false, children }: AxisLabelProps) {
  return (
    <Tooltip content={tooltip}>
      <Badge colorPalette={color} variant="subtle" gap="1">
        {icon}
        {children}
        {inProgress && <InProgressSpinner />}
      </Badge>
    </Tooltip>
  )
}

/**
 * Displays the installed OS/image name as an inventory fact rather than a lifecycle badge.
 * The tooltip keeps deployment provenance available without making the value look like state.
 */
function DeploymentImageValue({ tooltip, children }: { tooltip: string; children: ReactNode }) {
  return (
    <Tooltip content={tooltip}>
      <Box as="span" color="fg" fontSize="sm" fontWeight="medium">{children}</Box>
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
 * The Server's OS deployment state: Swallow's own deployment outcome combined with the
 * swallow-defined OS Provisioning State, resolved by `resolveDeploymentPhase` (decision 048).
 * Generic states such as Releasing, Inspecting, Failed, and Ready are shown by name; none of
 * them is hidden as a provider detail. In-progress states carry a spinner after the label.
 *
 * Two presentations. The default "merged" mode folds the deployed OS image name and,
 * unless `showEphemeral` is false, the ephemeral qualifier into one composed value. Pass
 * `stateOnly` for detail views where deployment state, deployed OS, and ephemeral each get
 * their own field.
 *
 * An in-progress state with a known start also shows its running time (`ElapsedTime`), under
 * the state by default or beside it with `elapsed="inline"` where the badge sits in a row of
 * badges (the Server detail header).
 */
export function DeploymentBadge({
  axis,
  provider,
  stateOnly = false,
  showEphemeral = true,
  elapsed = 'below',
}: {
  axis: DeploymentAxis | null
  provider: ProvisioningAxis | null
  stateOnly?: boolean
  /** False when the composed view presents RAM deployment beside another axis. */
  showEphemeral?: boolean
  elapsed?: 'below' | 'inline'
}) {
  const phase = resolveDeploymentPhase(axis, provider)
  let state: ReactNode
  if (phase.installed && stateOnly && !phase.installed.verified) {
    // State-only mode reports the deployment outcome; an OS Swallow did not deploy has none.
    state = <AxisLabel color="gray" tooltip="The OS is installed, but no Swallow deployment result exists. See the Deployed OS field for the installed image.">Unknown</AxisLabel>
  } else if (phase.installed && !stateOnly && phase.installed.imageName) {
    // Merged mode presents the effective OS image name (provider catalog title overlaid with any
    // Swallow custom name) as neutral inventory text; its tooltip carries verification provenance.
    // Without a derivable name the lifecycle badge below keeps the cell from going blank.
    state = <DeploymentImageValue tooltip={phase.tooltip}>{phase.installed.imageName}</DeploymentImageValue>
  } else {
    state = <AxisLabel color={phase.color} tooltip={phase.tooltip} inProgress={phase.inProgress}>{phase.label}</AxisLabel>
  }

  // In state-only mode the ephemeral qualifier is shown as its own field by the caller, so the
  // badge never appends the icon; the merged list badge keeps it as a compact supplementary cue.
  if (!stateOnly && showEphemeral && provider?.ephemeral) {
    state = (
      <HStack gap="1" flexWrap="nowrap">
        {state}
        <EphemeralIndicator />
      </HStack>
    )
  }
  if (!phase.inProgress || !phase.since) return state
  return (
    <Box
      as="span"
      display="inline-flex"
      flexDirection={elapsed === 'inline' ? 'row' : 'column'}
      alignItems={elapsed === 'inline' ? 'center' : 'flex-start'}
      gap={elapsed === 'inline' ? '2' : '1'}
    >
      {state}
      <ElapsedTime since={phase.since.at} description={phase.since.description} />
    </Box>
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
export function ProvisioningBadge({
  axis,
  showEphemeral = true,
}: {
  axis: ProvisioningAxis | null
  /** False when another badge in the same composed cell already owns the qualifier. */
  showEphemeral?: boolean
}) {
  if (!axis) return <UnknownBadge tooltip="Provisioning state has never been observed" />
  const state = (
    <AxisLabel color={PROVISIONING_COLORS[axis.state] ?? 'gray'} tooltip={`${axis.providerState} - ${observedAtLabel(axis.observedAt)}`}>
      {axis.state}
    </AxisLabel>
  )
  if (!axis.ephemeral || !showEphemeral) return state
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

/**
 * Metrics-owned liveness state resolved at query time rather than persisted.
 *
 * While monitoring is in development (always, in release builds) every Server shows
 * the same neutral "unavailable" label instead of its state: the badge keeps its
 * place in tables, headers, and summary cards, but never suggests a Server is down
 * or unobserved. The reason is part of the accessible text, not only the tooltip.
 */
export function HealthBadge({ axis }: { axis: HealthAxis | null }) {
  const monitoring = useExperimentalFeature('monitoring')
  if (!monitoring) {
    return (
      <AxisLabel color="gray" tooltip={NOT_AVAILABLE_IN_RELEASE}>
        unavailable
        <VisuallyHidden>{`: health is ${NOT_AVAILABLE_IN_RELEASE.toLowerCase()}`}</VisuallyHidden>
      </AxisLabel>
    )
  }
  if (!axis) return <UnknownBadge tooltip="Health has never been observed" />
  return (
    <AxisLabel color={axis.state === 'up' ? 'green' : 'red'} tooltip={observedAtLabel(axis.observedAt)}>
      {axis.state}
    </AxisLabel>
  )
}
