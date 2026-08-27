import { Flex, Label, Tooltip } from '@patternfly/react-core'
import type { HealthAxis, MembershipAxis, ProvisioningAxis } from '@/domain/server/types'

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

/** Provisioner-owned lifecycle state, including the non-persistent in-memory qualifier. */
export function ProvisioningBadge({ axis }: { axis: ProvisioningAxis | null }) {
  if (!axis) return <UnknownBadge tooltip="Provisioning state has never been observed" />
  const state = (
    <Tooltip content={`${axis.providerState} - ${observedAtLabel(axis.observedAt)}`}>
      <Label color={PROVISIONING_COLORS[axis.state] ?? 'grey'}>{axis.state}</Label>
    </Tooltip>
  )
  if (!axis.ephemeral) return state
  return (
    <Flex gap={{ default: 'gapXs' }} flexWrap={{ default: 'nowrap' }}>
      {state}
      <Tooltip content="Running from memory; root filesystem changes are lost on reboot">
        <Label color="orange">in memory</Label>
      </Tooltip>
    </Flex>
  )
}

/** Cluster-owned membership state; null means no cluster currently claims this Server. */
export function MembershipBadge({ axis }: { axis: MembershipAxis | null }) {
  if (!axis) return <UnknownBadge tooltip="No cluster reports this Server as a member" />
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
