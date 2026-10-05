import { useState } from 'react'
import { Button } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { BootMediaLiveState, Server, ServerBootMedia } from '@/domain/server/types'
import { Alert } from '@/presentation/components/ui/alert'
import { BootMediaPanel, type BootMediaPanelState } from '@/presentation/components/serverSummary/BootMediaPanel'
import { redfishSupportLabel } from '@/presentation/components/serverSummary/bootMediaLabels'
import {
  CapacityCard,
  ConnectionCard,
  DetailsCard,
  HardwareProfileCard,
  ManagementControllerCard,
  StatusCard,
} from '@/presentation/components/serverSummary/SummaryCards'
import { findTable } from '@/presentation/components/serverSummary/detailTableUtils'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useAsyncData, type AsyncData } from '@/presentation/hooks/useAsyncData'
import { ServerBootMediaDialog, type ServerBootMediaDialogMode } from './ServerBootMediaDialog'
import { ServerDefaultUserDialog } from './ServerDefaultUserDialog'
import { ServerTagEditor } from './ServerTagEditor'
import { useBootMediaApplyWatch } from './useBootMediaApplyWatch'
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
 * Maps the Boot Media read onto the panel's state, with `latest` (the newest poll while a
 * preflight runs) in place of the page's read. The loader yields `null` only for a virtual
 * machine, whose management card is not rendered, so that case never reaches the screen.
 */
function bootMediaPanelState(state: AsyncData<ServerBootMedia | null>, latest: ServerBootMedia | null): BootMediaPanelState {
  if (state.status !== 'ready') return state
  return latest ? { status: 'ready', data: latest } : { status: 'loading' }
}

/**
 * Operator-first overview for one Server. Projection-backed state and capacity remain useful
 * when live provider detail fails; dedicated tabs own large Network, Storage, and PCI tables so
 * the overview stays a fast scan rather than a second inventory page. In-band SSH connection
 * info sits beside the out-of-band management controller, so both ways in read as one row; its
 * login user is the Server Default User, set or changed here through the Default user dialog, and
 * the Server is re-read after a change so the card and SSH command follow it.
 *
 * A physical Server's management controller card also carries its Boot Media (decisions 047 and
 * 049): the stored setting, its Boot ISO, and the Redfish probe are read when the page opens; the
 * BMC itself is read only when the operator asks ("Check BMC"), and re-probing or changing the
 * setting (enable, change ISO, re-apply, disable) re-reads the block. While an enable preflight
 * runs — sent from here, from another tab, or before a reload — the block is re-read every two
 * seconds for its progress and its actions wait until it ends. A virtual machine has no BMC, so
 * it reads nothing.
 */
