import { Flex, Label, Tooltip } from '@patternfly/react-core'
import { Lock, MemoryStick } from 'lucide-react'
import type { DeploymentAxis, HealthAxis, MembershipAxis, ProvisioningAxis } from '@/domain/server/types'

const PROVISIONING_COLORS: Record<string, 'green' | 'blue' | 'orange' | 'grey' | 'red' | 'purple'> = {
  deployed: 'green', ready: 'blue', allocated: 'blue', deploying: 'orange', releasing: 'orange',
  commissioning: 'orange', testing: 'orange', new: 'grey', retired: 'grey', rescue: 'purple',
  broken: 'red', failed: 'red', unknown: 'grey',
}

function UnknownBadge({ tooltip }: { tooltip: string }) {
  return <Tooltip content={tooltip}><Label color="grey">unknown</Label></Tooltip>
}

function observedAtLabel(observedAt: string): string {
  return `Observed ${new Date(observedAt).toLocaleString()}`
}

/**
 * Marks an ephemeral (run-from-RAM) deployment with a memory-stick glyph shown beside the
 * state badge. The meaning is carried by the tooltip and `aria-label`, not colour alone, so it
 * stays accessible; the warning tint is only a supplementary cue.
 */
function EphemeralIndicator() {
  return (
    <Tooltip content="Ephemeral deployment; root filesystem changes are lost on reboot">
      <span className="sw-ephemeral-indicator" role="img" aria-label="Ephemeral deployment">
        <MemoryStick />
      </span>
    </Tooltip>
  )
}

const DEPLOYMENT_PRESENTATION = {
  deploying: { color: 'blue', label: 'Deploying' },
  verifying: { color: 'blue', label: 'Verifying' },
  succeeded: { color: 'green', label: 'Deployed' },
  failed: { color: 'red', label: 'Failed' },
  requires_attention: { color: 'orange', label: 'Attention' },
  canceled: { color: 'grey', label: 'Canceled' },
} as const

/** Swallow-owned deployment outcome; this is the primary deployability result. */
export function DeploymentBadge({
  axis,
  provider,
}: {
  axis: DeploymentAxis | null
  provider: ProvisioningAxis | null
}) {
  let state
  if (provider?.state === 'releasing') {
    state = <Tooltip content="The provider is returning this Server to its available pool."><Label color="orange">Releasing</Label></Tooltip>
  } else if (provider?.state === 'commissioning') {
    state = <Tooltip content="The provider is commissioning this Server."><Label color="blue">Commissioning</Label></Tooltip>
  } else if (provider?.state === 'testing') {
    state = <Tooltip content="The provider is testing this Server."><Label color="blue">Testing</Label></Tooltip>
  } else if (axis) {
    const presentation = DEPLOYMENT_PRESENTATION[axis.state]
    const detail = axis.statusReason ||
      (axis.state === 'succeeded'
        ? 'Swallow verified the installed image, provider address, and SSH endpoint.'
        : `Operation ${axis.operationId}, attempt ${axis.attempt}`)
    state = <Tooltip content={detail}><Label color={presentation.color}>{presentation.label}</Label></Tooltip>
  } else if (provider?.state === 'deploying') {
    state = <Tooltip content="The provider is installing an operating system; no verified Swallow result exists yet."><Label color="blue">Deploying</Label></Tooltip>
  } else if (provider?.state === 'deployed') {
    state = <Tooltip content="The OS is installed, but no Swallow deployment result exists. Check the OS and Network fields for the facts that are known."><Label color="grey">Unknown</Label></Tooltip>
  } else if (provider?.state === 'failed' || provider?.state === 'broken') {
    state = <Tooltip content={`Provider lifecycle: ${provider.providerState}`}><Label color="red">Failed</Label></Tooltip>
  } else if (provider?.state === 'ready') {
    // A released machine is back in the provider's available pool. Show it as "Ready"
    // (the provider's own term) rather than "Not deployed", which reads like a fault.
    state = <Tooltip content="The Server is in the provider's available pool, ready to be deployed."><Label color="blue">Ready</Label></Tooltip>
  } else {
    state = <Tooltip content="No operating system deployment is active or verified."><Label color="grey">Not deployed</Label></Tooltip>
  }

  if (!provider?.ephemeral) return state
  return (
    <Flex gap={{ default: 'gapXs' }} alignItems={{ default: 'alignItemsCenter' }} flexWrap={{ default: 'nowrap' }}>
      {state}
      <EphemeralIndicator />
    </Flex>
  )
}

