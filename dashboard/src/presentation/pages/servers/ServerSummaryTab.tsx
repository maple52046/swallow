import { useState } from 'react'
import type { Server } from '@/domain/server/types'
import { Alert } from '@/presentation/components/ui/alert'
import {
  CapacityCard,
  DetailsCard,
  HardwareProfileCard,
  ManagementControllerCard,
  StatusCard,
} from '@/presentation/components/serverSummary/SummaryCards'
import { findTable } from '@/presentation/components/serverSummary/detailTableUtils'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { ServerTagEditor } from './ServerTagEditor'
import { useServerDetailContext } from './useServerDetail'

/**
 * Builds a catalog deep link only for an OS deployment Swallow completed successfully. The
 * effective image name is paired with its integration and primary architecture; OS Images
 * applies those identities before falling back to the observed OS/release tuple.
 */
function deployedImageCatalogHref(server: Server, scopedHref: (path: string) => string): string | undefined {
  const axis = server.provisioning
  if (
    server.deployment?.state !== 'succeeded' ||
    axis?.state !== 'deployed' ||
    !axis.deployedImageName ||
    !axis.integrationId
  ) {
    return undefined
  }

  const target = new URL(scopedHref('/provisioning/images'), window.location.origin)
  target.searchParams.set('integrationId', axis.integrationId)
  target.searchParams.set('imageName', axis.deployedImageName)
  if (server.architecture) target.searchParams.set('architecture', server.architecture)
  if (axis.osSystem) target.searchParams.set('osSystem', axis.osSystem)
  if (axis.distroSeries) target.searchParams.set('release', axis.distroSeries)
  return `${target.pathname}${target.search}`
}

/**
 * Operator-first overview for one Server. Projection-backed state and capacity remain useful
 * when live provider detail fails; dedicated tabs own large Network, Storage, and PCI tables so
 * the overview stays a fast scan rather than a second inventory page.
 */
export function ServerSummaryTab() {
  const { server, detail, detailError, reload } = useServerDetailContext()
  const { scopedHref } = useSiteScope()
  const [tagEditorOpen, setTagEditorOpen] = useState(false)
  const system = detail?.sections.find((section) => section.title === 'System')
  const management = detail?.sections.find((section) => section.title === 'BMC')
  const storage = detail ? findTable(detail.tables, 'Storage') : undefined
  const deployedImageHref = deployedImageCatalogHref(server, scopedHref)
  const physical = !server.providerPod
  return (
    <div className="sw-server-summary">
      <div className="sw-server-summary-grid sw-server-summary-grid--lead">
        <StatusCard server={server} deployedImageHref={deployedImageHref} />
        <CapacityCard server={server} storageDeviceCount={storage?.rows.length} />
      </div>
      {detailError && (
        <Alert status="warning" title="Live hardware detail unavailable">
          {detailError}
        </Alert>
      )}
      <div className="sw-server-summary-grid sw-server-summary-grid--expand-single">
        {physical && <ManagementControllerCard management={management} />}
        <HardwareProfileCard server={server} system={system} />
      </div>
      <div className="sw-server-summary-grid sw-server-summary-grid--expand-single">
        <DetailsCard server={server} onEditTags={() => setTagEditorOpen(true)} />
      </div>
      {tagEditorOpen && (
        <ServerTagEditor
          servers={[server]}
          onClose={() => setTagEditorOpen(false)}
          onSaved={reload}
        />
      )}
    </div>
  )
}
