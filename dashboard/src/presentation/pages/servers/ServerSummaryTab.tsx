import { useState } from 'react'
import { Button } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { BootMediaLiveState, Server, ServerBootMedia, ServerPowerConfiguration } from '@/domain/server/types'
import { Alert } from '@/presentation/components/ui/alert'
import { BootMediaPanel, type BootMediaPanelState } from '@/presentation/components/serverSummary/BootMediaPanel'
import { PowerConfigurationPanel, type PowerConfigurationPanelState } from '@/presentation/components/serverSummary/PowerConfigurationPanel'
import {
  bootMediaTargetLabel,
  libvirtSupportLabel,
  redfishSupportLabel,
  shownBootMediaMethod,
} from '@/presentation/components/serverSummary/bootMediaLabels'
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
import { primaryImageArchitecture } from '@/presentation/pages/provisioning/osImageListPresentation'
import { useExperimentalFeature } from '@/presentation/contexts/ExperimentalFeaturesContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useAsyncData, type AsyncData } from '@/presentation/hooks/useAsyncData'
import { ServerBootMediaDialog, type ServerBootMediaDialogMode } from './ServerBootMediaDialog'
import { ServerDefaultUserDialog } from './ServerDefaultUserDialog'
import { ServerPowerConfigurationDialog } from './ServerPowerConfigurationDialog'
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
  if (server.architecture) target.searchParams.set('architecture', primaryImageArchitecture(server.architecture))
  if (axis.osSystem) target.searchParams.set('osSystem', axis.osSystem)
  if (axis.distroSeries) target.searchParams.set('release', axis.distroSeries)
  return `${target.pathname}${target.search}`
}

/**
 * Maps the Boot Media read onto the panel's state, with `latest` (the newest poll while a
 * preflight runs) in place of the page's read. The loader yields `null` only for a Server whose
 * Boot Media block is not rendered (no BMC and no Hypervisor shown), so that case never reaches the
 * screen.
 */
function bootMediaPanelState(state: AsyncData<ServerBootMedia | null>, latest: ServerBootMedia | null): BootMediaPanelState {
  if (state.status !== 'ready') return state
  return latest ? { status: 'ready', data: latest } : { status: 'loading' }
}

/**
 * Maps the Power Configuration read onto the panel's state, with `saved` (the configuration read
 * back by the last save on this page) in place of the read, so a save shows at once without a
 * reload that would flash the card back to loading. The loader yields `null` only when the
 * provisioner does not offer the capability.
 */
function powerPanelState(
  state: AsyncData<ServerPowerConfiguration | null>,
  saved: ServerPowerConfiguration | null,
): PowerConfigurationPanelState {
  if (saved) return { status: 'ready', data: saved }
  if (state.status !== 'ready') return state
  return state.data
    ? { status: 'ready', data: state.data }
    : { status: 'unavailable', message: 'This provisioner does not let swallow read or set the power configuration.' }
}

/**
 * Operator-first overview for one Server. Projection-backed state and capacity remain useful
 * when live provider detail fails; dedicated tabs own large Network, Storage, and PCI tables so
 * the overview stays a fast scan rather than a second inventory page. In-band SSH connection
 * info sits beside the out-of-band management controller, so both ways in read as one row; its
 * login user is the Server Default User, set or changed here through the Default user dialog, and
 * the Server is re-read after a change so the card and SSH command follow it.
 *
 * The management controller card always carries the Server's Power Configuration (decision 054),
 * read live from the provisioner when the page opens and editable through its dialog. Its driver
 * family decides whether the Server has a BMC: only a `bmc` driver of a Server outside a provisioner
 * VM host does. Until it is read, or when it cannot be, VM-host membership is the only signal, so a
 * physical Server keeps its card while the read is in flight.
 *
 * A Server with a BMC also shows its Boot Media there (decisions 047 and 049): the stored setting,
 * its Boot ISO, and the Redfish probe are read when the page opens; the BMC itself is read only
 * when the operator asks ("Check BMC"), and re-probing or changing the setting (enable, change ISO,
 * re-apply, disable) re-reads the block. While an enable preflight runs — sent from here, from
 * another tab, or before a reload — the block is re-read every two seconds for its progress and its
 * actions wait until it ends. A libvirt virtual machine (a `virsh` driver outside a provisioner VM
 * host) shows the same block through its Hypervisor (decision 055) while the experimental
 * `virtualMachines` switch is on; release builds keep it hidden. Any other Server reads no Boot
 * Media.
 */
