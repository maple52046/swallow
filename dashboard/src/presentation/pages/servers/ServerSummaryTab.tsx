import { Alert, AlertVariant, Card, CardBody, CardTitle, DescriptionList, DescriptionListDescription, DescriptionListGroup, DescriptionListTerm, Gallery } from '@patternfly/react-core'
import { DetailSectionView, DetailTableCard } from '@/presentation/components/serverSummary/DetailViews'
import { findTable } from '@/presentation/components/serverSummary/detailTableUtils'
import { DetailsCard, GpuCard, StatusCard, SummaryStatCard } from '@/presentation/components/serverSummary/SummaryCards'
import { DeployCard } from './DeployCard'
import { useServerDetailContext } from './useServerDetail'

function IdentityCard() {
  const { server } = useServerDetailContext()
  const items = [{ label: 'Server ID', value: server.id }, { label: 'Provisioner machine ID', value: server.source.providerMachineId }, { label: 'System UUID', value: server.hardware.systemUuid }, { label: 'Serial number', value: server.hardware.serialNumber }, { label: 'MAC addresses', value: server.hardware.macAddresses.length ? server.hardware.macAddresses.join(', ') : null }, { label: 'Last seen', value: server.lastSeenAt }]
  return <Card><CardTitle>Identity and inspection</CardTitle><CardBody><DescriptionList isHorizontal isCompact>{items.map((item) => <DescriptionListGroup key={item.label}><DescriptionListTerm>{item.label}</DescriptionListTerm><DescriptionListDescription>{item.value || 'Not observed'}</DescriptionListDescription></DescriptionListGroup>)}</DescriptionList></CardBody></Card>
}

/**
 * Cockpit-style Summary scan: power/provisioning first, then resources, live hardware,
 * provider details, deployment, and identity. Projection sections remain available when
 * live provisioner detail fails, making that failure partial rather than page-wide.
 */
export function ServerSummaryTab() {
  const { server, detail, detailError, reload } = useServerDetailContext()
  const system = detail?.sections.find((section) => section.title === 'System')
  const numa = detail ? findTable(detail.tables, 'NUMA') : undefined
  const network = detail ? findTable(detail.tables, 'Network') : undefined
  const storage = detail ? findTable(detail.tables, 'Storage') : undefined
  const cpuSub = [server.cpuModel, server.architecture].filter(Boolean).join(' - ')
  return <div className="sw-server-summary">
    <Gallery hasGutter minWidths={{ default: '320px' }}><StatusCard server={server} /><DetailsCard server={server} /></Gallery>
    <Gallery hasGutter minWidths={{ default: '220px' }}><SummaryStatCard title="CPU" value={server.cpuCores ? `${server.cpuCores} cores` : 'Unknown'} sub={cpuSub || undefined} /><SummaryStatCard title="Memory" value={server.memoryMiB ? `${Math.round(server.memoryMiB / 1024)} GiB` : 'Unknown'} /><SummaryStatCard title="Storage" value={server.storageGB ? `${Math.round(server.storageGB)} GB` : 'Unknown'} sub={storage?.rows.length ? `${storage.rows.length} devices` : undefined} /></Gallery>
    {detailError && <Alert variant={AlertVariant.warning} title="Live hardware detail unavailable" isInline>{detailError}</Alert>}
    <Gallery hasGutter minWidths={{ default: '340px' }}>{system && <Card><CardTitle>Hardware inventory</CardTitle><CardBody><DetailSectionView section={system} /></CardBody></Card>}{numa && <DetailTableCard table={numa} />}{network && <DetailTableCard table={network} />}<GpuCard server={server} /></Gallery>
    <Gallery hasGutter minWidths={{ default: '340px' }}><DeployCard server={server} onActed={reload} /><IdentityCard /></Gallery>
  </div>
}
