import { Card, Heading, SimpleGrid } from '@chakra-ui/react'
import { DetailSectionView, DetailTableCard } from '@/presentation/components/serverSummary/DetailViews'
import { findTable } from '@/presentation/components/serverSummary/detailTableUtils'
import { DetailsCard, GpuCard, StatusCard, SummaryStatCard } from '@/presentation/components/serverSummary/SummaryCards'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import { Alert } from '@/presentation/components/ui/alert'
import { useServerDetailContext } from './useServerDetail'

/** Backend identity and inspection facts, kept separate from provider lifecycle. */
function IdentityCard() {
  const { server } = useServerDetailContext()
  const items = [
    { label: 'Server ID', value: server.id },
    { label: 'Provisioner machine ID', value: server.source.providerMachineId },
    { label: 'System UUID', value: server.hardware.systemUuid },
    { label: 'Serial number', value: server.hardware.serialNumber },
    { label: 'MAC addresses', value: server.hardware.macAddresses.length ? server.hardware.macAddresses.join(', ') : null },
    { label: 'Last seen', value: server.lastSeenAt },
  ]
  return (
    <Card.Root>
      <Card.Body gap="4">
        <Heading size="sm">Identity and inspection</Heading>
        <DescriptionList items={items} />
      </Card.Body>
    </Card.Root>
  )
}

/**
 * Cockpit-style Summary scan: power/provisioning first, then resources, live hardware,
 * provider details, and identity. Projection sections remain available when live provisioner
 * detail fails, making that failure partial rather than page-wide.
 */
export function ServerSummaryTab() {
  const { server, detail, detailError } = useServerDetailContext()
  const system = detail?.sections.find((section) => section.title === 'System')
  const numa = detail ? findTable(detail.tables, 'NUMA') : undefined
  const network = detail ? findTable(detail.tables, 'Network') : undefined
  const storage = detail ? findTable(detail.tables, 'Storage') : undefined
  const cpuSub = [server.cpuModel, server.architecture].filter(Boolean).join(' - ')
  return (
    <div className="sw-server-summary">
      <SimpleGrid minChildWidth="320px" gap="4">
        <StatusCard server={server} />
        <DetailsCard server={server} />
      </SimpleGrid>
      <SimpleGrid minChildWidth="220px" gap="4">
        <SummaryStatCard title="CPU" value={server.cpuCores ? `${server.cpuCores} cores` : 'Unknown'} sub={cpuSub || undefined} />
        <SummaryStatCard title="Memory" value={server.memoryMiB ? `${Math.round(server.memoryMiB / 1024)} GiB` : 'Unknown'} />
        <SummaryStatCard title="Storage" value={server.storageGB ? `${Math.round(server.storageGB)} GB` : 'Unknown'} sub={storage?.rows.length ? `${storage.rows.length} devices` : undefined} />
      </SimpleGrid>
      {detailError && (
        <Alert status="warning" title="Live hardware detail unavailable">
          {detailError}
        </Alert>
      )}
      <SimpleGrid minChildWidth="340px" gap="4">
        {system && (
          <Card.Root>
            <Card.Body gap="4">
              <Heading size="sm">Hardware inventory</Heading>
              <DetailSectionView section={system} />
            </Card.Body>
          </Card.Root>
        )}
        {numa && <DetailTableCard table={numa} />}
        {network && <DetailTableCard table={network} />}
        <GpuCard server={server} />
      </SimpleGrid>
      <IdentityCard />
    </div>
  )
}
