import { Card, CardBody, CardTitle, DescriptionList, DescriptionListDescription, DescriptionListGroup, DescriptionListTerm, Flex, Label } from '@patternfly/react-core'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { ProvisioningBadge, HealthBadge, MembershipBadge } from '@/presentation/components/AxisBadge'
import type { Server } from '@/domain/server/types'

function Fields({ items }: { items: Array<{ label: string; value: string | null | undefined }> }) {
  return <DescriptionList isHorizontal isCompact>{items.map((item) => <DescriptionListGroup key={item.label}><DescriptionListTerm>{item.label}</DescriptionListTerm><DescriptionListDescription>{item.value || 'Not observed'}</DescriptionListDescription></DescriptionListGroup>)}</DescriptionList>
}

/** Compact resource headline card reused for CPU, memory, and storage. */
export function SummaryStatCard({ title, value, sub }: { title: string; value: string; sub?: string }) {
  return <Card isCompact><CardTitle>{title}</CardTitle><CardBody><strong className="sw-resource-value">{value}</strong>{sub && <small>{sub}</small>}</CardBody></Card>
}

/** Independent machine lifecycle, membership, and liveness axes with provider qualifiers. */
export function StatusCard({ server }: { server: Server }) {
  const axis = server.provisioning
  return <Card><CardTitle>Power and provisioning</CardTitle><CardBody><Flex gap={{ default: 'gapSm' }} flexWrap={{ default: 'wrap' }}><ProvisioningBadge axis={axis} /><MembershipBadge axis={server.membership} /><HealthBadge axis={server.health} />{axis?.locked && <Label color="orange">locked</Label>}</Flex>{axis && <Fields items={[{ label: 'Power', value: axis.powerState }, { label: 'Deployed OS', value: axis.distroSeries ? [axis.osSystem, axis.distroSeries].filter(Boolean).join(' ') : null }, { label: 'Kernel', value: axis.hweKernel }, { label: 'Commissioning', value: axis.commissioningStatus }, { label: 'Testing', value: axis.testingStatus }]} />}</CardBody></Card>
}

/** Provider placement and inventory labels kept separate from Swallow-owned identity. */
export function DetailsCard({ server }: { server: Server }) {
  return <Card><CardTitle>Provider details</CardTitle><CardBody><Fields items={[{ label: 'Zone', value: server.providerZone }, { label: 'Resource pool', value: server.providerResourcePool }, { label: 'VM host', value: server.providerPod }, { label: 'Tags', value: server.tags.length ? server.tags.join(', ') : null }]} /></CardBody></Card>
}

/** Hardware GPU inventory; absence is explicit and never confused with zero utilization. */
export function GpuCard({ server }: { server: Server }) {
  return <Card><CardTitle>GPU inventory</CardTitle><CardBody>{server.gpus.length === 0 ? 'No GPUs reported.' : <Table aria-label="GPU inventory" variant="compact"><Thead><Tr><Th>Vendor</Th><Th>Model</Th><Th>Count</Th></Tr></Thead><Tbody>{server.gpus.map((gpu, index) => <Tr key={`${gpu.vendor}-${gpu.model}-${index}`}><Td dataLabel="Vendor">{gpu.vendor}</Td><Td dataLabel="Model">{gpu.model}</Td><Td dataLabel="Count">{gpu.count}</Td></Tr>)}</Tbody></Table>}</CardBody></Card>
}