export function ServerSummaryTab() {
  const { server, detail, detailError, reload } = useServerDetailContext()
  const { servers } = useApp()
  const { showToast } = useToast()
  const { scopedHref } = useSiteScope()
  const [tagEditorOpen, setTagEditorOpen] = useState(false)
  const [defaultUserOpen, setDefaultUserOpen] = useState(false)
  const [bootMediaDialog, setBootMediaDialog] = useState<ServerBootMediaDialogMode | null>(null)
  const [bootMediaBusy, setBootMediaBusy] = useState<'probe' | 'live' | null>(null)
  const [live, setLive] = useState<{ state: BootMediaLiveState | null; error?: string } | undefined>(undefined)
  const system = detail?.sections.find((section) => section.title === 'System')
  const management = detail?.sections.find((section) => section.title === 'BMC')
  const storage = detail ? findTable(detail.tables, 'Storage') : undefined
  const deployedImageHref = deployedImageCatalogHref(server, scopedHref)
  const physical = !server.providerPod
  const bootMedia = useAsyncData(
    async () => (physical ? servers.getBootMedia(server.id) : null),
    [servers, server.id, physical],
  )

  const probe = async () => {
    setBootMediaBusy('probe')
    try {
      const capability = await servers.probeRedfish(server.id)
      showToast({ tone: capability.support === 'supported' ? 'success' : 'warning', title: redfishSupportLabel(capability.support), description: capability.reason })
      bootMedia.reload()
    } catch (caught) {
      showToast({ tone: 'error', title: 'Redfish probe failed', description: caught instanceof Error ? caught.message : undefined })
    } finally {
      setBootMediaBusy(null)
    }
  }

  // A live read reaches the BMC and takes seconds, so it runs only on request and its answer is
  // kept beside the stored facts until the next read or setting change.
  const checkLive = async () => {
    setBootMediaBusy('live')
    try {
      const result = await servers.getBootMedia(server.id, { live: true })
      setLive({ state: result.live, error: result.liveError })
    } catch (caught) {
      setLive({ state: null, error: caught instanceof Error ? caught.message : 'The BMC could not be read.' })
    } finally {
      setBootMediaBusy(null)
    }
  }

  const applyWatch = useBootMediaApplyWatch(servers, server.id, bootMedia.status === 'ready' ? bootMedia.data : null)
  const bootMediaData = applyWatch.media
  const enabled = bootMediaData?.setting?.enabled ?? false
  // The Boot ISO is chosen in the dialog, so only a Server without a BMC cannot start one. While a
  // preflight runs every BMC action waits: the API refuses a second write, and a probe or live
  // read would compete with the preflight's own requests on a slow BMC.
  const applying = applyWatch.applying
  const canEnable = bootMediaData?.redfish?.support !== 'no_bmc'
  const bootMediaActions = bootMediaData && (
    <>
      {enabled ? (
        <>
          <Button size="xs" variant="outline" disabled={applying} onClick={() => setBootMediaDialog('disable')}>
            Disable
          </Button>
          <Button size="xs" variant="outline" colorPalette="brand" disabled={applying} onClick={() => setBootMediaDialog('change')}>
            Change ISO
          </Button>
          <Button size="xs" variant="ghost" disabled={applying} onClick={() => setBootMediaDialog('reapply')}>
            Re-apply
          </Button>
        </>
      ) : (
        <Button size="xs" variant="outline" colorPalette="brand" disabled={!canEnable || applying} onClick={() => setBootMediaDialog('enable')}>
          Enable Boot Media
        </Button>
      )}
      <Button size="xs" variant="ghost" loading={bootMediaBusy === 'probe'} disabled={bootMediaBusy !== null || applying} onClick={() => void probe()}>
        Re-detect Redfish
      </Button>
      <Button size="xs" variant="ghost" loading={bootMediaBusy === 'live'} disabled={bootMediaBusy !== null || applying} onClick={() => void checkLive()}>
        Check BMC
      </Button>
    </>
  )

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
        <ConnectionCard
          server={server}
          imageHref={deployedImageHref}
          loginUserAction={
            <Button
              size="xs"
              variant="outline"
              aria-label={server.defaultUser ? 'Change default user' : 'Set default user'}
              onClick={() => setDefaultUserOpen(true)}
            >
              {server.defaultUser ? 'Change' : 'Set default user'}
            </Button>
          }
        />
        {physical && (
          <ManagementControllerCard
            management={management}
            bootMedia={
              <BootMediaPanel state={bootMediaPanelState(bootMedia, bootMediaData)} live={live} actions={bootMediaActions} />
            }
          />
        )}
      </div>
      <div className="sw-server-summary-grid sw-server-summary-grid--expand-single">
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
      {defaultUserOpen && (
        <ServerDefaultUserDialog server={server} onClose={() => setDefaultUserOpen(false)} onChanged={reload} />
      )}
      {bootMediaDialog && bootMediaData && (
        <ServerBootMediaDialog
          server={server}
          bootMedia={bootMediaData}
          mode={bootMediaDialog}
          onClose={() => setBootMediaDialog(null)}
          onApplyStarted={applyWatch.start}
          onApplySettled={applyWatch.stop}
          onChanged={() => {
            setLive(undefined)
            bootMedia.reload()
          }}
        />
      )}
    </div>
  )
}
