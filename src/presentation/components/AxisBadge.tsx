import { Badge, Group, Tooltip } from '@mantine/core'
import type { HealthAxis, MembershipAxis, ProvisioningAxis } from '@/domain/server/types'

/**
 * A server's status is three independent axes, so there is one badge per axis and no
 * combined one. A server that is deployed, in no cluster, and not reporting metrics is
 * either a spare awaiting allocation or a broken host, and no rule can tell which — so
 * any single badge would have to pick an interpretation and hide the rest.
 *
 * An axis that has never been observed renders as "unknown", visibly different from any
 * value it could take. "We do not know" must never look like "we know it is bad".
 */

const PROVISIONING_COLOURS: Record<string, string> = {
  deployed: 'green',
  ready: 'blue',
  allocated: 'blue',
  deploying: 'yellow',
  releasing: 'yellow',
  commissioning: 'yellow',
  testing: 'yellow',
  new: 'gray',
  retired: 'gray',
  rescue: 'orange',
  broken: 'red',
  failed: 'red',
  unknown: 'gray',
}

function UnknownBadge({ tooltip }: { tooltip: string }) {
  return (
    <Tooltip label={tooltip}>
      <Badge color="gray" variant="outline" size="sm">
        unknown
      </Badge>
    </Tooltip>
  )
}

function observedAtLabel(observedAt: string): string {
  return `observed ${new Date(observedAt).toLocaleString()}`
}

export function ProvisioningBadge({ axis }: { axis: ProvisioningAxis | null }) {
  if (!axis) return <UnknownBadge tooltip="Provisioning state has never been observed" />

  const state = (
    <Tooltip label={`${axis.providerState} — ${observedAtLabel(axis.observedAt)}`}>
      <Badge color={PROVISIONING_COLOURS[axis.state] ?? 'gray'} variant="light" size="sm">
        {axis.state}
      </Badge>
    </Tooltip>
  )

  if (!axis.ephemeral) return state

  // Alongside the state rather than instead of it: the machine really is deployed, and
  // what needs saying is that nothing on it survives a reboot. Without this marker an
  // ephemeral machine is indistinguishable from one with the same OS on disk.
  return (
    <Group gap={4} wrap="nowrap">
      {state}
      <Tooltip label="Running from memory: the disks are untouched, and anything written to the root filesystem is lost on reboot">
        <Badge color="orange" variant="light" size="sm">
          in memory
        </Badge>
      </Tooltip>
    </Group>
  )
}

export function MembershipBadge({ axis }: { axis: MembershipAxis | null }) {
  // A dash here would read as an empty cell, indistinguishable from a rendering fault,
  // for an axis that is no less unknown than the other two.
  //
  // The wording avoids claiming the axis was never observed, because the API cannot say:
  // a server that left a cluster is cleared back to null and becomes indistinguishable
  // from one no cluster ever reported on. What is true in both cases is that no cluster
  // currently claims it.
  if (!axis) {
    return <UnknownBadge tooltip="No cluster reports this server as a member" />
  }

  const role = axis.role ? ` · ${axis.role}` : ''
  return (
    <Tooltip label={`${axis.nodeName}${role} — ${observedAtLabel(axis.observedAt)}`}>
      <Badge color="grape" variant="light" size="sm">
        {axis.state || 'member'}
      </Badge>
    </Tooltip>
  )
}

export function HealthBadge({ axis }: { axis: HealthAxis | null }) {
  if (!axis) return <UnknownBadge tooltip="Health has never been observed" />

  return (
    <Tooltip label={observedAtLabel(axis.observedAt)}>
      <Badge color={axis.state === 'up' ? 'teal' : 'red'} variant="light" size="sm">
        {axis.state}
      </Badge>
    </Tooltip>
  )
}
