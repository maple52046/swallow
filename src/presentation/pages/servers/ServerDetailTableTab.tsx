import { Card, Heading } from '@radix-ui/themes'
import {
  DetailTableView,
  DetailUnavailable,
} from '@/presentation/components/serverSummary/DetailViews'
import { findTable } from '@/presentation/components/serverSummary/detailTableUtils'
import { useServerDetailContext } from './useServerDetail'

interface ServerDetailTableTabProps {
  /** Table title to pull from the live provisioner detail (e.g. "Network", "Storage"). */
  title: string
}

/**
 * A detail tab that renders one live provisioner-detail table in full.
 *
 * Shared by the Network, Storage, and PCI devices tabs — one implementation so every tab
 * behaves identically. When the provisioner detail could not be read it shows the
 * unavailable state with the reason; when the table is simply empty, the table view says
 * so.
 */
export function ServerDetailTableTab({ title }: ServerDetailTableTabProps) {
  const { detail, detailError } = useServerDetailContext()

  if (!detail) {
    return <DetailUnavailable message={detailError ?? 'The provisioner did not return detail.'} />
  }

  const table = findTable(detail.tables, title)
  if (!table) {
    return <DetailUnavailable message={`This provisioner reported no ${title.toLowerCase()} detail.`} />
  }

  return (
    <Card>
      <Heading as="h2" size="3" mb="2">
        {table.title}
      </Heading>
      <DetailTableView table={table} />
    </Card>
  )
}

/** Network tab: the live network interfaces table. */
export function ServerNetworkTab() {
  return <ServerDetailTableTab title="Network" />
}

/** Storage tab: the live block-device table. */
export function ServerStorageTab() {
  return <ServerDetailTableTab title="Storage" />
}

/** PCI devices tab: the live PCI device map. */
export function ServerPciTab() {
  return <ServerDetailTableTab title="PCI devices" />
}
