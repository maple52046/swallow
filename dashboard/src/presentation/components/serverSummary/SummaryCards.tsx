import { Card, Heading, HStack, Table, Text } from '@chakra-ui/react'
import { HealthBadge, LockBadge, MembershipBadge, ProvisioningBadge } from '@/presentation/components/AxisBadge'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import type { Server } from '@/domain/server/types'

/** Large tabular-numeric value used across the resource headline cards. */
function ResourceValue({ value, sub }: { value: string; sub?: string }) {
  return (
    <>
      <Text fontSize="2xl" fontWeight="bold" fontVariantNumeric="tabular-nums" lineHeight="1.2">
        {value}
      </Text>
      {sub && (
        <Text fontSize="sm" color="fg.muted">
          {sub}
        </Text>
      )}
    </>
  )
}

/** Compact resource headline card reused for CPU, memory, and storage. */
export function SummaryStatCard({ title, value, sub }: { title: string; value: string; sub?: string }) {
  return (
    <Card.Root size="sm">
      <Card.Body gap="1">
        <Text fontSize="sm" color="fg.muted" fontWeight="medium">
          {title}
        </Text>
        <ResourceValue value={value} sub={sub} />
      </Card.Body>
    </Card.Root>
  )
}

/** Independent machine lifecycle, membership, and liveness axes with provider qualifiers. */
export function StatusCard({ server }: { server: Server }) {
  const axis = server.provisioning
  return (
    <Card.Root>
      <Card.Body gap="4">
        <Heading size="sm">Power and provisioning</Heading>
        <HStack gap="2" wrap="wrap">
          <ProvisioningBadge axis={axis} />
          <LockBadge locked={axis?.locked ?? false} />
          <MembershipBadge axis={server.membership} />
          <HealthBadge axis={server.health} />
        </HStack>
        {axis && (
          <DescriptionList
            items={[
              { label: 'Power', value: axis.powerState },
              {
                label: 'Deployed OS',
                value: axis.distroSeries ? [axis.osSystem, axis.distroSeries].filter(Boolean).join(' ') : null,
              },
              { label: 'Kernel', value: axis.hweKernel },
              { label: 'Commissioning', value: axis.commissioningStatus },
              { label: 'Testing', value: axis.testingStatus },
            ]}
          />
        )}
      </Card.Body>
    </Card.Root>
  )
}

/** Provider placement and inventory labels kept separate from Swallow-owned identity. */
export function DetailsCard({ server }: { server: Server }) {
  return (
    <Card.Root>
      <Card.Body gap="4">
        <Heading size="sm">Provider details</Heading>
        <DescriptionList
          items={[
            { label: 'Zone', value: server.providerZone },
            { label: 'Resource pool', value: server.providerResourcePool },
            { label: 'VM host', value: server.providerPod },
            { label: 'Tags', value: server.tags.length ? server.tags.join(', ') : null },
          ]}
        />
      </Card.Body>
    </Card.Root>
  )
}

/** Hardware GPU inventory; absence is explicit and never confused with zero utilization. */
export function GpuCard({ server }: { server: Server }) {
  return (
    <Card.Root>
      <Card.Body gap="4">
        <Heading size="sm">GPU inventory</Heading>
        {server.gpus.length === 0 ? (
          <Text color="fg.muted">No GPUs reported.</Text>
        ) : (
          <Table.Root size="sm" aria-label="GPU inventory">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Vendor</Table.ColumnHeader>
                <Table.ColumnHeader>Model</Table.ColumnHeader>
                <Table.ColumnHeader>Count</Table.ColumnHeader>
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
      </Card.Body>
    </Card.Root>
  )
}
