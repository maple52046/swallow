import { useCallback, useEffect, useState } from 'react'
import { Button, HStack, Stack, Table, Text } from '@chakra-ui/react'
import { Download, FileCode2, Plus, Trash2 } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import type { BootISO, BootISOCatalog } from '@/domain/provisioning/types'
import type { Integration } from '@/domain/site/types'
import { CodeBlock } from '@/presentation/components/CodeBlock'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { CopyButton } from '@/presentation/components/CopyButton'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import { Modal } from '@/presentation/components/ui/modal'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatBytes } from '@/shared/utils/bytes'
import { formatDateTime } from '@/shared/utils/time'
import { BuildBootISODialog } from './BuildBootISODialog'
import { ProvisioningTabs } from './ProvisioningTabs'

type BootISOsState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; catalog: BootISOCatalog; integrations: Integration[] }

/** Why a Boot ISO cannot be deleted now, or an empty string when it can. */
function deleteBlockedReason(iso: BootISO): string {
  if (iso.inUseBy === 0) return ''
  const servers = iso.inUseBy === 1 ? '1 Server' : `${iso.inUseBy} Servers`
  return `${servers} use it as Boot Media. Disable Boot Media on them or switch them to another ISO first.`
}

/** "In use by" in words; never a bare number, so 0 reads as unused rather than missing. */
function inUseLabel(iso: BootISO): string {
  if (iso.inUseBy === 0) return 'Not used'
  return iso.inUseBy === 1 ? '1 Server' : `${iso.inUseBy} Servers`
}

/**
 * The Provisioning workspace's Boot ISOs tab (`/provisioning/boot-isos`, admin-only like every
 * provisioning route): the iPXE boot ISOs swallow built per provisioner (glossary Boot ISO, decision
 * 049), in the current Site scope.
 *
 * Each ISO chains to one provisioner's MAAS rack; a Server's Boot Media (Server Summary) mounts one
 * of its own provisioner's ISOs through the BMC. The page lists them with the rack and chain URL,
 * the iPXE version, size, and how many Servers' Boot Media use them, and offers View script (the
 * rendered template, read-only), Download (the same unauthenticated URL BMCs mount), and Delete —
 * refused with the reason while any Server uses the ISO, matching the API's 409.
 *
 * Building needs the installation's iPXE assets and a Boot Media base URL. When the API reports the
 * builder unavailable, its reason is shown and Build ISO is disabled, but existing ISOs stay listed
 * and deletable. A Site with no provisioner disables Build ISO with that explanation instead.
 */