export function ServerSummaryTab() {
  const { server, detail, detailError, reload } = useServerDetailContext()
  const { servers } = useApp()
  const { showToast } = useToast()
  const { scopedHref } = useSiteScope()
  const [tagEditorOpen, setTagEditorOpen] = useState(false)
  const [defaultUserOpen, setDefaultUserOpen] = useState(false)
  const [powerDialogOpen, setPowerDialogOpen] = useState(false)
  const [savedPower, setSavedPower] = useState<ServerPowerConfiguration | null>(null)
  const [bootMediaDialog, setBootMediaDialog] = useState<ServerBootMediaDialogMode | null>(null)
  const [bootMediaBusy, setBootMediaBusy] = useState<'probe' | 'live' | null>(null)
  const [live, setLive] = useState<{ state: BootMediaLiveState | null; error?: string } | undefined>(undefined)
  const system = detail?.sections.find((section) => section.title === 'System')
  const management = detail?.sections.find((section) => section.title === 'BMC')
  const storage = detail ? findTable(detail.tables, 'Storage') : undefined
  const deployedImageHref = deployedImageCatalogHref(server, scopedHref)
  // Unknown until the provisioner detail arrives; only an explicit false skips the read.
  const powerSupported = detail?.capabilities.powerConfiguration !== false
  const powerConfiguration = useAsyncData(
    async () => (powerSupported ? servers.getPowerConfiguration(server.id) : null),
    [servers, server.id, powerSupported],
  )
  const power = powerPanelState(powerConfiguration, savedPower?.serverId === server.id ? savedPower : null)
  const hasBMC = power.status === 'ready'
    ? power.data.family === 'bmc' && !server.providerPod
    : !server.providerPod
  const virtualMachines = useExperimentalFeature('virtualMachines')
  // Only a virsh driver the operator controls (not a provisioner VM host's) can name a swallow
  // Hypervisor; whether it does is the Boot Media read's answer (`libvirt.support`).
  const throughHypervisor = virtualMachines && power.status === 'ready' && power.data.family === 'virsh' && !server.providerPod
  const readsBootMedia = hasBMC || throughHypervisor
  const bootMedia = useAsyncData(
    async () => (readsBootMedia ? servers.getBootMedia(server.id) : null),
    [servers, server.id, readsBootMedia],
  )

  const probe = async () => {
    setBootMediaBusy('probe')
    try {
      const { redfish, libvirt } = await servers.probeBootMedia(server.id)
      if (libvirt) {
        showToast({ tone: libvirt.support === 'supported' ? 'success' : 'warning', title: libvirtSupportLabel(libvirt.support), description: libvirt.reason })
      } else if (redfish) {
        showToast({ tone: redfish.support === 'supported' ? 'success' : 'warning', title: redfishSupportLabel(redfish.support), description: redfish.reason })
      }
      bootMedia.reload()
    } catch (caught) {
      showToast({ tone: 'error', title: 'Boot Media probe failed', description: caught instanceof Error ? caught.message : undefined })
    } finally {
      setBootMediaBusy(null)
    }
  }

  // A live read reaches the BMC or Hypervisor and takes seconds, so it runs only on request and its
  // answer is kept beside the stored facts until the next read or setting change.
  const checkLive = async () => {
    setBootMediaBusy('live')
    try {
      const result = await servers.getBootMedia(server.id, { live: true })
      setLive({ state: result.live, error: result.liveError })
    } catch (caught) {
      setLive({ state: null, error: caught instanceof Error ? caught.message : `The ${target} could not be read.` })
    } finally {
      setBootMediaBusy(null)
    }
  }

  const applyWatch = useBootMediaApplyWatch(servers, server.id, bootMedia.status === 'ready' ? bootMedia.data : null)
  const bootMediaData = applyWatch.media
  const method = bootMediaData ? shownBootMediaMethod(bootMediaData) : null
  const target = bootMediaTargetLabel(method)
  const enabled = bootMediaData?.setting?.enabled ?? false
  // The Boot ISO is chosen in the dialog, so only a Server without a method cannot start one. While
  // a preflight runs every action waits: the API refuses a second write, and a probe or live read
  // would compete with the preflight's own requests on a slow BMC.
  const applying = applyWatch.applying
  const canEnable = method === 'libvirt'
    ? bootMediaData?.libvirt?.support !== 'no_hypervisor'
    : bootMediaData?.redfish?.support !== 'no_bmc'
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
        {method === 'libvirt' ? 'Re-detect hypervisor' : 'Re-detect Redfish'}
      </Button>
      <Button size="xs" variant="ghost" loading={bootMediaBusy === 'live'} disabled={bootMediaBusy !== null || applying} onClick={() => void checkLive()}>
        {target === 'hypervisor' ? 'Check hypervisor' : 'Check BMC'}
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
        <ManagementControllerCard
          hasBMC={hasBMC}
          management={management}
          power={
            <PowerConfigurationPanel
              state={power}
              compact={hasBMC}
              actions={power.status === 'ready' && (
                <Button
                  size="xs"
                  variant="outline"
                  colorPalette="brand"
                  disabled={!power.data.editable}
                  onClick={() => setPowerDialogOpen(true)}
                >
                  {power.data.driver ? 'Edit power configuration' : 'Set power configuration'}
                </Button>
              )}
            />
          }
          bootMedia={readsBootMedia && (
            <BootMediaPanel state={bootMediaPanelState(bootMedia, bootMediaData)} live={live} actions={bootMediaActions} />
          )}
        />
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
      {powerDialogOpen && power.status === 'ready' && (
        <ServerPowerConfigurationDialog
          server={server}
          configuration={power.data}
          onClose={() => setPowerDialogOpen(false)}
          onChanged={(saved) => {
            setSavedPower(saved)
            // The provisioner detail's BMC facts follow the new driver.
            reload()
          }}
        />
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