/** Provider-owned OS installation fact, deliberately independent from deployment success. */
export function OperatingSystemBadge({ axis }: { axis: ProvisioningAxis | null }) {
  if (!axis) {
    return <UnknownBadge tooltip="The provisioner has not reported an operating system state." />
  }
  if (axis.state === 'deployed') {
    const image = [axis.osSystem, axis.distroSeries].filter(Boolean).join(' ')
    return <Tooltip content={image || 'The provisioner reports an installed operating system.'}><Label color="blue">Installed</Label></Tooltip>
  }
  if (axis.state === 'deploying') {
    return <Tooltip content={`Provider lifecycle: ${axis.providerState}`}><Label color="blue">Installing</Label></Tooltip>
  }
  if (axis.state === 'releasing') {
    return <Tooltip content={`Provider lifecycle: ${axis.providerState}`}><Label color="orange">Removing</Label></Tooltip>
  }
  if (['new', 'ready', 'allocated', 'retired'].includes(axis.state)) {
    return <Tooltip content={`Provider lifecycle: ${axis.providerState}`}><Label color="grey">Not installed</Label></Tooltip>
  }
  return <UnknownBadge tooltip={`Provider lifecycle: ${axis.providerState}`} />
}

/** Reachability proven by the latest Swallow OS deployment, not physical NIC link state. */
export function DeploymentNetworkBadge({ axis }: { axis: DeploymentAxis | null }) {
  if (!axis) {
    return <UnknownBadge tooltip="No Swallow deployment has recorded a network reachability result." />
  }
  if (axis.state === 'succeeded') {
    return <Tooltip content="Swallow observed an address and reached the configured SSH endpoint."><Label color="green">Reachable</Label></Tooltip>
  }
  if (axis.state === 'verifying') {
    return <Tooltip content="Swallow is checking the observed address and SSH endpoint."><Label color="blue">Checking</Label></Tooltip>
  }
  if (axis.state === 'deploying') {
    return <Tooltip content="Network reachability is checked after OS installation."><Label color="grey">Pending</Label></Tooltip>
  }
  if (axis.state === 'failed' && axis.stage === 'ssh_readiness') {
    return <Tooltip content={axis.statusReason || 'The deployment address or SSH endpoint was not reachable.'}><Label color="red">Unreachable</Label></Tooltip>
  }
  if (axis.state === 'failed' && axis.stage.includes('network')) {
    return <Tooltip content={axis.statusReason || 'Network configuration failed.'}><Label color="red">Configuration failed</Label></Tooltip>
  }
  if (axis.state === 'requires_attention' && axis.stage === 'ssh_readiness') {
    return <Tooltip content={axis.statusReason || 'Swallow could not complete network verification.'}><Label color="orange">Unknown</Label></Tooltip>
  }
  return <Tooltip content="This deployment did not complete network reachability verification."><Label color="grey">Not checked</Label></Tooltip>
}

/** Provisioner-owned lifecycle state, including its Ephemeral deployment qualifier. */
export function ProvisioningBadge({ axis }: { axis: ProvisioningAxis | null }) {
  if (!axis) return <UnknownBadge tooltip="Provisioning state has never been observed" />
  const state = (
    <Tooltip content={`${axis.providerState} - ${observedAtLabel(axis.observedAt)}`}>
      <Label color={PROVISIONING_COLORS[axis.state] ?? 'grey'}>{axis.state}</Label>
    </Tooltip>
  )
  if (!axis.ephemeral) return state
  return (
    <Flex gap={{ default: 'gapXs' }} alignItems={{ default: 'alignItemsCenter' }} flexWrap={{ default: 'nowrap' }}>
      {state}
      <EphemeralIndicator />
    </Flex>
  )
}

/** Provider-owned mutation protection, deliberately separate from lifecycle state. */
export function LockBadge({ locked }: { locked: boolean }) {
  if (!locked) return null
  return (
    <Tooltip content="This Server is protected. Unlock it before making changes.">
      <Label color="orange" icon={<Lock />}>Locked</Label>
    </Tooltip>
  )
}

/** Platform-owned membership state; null means no platform currently claims this Server. */
export function MembershipBadge({ axis }: { axis: MembershipAxis | null }) {
  if (!axis) return <UnknownBadge tooltip="No platform reports this Server as a member" />
  const role = axis.role ? ` - ${axis.role}` : ''
  return (
    <Tooltip content={`${axis.nodeName}${role} - ${observedAtLabel(axis.observedAt)}`}>
      <Label color="purple">{axis.state || 'member'}</Label>
    </Tooltip>
  )
}

/** Metrics-owned liveness state resolved at query time rather than persisted. */
export function HealthBadge({ axis }: { axis: HealthAxis | null }) {
  if (!axis) return <UnknownBadge tooltip="Health has never been observed" />
  return (
    <Tooltip content={observedAtLabel(axis.observedAt)}>
      <Label color={axis.state === 'up' ? 'green' : 'red'}>{axis.state}</Label>
    </Tooltip>
  )
}
