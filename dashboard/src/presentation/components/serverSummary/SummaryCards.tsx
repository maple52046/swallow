import { Badge, Card, Flex, Heading, Table, Text } from '@radix-ui/themes'
import { ProvisioningBadge, HealthBadge, MembershipBadge } from '@/presentation/components/AxisBadge'
import type { Server } from '@/domain/server/types'

/**
 * The shared server-summary card set, modelled on MAAS's OverviewCard composite but on
 * Radix and the gdcm projection. Each card renders one facet of a `Server`; the Summary
 * tab composes them into the MAAS grid. Kept as one cohesive set so a field added to a
 * facet lands in one place.
 */

/** A labelled value line used inside the cards; muted when the value is absent. */
function Field({ label, value }: { label: string; value: string | null | undefined }) {
  return (
    <Flex justify="between" gap="4">
      <Text size="2" color="gray">
        {label}
      </Text>
      <Text size="2" color={value ? undefined : 'gray'} style={{ textAlign: 'right' }}>
        {value || 'not observed'}
      </Text>
    </Flex>
  )
}

/**
 * A single headline resource card (CPU / Memory / Storage): a big value with an optional
 * sub-line. Reused for the three resource facets so they stay visually identical.
 */
export function SummaryStatCard({
  title,
  value,
  sub,
}: {
  title: string
  value: string
  sub?: string
}) {
  return (
    <Card>
      <Text size="1" color="gray">
        {title}
      </Text>
      <Text as="div" size="6" weight="bold">
        {value}
      </Text>
      {sub && (
        <Text size="1" color="gray">
          {sub}
        </Text>
      )}
    </Card>
  )
}

/**
 * Machine status: the three axes plus the provisioner facts that qualify them
 * (ephemerality, lock, commissioning/testing outcomes). Status is shown by badges with
 * text, never colour alone.
 */
export function StatusCard({ server }: { server: Server }) {
  const provisioning = server.provisioning
  return (
    <Card>
      <Heading as="h2" size="3" mb="2">
        Machine status
      </Heading>
      <Flex direction="column" gap="2">
        <Flex align="center" gap="2" wrap="wrap">
          <ProvisioningBadge axis={provisioning} />
          <MembershipBadge axis={server.membership} />
          <HealthBadge axis={server.health} />
        </Flex>
        {provisioning && (
          <Flex direction="column" gap="1" mt="1">
            <Field
              label="Deployed OS"
              value={
                provisioning.distroSeries
                  ? [provisioning.osSystem, provisioning.distroSeries].filter(Boolean).join(' ')
                  : null
              }
            />
            <Field label="Kernel" value={provisioning.hweKernel || null} />
            <Field label="Commissioning" value={provisioning.commissioningStatus || null} />
            <Field label="Testing" value={provisioning.testingStatus || null} />
            {provisioning.locked && (
              <Flex justify="between">
                <Text size="2" color="gray">
                  Locked
                </Text>
                <Badge color="amber">locked</Badge>
              </Flex>
            )}
          </Flex>
        )}
      </Flex>
    </Card>
  )
}

/** Provisioner grouping facts: zone, resource pool, VM host, and tags. */
export function DetailsCard({ server }: { server: Server }) {
  return (
    <Card>
      <Heading as="h2" size="3" mb="2">
        Details
      </Heading>
      <Flex direction="column" gap="1">
        <Field label="Zone" value={server.providerZone || null} />
        <Field label="Resource pool" value={server.providerResourcePool || null} />
        <Field label="VM host" value={server.providerPod || null} />
        <Field label="Tags" value={server.tags.length ? server.tags.join(', ') : null} />
      </Flex>
    </Card>
  )
}

/** The machine's GPU inventory, the reason this platform exists; empty state is explicit. */
export function GpuCard({ server }: { server: Server }) {
  return (
    <Card>
      <Heading as="h2" size="3" mb="2">
        GPUs
      </Heading>
      {server.gpus.length === 0 ? (
        <Text size="2" color="gray">
          No GPUs reported.
        </Text>
      ) : (
        <Table.Root variant="surface">
          <Table.Header>
            <Table.Row>
              <Table.ColumnHeaderCell>Vendor</Table.ColumnHeaderCell>
              <Table.ColumnHeaderCell>Model</Table.ColumnHeaderCell>
              <Table.ColumnHeaderCell>Count</Table.ColumnHeaderCell>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {server.gpus.map((gpu, index) => (
              <Table.Row key={`${gpu.vendor}-${gpu.model}-${index}`}>
                <Table.Cell>{gpu.vendor}</Table.Cell>
                <Table.Cell>{gpu.model}</Table.Cell>
                <Table.Cell>{gpu.count}</Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table.Root>
      )}
    </Card>
  )
}
