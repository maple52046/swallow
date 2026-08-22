import { Card, Callout, Flex, Grid, Heading } from '@radix-ui/themes'
import { InfoCircledIcon } from '@radix-ui/react-icons'
import {
  DetailSectionView,
  DetailTableCard,
} from '@/presentation/components/serverSummary/DetailViews'
import { findTable } from '@/presentation/components/serverSummary/detailTableUtils'
import {
  DetailsCard,
  GpuCard,
  StatusCard,
  SummaryStatCard,
} from '@/presentation/components/serverSummary/SummaryCards'
import { DeployCard } from './DeployCard'
import { useServerDetailContext } from './useServerDetail'

/** A labelled value line for the Identity card. */
function IdentityField({ label, value }: { label: string; value: string | null }) {
  return (
    <Flex justify="between" gap="4">
      <span style={{ color: 'var(--gray-11)', fontSize: 'var(--font-size-2)' }}>{label}</span>
      <span style={{ fontSize: 'var(--font-size-2)', textAlign: 'right', color: value ? undefined : 'var(--gray-9)' }}>
        {value || 'not observed'}
      </span>
    </Flex>
  )
}

/**
 * The Summary tab: MAAS's OverviewCard grid, plus hardware, NUMA, network, GPUs, deploy,
 * and identity.
 *
 * Status/CPU/Memory/Details/GPUs/Identity come from the server projection; hardware
 * (mainboard/firmware), NUMA, and network come from the live provisioner detail. Storage's
 * disk count is read from the detail Storage table when available. When the provisioner
 * detail could not be read the projection-based cards still render, with a note in place of
 * the live sections.
 */
export function ServerSummaryTab() {
  const { server, detail, detailError, reload } = useServerDetailContext()

  const systemSection = detail?.sections.find((section) => section.title === 'System')
  const numaTable = detail ? findTable(detail.tables, 'NUMA') : undefined
  const networkTable = detail ? findTable(detail.tables, 'Network') : undefined
  const storageTable = detail ? findTable(detail.tables, 'Storage') : undefined
  const diskCount = storageTable?.rows.length ?? 0

  const cpuSub = [server.cpuModel, server.architecture].filter(Boolean).join(' · ')

  return (
    <Flex direction="column" gap="4">
      <Grid columns={{ initial: '1', md: '2' }} gap="4">
        <StatusCard server={server} />
        <DetailsCard server={server} />
      </Grid>

      <Grid columns={{ initial: '1', sm: '3' }} gap="4">
        <SummaryStatCard title="CPU" value={server.cpuCores ? `${server.cpuCores} cores` : 'Unknown'} sub={cpuSub || undefined} />
        <SummaryStatCard title="Memory" value={server.memoryMiB ? `${Math.round(server.memoryMiB / 1024)} GiB` : 'Unknown'} />
        <SummaryStatCard
          title="Storage"
          value={server.storageGB ? `${Math.round(server.storageGB)} GB` : 'Unknown'}
          sub={diskCount > 0 ? `over ${diskCount} ${diskCount === 1 ? 'disk' : 'disks'}` : undefined}
        />
      </Grid>

      {detailError && (
        <Callout.Root color="gray">
          <Callout.Icon>
            <InfoCircledIcon />
          </Callout.Icon>
          <Callout.Text>Live provisioner detail unavailable: {detailError}</Callout.Text>
        </Callout.Root>
      )}

      <Grid columns={{ initial: '1', md: '2' }} gap="4">
        {systemSection && (
          <Card>
            <Heading as="h2" size="3" mb="2">
              Hardware information
            </Heading>
            <DetailSectionView section={systemSection} />
          </Card>
        )}
        {numaTable && <DetailTableCard table={numaTable} />}
        {networkTable && <DetailTableCard table={networkTable} />}
        <GpuCard server={server} />
      </Grid>

      <Grid columns={{ initial: '1', md: '2' }} gap="4">
        <DeployCard server={server} onActed={reload} />
        <Card>
          <Heading as="h2" size="3" mb="2">
            Identity
          </Heading>
          <Flex direction="column" gap="1">
            <IdentityField label="Server ID" value={server.id} />
            <IdentityField label="Provisioner machine ID" value={server.source.providerMachineId} />
            <IdentityField label="System UUID" value={server.hardware.systemUuid} />
            <IdentityField label="Serial number" value={server.hardware.serialNumber} />
            <IdentityField
              label="MAC addresses"
              value={server.hardware.macAddresses.length ? server.hardware.macAddresses.join(', ') : null}
            />
            <IdentityField label="Last seen" value={server.lastSeenAt} />
          </Flex>
        </Card>
      </Grid>
    </Flex>
  )
}