export function BootISOsPage() {
  const { provisioning, sites: siteRepository } = useApp()
  const { siteId, sites, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [state, setState] = useState<BootISOsState>({ status: 'loading' })
  const [building, setBuilding] = useState(false)
  const [scriptOf, setScriptOf] = useState<BootISO | null>(null)
  const [deleting, setDeleting] = useState<BootISO | null>(null)
  const [deleteBusy, setDeleteBusy] = useState(false)

  const load = useCallback(async () => {
    setState({ status: 'loading' })
    try {
      const [catalog, integrations] = await Promise.all([
        provisioning.listBootISOs({ siteId }),
        siteRepository.listIntegrations({ siteId, kind: 'provisioner' }),
      ])
      setState({ status: 'ready', catalog, integrations })
    } catch (error) {
      setState({ status: 'error', message: error instanceof Error ? error.message : 'Could not load Boot ISOs.' })
    }
  }, [provisioning, siteId, siteRepository])

  useEffect(() => {
    void load()
  }, [load])

  const remove = async () => {
    if (!deleting || deleteBusy) return
    setDeleteBusy(true)
    try {
      await provisioning.deleteBootISO(deleting.id)
      showToast({ tone: 'success', title: `Boot ISO ${deleting.name} deleted` })
      setDeleting(null)
      await load()
    } catch (error) {
      showToast({ tone: 'error', title: 'Could not delete the Boot ISO', description: error instanceof Error ? error.message : 'Unknown error' })
    } finally {
      setDeleteBusy(false)
    }
  }

  const ready = state.status === 'ready' ? state : null
  const integrationName = (id: string) => ready?.integrations.find((item) => item.id === id)?.name ?? id
  const siteName = (id: string) => sites.find((item) => item.id === id)?.name ?? id
  const builderReason = ready && !ready.catalog.builder.available ? ready.catalog.builder.reason ?? 'This installation cannot build Boot ISOs.' : ''
  const noProvisioner = ready !== null && ready.integrations.length === 0
  const buildDisabledReason = builderReason
    ? 'This installation cannot build Boot ISOs; see the notice below.'
    : noProvisioner
      ? 'This Site has no provisioner integration to build an ISO for.'
      : ''

  const rowActions = (iso: BootISO, compact: boolean) => {
    const blocked = deleteBlockedReason(iso)
    return (
      <>
        <Button
          size={compact ? 'xs' : 'sm'}
          variant={compact ? 'plain' : 'outline'}
          aria-label={`View script of ${iso.name}`}
          onClick={() => setScriptOf(iso)}
        >
          <FileCode2 size={14} aria-hidden />
          View script
        </Button>
        {iso.url && (
          // The same unauthenticated URL BMCs mount, so a download proves what they will get. It is
          // usually another origin (the Boot Media base URL), where `download` is ignored; the API
          // names the file swallow-ipxe.iso either way.
          <Button asChild size={compact ? 'xs' : 'sm'} variant={compact ? 'plain' : 'outline'}>
            <a href={iso.url} download aria-label={`Download ${iso.name}`}>
              <Download size={14} aria-hidden />
              Download
            </a>
          </Button>
        )}
        {/* The span keeps the tooltip reachable: a disabled button receives no pointer events. */}
        <Tooltip content={blocked} disabled={!blocked}>
          <span>
            <Button
              size={compact ? 'xs' : 'sm'}
              variant={compact ? 'plain' : 'outline'}
              colorPalette="red"
              disabled={Boolean(blocked)}
              aria-label={blocked ? `Delete ${iso.name} (unavailable: in use)` : `Delete ${iso.name}`}
              onClick={() => setDeleting(iso)}
            >
              <Trash2 size={14} aria-hidden />
              Delete
            </Button>
          </span>
        </Tooltip>
      </>
    )
  }

  return (
    <div className="operator-page">
      <PageHeader
        title="Boot ISOs"
        subtitle="iPXE boot ISOs that reach a provisioner from a network whose DHCP it does not run. A Server's Boot Media mounts one through its BMC."
        breadcrumbs={[{ label: 'Provisioning', href: scopedHref('/provisioning/images') }, { label: 'Boot ISOs' }]}
        actions={
          <Tooltip content={buildDisabledReason} disabled={!buildDisabledReason}>
            <span>
              <Button colorPalette="brand" disabled={!ready || Boolean(buildDisabledReason)} onClick={() => setBuilding(true)}>
                <Plus size={16} />
                Build ISO
              </Button>
            </span>
          </Tooltip>
        }
      />
      <ProvisioningTabs />

      {builderReason && (
        <Alert status="warning" title="Boot ISOs cannot be built here">
          {builderReason}
        </Alert>
      )}

      {state.status === 'loading' && <LoadingState rows={4} />}
      {state.status === 'error' && <ErrorState message={state.message} onRetry={() => void load()} />}
      {ready && ready.catalog.items.length === 0 && (
        <EmptyState
          title="No Boot ISOs"
          message={
            noProvisioner
              ? 'Add a provisioner integration to this Site, then build an ISO for it.'
              : 'Build one for a provisioner, then choose it in a Server’s Boot Media.'
          }
          action={buildDisabledReason ? undefined : { label: 'Build ISO', onClick: () => setBuilding(true) }}
        />
      )}
      {ready && ready.catalog.items.length > 0 && (
        <ResponsiveDataView
          desktop={
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Boot ISOs" className="sw-provisioning-table">
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeader>Name</Table.ColumnHeader>
                    <Table.ColumnHeader>Provisioner</Table.ColumnHeader>
                    <Table.ColumnHeader>Chains to</Table.ColumnHeader>
                    <Table.ColumnHeader>Size</Table.ColumnHeader>
                    <Table.ColumnHeader>In use by</Table.ColumnHeader>
                    <Table.ColumnHeader>Built</Table.ColumnHeader>
                    <Table.ColumnHeader />
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {ready.catalog.items.map((iso) => (
                    <Table.Row key={iso.id}>
                      <Table.Cell>
                        <strong>{iso.name}</strong>
                        <Text as="small" display="block" color="fg.muted">
                          iPXE {iso.ipxeVersion}
                        </Text>
                      </Table.Cell>
                      <Table.Cell>
                        {integrationName(iso.integrationId)}
                        <Text as="small" display="block" color="fg.muted">
                          {siteName(iso.siteId)}
                        </Text>
                      </Table.Cell>
                      <Table.Cell className="sw-mono">{iso.chainUrl}</Table.Cell>
                      <Table.Cell>{formatBytes(iso.sizeBytes)}</Table.Cell>
                      <Table.Cell>{inUseLabel(iso)}</Table.Cell>
                      <Table.Cell>
                        {formatDateTime(iso.createdAt)}
                        {iso.createdBy && (
                          <Text as="small" display="block" color="fg.muted">
                            by {iso.createdBy}
                          </Text>
                        )}
                      </Table.Cell>
                      <Table.Cell textAlign="end">
                        <HStack gap="1" justify="flex-end" wrap="wrap">
                          {rowActions(iso, true)}
                        </HStack>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          }
          mobile={
            <div className="sw-resource-card-list">
              {ready.catalog.items.map((iso) => (
                <ResourceCard key={iso.id} title={iso.name} description={iso.chainUrl} actions={rowActions(iso, false)}>
                  <ResourceCardField label="Provisioner">
                    {integrationName(iso.integrationId)} · {siteName(iso.siteId)}
                  </ResourceCardField>
                  <ResourceCardField label="Size">{formatBytes(iso.sizeBytes)}</ResourceCardField>
                  <ResourceCardField label="In use by">{inUseLabel(iso)}</ResourceCardField>
                  <ResourceCardField label="Built">{formatDateTime(iso.createdAt)}</ResourceCardField>
                </ResourceCard>
              ))}
            </div>
          }
        />
      )}

      {building && ready && (
        <BuildBootISODialog
          integrations={ready.integrations}
          onClose={() => setBuilding(false)}
          onBuilt={(iso) => {
            setBuilding(false)
            showToast({ tone: 'success', title: `Boot ISO ${iso.name} built`, description: `It chains to ${iso.chainUrl}.` })
            void load()
          }}
        />
      )}
      {scriptOf && <BootISOScriptDialog iso={scriptOf} onClose={() => setScriptOf(null)} />}
      <ConfirmDialog
        open={deleting !== null}
        title="Delete Boot ISO"
        confirmLabel={deleting ? `Delete ${deleting.name}` : 'Delete'}
        busy={deleteBusy}
        onConfirm={() => void remove()}
        onCancel={() => setDeleting(null)}
      >
        <Text>
          Delete <strong>{deleting?.name}</strong>? Its file is removed and its URL stops working, so no BMC can mount it
          again. No Server uses it now. This cannot be undone; you can build a new one at any time.
        </Text>
      </ConfirmDialog>
    </div>
  )
}

/**
 * The rendered iPXE script and build facts of one Boot ISO, read-only. The script is swallow's
 * template filled with the rack address; showing it lets an operator check what a Server will run
 * before choosing the ISO, and the SHA-256 lets them verify a downloaded copy.
 */
function BootISOScriptDialog({ iso, onClose }: { iso: BootISO; onClose: () => void }) {
  return (
    <Modal
      open
      onClose={onClose}
      size="lg"
      title={`iPXE script of ${iso.name}`}
      footer={
        <Button variant="ghost" onClick={onClose}>
          Close
        </Button>
      }
    >
      <Stack gap="4">
        <CodeBlock code={iso.script} language="bash" aria-label={`iPXE script of ${iso.name}`} />
        <DescriptionList
          items={[
            { label: 'Rack address', value: <Text as="span" className="sw-mono">{iso.rackAddress}</Text> },
            { label: 'Chains to', value: <Text as="span" className="sw-mono">{iso.chainUrl}</Text> },
            { label: 'iPXE', value: iso.ipxeVersion },
            {
              label: 'ISO URL',
              value: iso.url ? (
                <HStack gap="1">
                  <Text as="span" className="sw-mono" fontSize="sm" wordBreak="break-all">{iso.url}</Text>
                  <CopyButton value={iso.url} label="Copy ISO URL" />
                </HStack>
              ) : (
                <Text as="span" color="fg.muted">Not served: this installation has no Boot Media base URL.</Text>
              ),
            },
            {
              label: 'SHA-256',
              value: (
                <HStack gap="1">
                  <Text as="span" className="sw-mono" fontSize="xs" wordBreak="break-all">{iso.sha256}</Text>
                  <CopyButton value={iso.sha256} label="Copy SHA-256" />
                </HStack>
              ),
            },
          ]}
        />
      </Stack>
    </Modal>
  )
}
