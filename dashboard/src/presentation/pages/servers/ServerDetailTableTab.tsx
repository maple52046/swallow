import { Card, CardBody, CardTitle } from '@patternfly/react-core'
import { DetailTableView, DetailUnavailable } from '@/presentation/components/serverSummary/DetailViews'
import { findTable } from '@/presentation/components/serverSummary/detailTableUtils'
import { useServerDetailContext } from './useServerDetail'

/** Shared provider-detail table tab used by Network, Storage, and PCI routes. */
export function ServerDetailTableTab({ title }: { title: string }) {
  const { detail, detailError } = useServerDetailContext()
  if (!detail) return <DetailUnavailable message={detailError ?? 'The provisioner did not return detail.'} />
  const table = findTable(detail.tables, title)
  if (!table) return <DetailUnavailable message={`This provisioner reported no ${title.toLowerCase()} detail.`} />
  return <Card><CardTitle>{table.title}</CardTitle><CardBody><DetailTableView table={table} /></CardBody></Card>
}

/** Live network interfaces table. */
export function ServerNetworkTab() { return <ServerDetailTableTab title="Network" /> }
/** Live block-device table. */
export function ServerStorageTab() { return <ServerDetailTableTab title="Storage" /> }
/** Live PCI hardware map. */
export function ServerPciTab() { return <ServerDetailTableTab title="PCI devices" /> }
