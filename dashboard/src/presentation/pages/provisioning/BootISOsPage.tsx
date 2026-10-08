import { useCallback, useEffect, useRef, useState } from 'react'
import { Box, Button, Heading, HStack, Menu, Portal, Stack, Text } from '@chakra-ui/react'
import { ArrowRight, Disc, Download, EllipsisVertical, FileCode2, Network, Plus, RefreshCw, Server, Trash2 } from 'lucide-react'
import { Link as RouterLink } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { BootISO, BootISOCatalog } from '@/domain/provisioning/types'
import type { Integration } from '@/domain/site/types'
import { CodeBlock } from '@/presentation/components/CodeBlock'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { CopyButton } from '@/presentation/components/CopyButton'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import { Modal } from '@/presentation/components/ui/modal'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatBytes } from '@/shared/utils/bytes'
import { formatDateTime, formatRelative } from '@/shared/utils/time'
import { BuildBootISODialog } from './BuildBootISODialog'
import './boot-isos.css'
import { ProvisioningTabs } from './ProvisioningTabs'

interface ReadyBootISOsState {
  status: 'ready'
  catalog: BootISOCatalog
  integrations: Integration[]
  refreshedAt: string
  refreshError?: string
  refreshing: boolean
}

type BootISOsState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | ReadyBootISOsState

type LoadMode = 'replace' | 'refresh'

/** Why a Boot ISO cannot be deleted now, or an empty string when it can. */
function deleteBlockedReason(iso: BootISO): string {
  if (iso.inUseBy === 0) return ''
  const servers = iso.inUseBy === 1 ? '1 Server' : `${iso.inUseBy} Servers`
  return `${servers} use it as Boot Media. Disable Boot Media on them or switch them to another ISO first.`
}

/** "In use by" in words; never a bare number, so 0 reads as unused rather than missing. */
function inUseLabel(iso: BootISO): string {
  if (iso.inUseBy === 0) return 'Not in use'
  return iso.inUseBy === 1 ? 'Used by 1 Server' : `Used by ${iso.inUseBy} Servers`
}

function integrationHref(integrationId: string, siteId: string): string {
  const params = new URLSearchParams({ site: siteId })
  return `/infrastructure/integrations?${params.toString()}#integration-${encodeURIComponent(integrationId)}`
}

function siteHref(siteId: string): string {
  const params = new URLSearchParams({ site: siteId })
  return `/infrastructure/sites?${params.toString()}#site-${encodeURIComponent(siteId)}`
}

/**
 * Optional external-network boot setup for the current Site scope.
 *
 * A Boot ISO is only needed when a Server cannot use its provisioner's managed network path. Cards
 * keep serving, usage, chain, and technical facts distinct; a missing ISO is therefore an ordinary
 * empty setup rather than a fleet warning. Refreshes preserve the last-good setup, while Site
 * changes replace it and invalidate late responses from the previous Site.
 */
export function BootISOsPage() {
  const { provisioning, sites: siteRepository } = useApp()
  const { siteId, sites, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [state, setState] = useState<BootISOsState>({ status: 'loading' })
  const generationRef = useRef(0)
  const [building, setBuilding] = useState(false)
  const [scriptOf, setScriptOf] = useState<BootISO | null>(null)
  const [deleting, setDeleting] = useState<BootISO | null>(null)
  const [deleteBusy, setDeleteBusy] = useState(false)

  const load = useCallback(async (mode: LoadMode = 'refresh') => {
    const generation = ++generationRef.current
    if (mode === 'replace') {
      setState({ status: 'loading' })
    } else {
      setState((current) =>
        current.status === 'ready'
          ? { ...current, refreshing: true, refreshError: undefined }
          : { status: 'loading' },
      )
    }

    try {
      const [catalog, integrations] = await Promise.all([
        provisioning.listBootISOs({ siteId }),
        siteRepository.listIntegrations({ siteId, kind: 'provisioner' }),
      ])
      if (generation !== generationRef.current) return
      setState({
        status: 'ready',
        catalog,
        integrations,
        refreshedAt: new Date().toISOString(),
        refreshing: false,
      })
    } catch (error) {
      if (generation !== generationRef.current) return
      const message = error instanceof Error ? error.message : 'Could not load Boot ISOs.'
      setState((current) =>
        mode === 'refresh' && current.status === 'ready'
          ? { ...current, refreshing: false, refreshError: message }
          : { status: 'error', message },
      )
    }
  }, [provisioning, siteId, siteRepository])

  useEffect(() => {
    void load('replace')
    return () => {
      generationRef.current += 1
    }
  }, [load])

  const remove = async () => {
    if (!deleting || deleteBusy) return
    setDeleteBusy(true)
    try {
      await provisioning.deleteBootISO(deleting.id)
      showToast({ tone: 'success', title: `Boot ISO ${deleting.name} deleted` })
      setDeleting(null)
      await load('refresh')
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

  return (
    <div className="operator-page sw-boot-isos-page">
      <PageHeader
        title="Boot ISOs"
        subtitle="Optional iPXE boot media for Servers that use external DHCP instead of their provisioner's managed network."
        breadcrumbs={[{ label: 'Provisioning', href: scopedHref('/provisioning/images') }, { label: 'Boot ISOs' }]}
        stackActionsOnMobile
        actions={
          <HStack gap="2" width={{ base: '100%', md: 'auto' }}>
            <Button
              variant="outline"
              loading={ready?.refreshing}
              loadingText="Refreshing…"
              disabled={!ready}
              onClick={() => void load('refresh')}
            >
              <RefreshCw size={16} aria-hidden />
              Refresh
            </Button>
            <Tooltip content={buildDisabledReason} disabled={!buildDisabledReason}>
              <span>
                <Button colorPalette="brand" disabled={!ready || Boolean(buildDisabledReason)} onClick={() => setBuilding(true)}>
                  <Plus size={16} aria-hidden />
                  Build Boot ISO
                </Button>
              </span>
            </Tooltip>
          </HStack>
        }
      />
      <ProvisioningTabs />

      <section className="sw-boot-iso-setup" aria-labelledby="boot-iso-setup-title">
        <BootPathGuide />

        <div className="sw-boot-iso-resources">
          <div className="sw-boot-iso-resources__header">
            <Stack gap="1">
              <Heading as="h2" id="boot-iso-resources-title" size="md">
                Boot media
              </Heading>
              <Text color="fg.muted" fontSize="sm">
                {ready ? `${ready.catalog.items.length} configured` : 'Loading configured media'}
              </Text>
            </Stack>
            {ready && (
              <Text color="fg.muted" fontSize="xs">
                Updated {formatRelative(ready.refreshedAt)}
              </Text>
            )}
          </div>

          {ready?.refreshError && (
            <Alert status="warning" title="Could not refresh Boot ISOs">
              The last successful setup is still shown. {ready.refreshError}
            </Alert>
          )}
          {builderReason && (
            <Alert status="warning" title="Boot ISO builder unavailable">
              {builderReason} Existing ISO files remain available when they have a served URL.
            </Alert>
          )}

          {state.status === 'loading' && <LoadingState rows={3} />}
          {state.status === 'error' && <ErrorState message={state.message} onRetry={() => void load('replace')} />}
          {ready && ready.catalog.items.length === 0 && (
            <div className="sw-boot-iso-empty">
              <EmptyState
                icon={<Disc aria-hidden />}
                title="No Boot ISO configured"
                message={
                  noProvisioner
                    ? 'This Site has no provisioner Integration. Most Sites do not need a Boot ISO; connect one only when external DHCP prevents normal network boot.'
                    : 'This is normal for most Sites. Build an ISO only for Servers that receive their boot network from external DHCP.'
                }
              />
              <HStack className="sw-boot-iso-empty__actions" gap="2" justify="center" wrap="wrap">
                {noProvisioner ? (
                  <Button asChild variant="outline">
                    <RouterLink to={scopedHref('/infrastructure/integrations')}>Manage integrations</RouterLink>
                  </Button>
                ) : (
                  !buildDisabledReason && (
                    <Button colorPalette="brand" onClick={() => setBuilding(true)}>
                      <Plus size={16} aria-hidden />
                      Build Boot ISO
                    </Button>
                  )
                )}
              </HStack>
            </div>
          )}
          {ready && ready.catalog.items.length > 0 && (
            <div className="sw-boot-iso-list" role="list" aria-label="Boot ISOs">
              {ready.catalog.items.map((iso) => (
                <BootISOCard
                  key={iso.id}
                  iso={iso}
                  integrationName={integrationName(iso.integrationId)}
                  siteName={siteName(iso.siteId)}
                  showSite={!siteId}
                  onViewScript={() => setScriptOf(iso)}
                  onDelete={() => setDeleting(iso)}
                />
              ))}
            </div>
          )}
        </div>
      </section>

      {building && ready && (
        <BuildBootISODialog
          integrations={ready.integrations}
          onClose={() => setBuilding(false)}
          onBuilt={(iso) => {
            setBuilding(false)
            showToast({ tone: 'success', title: `Boot ISO ${iso.name} built`, description: `It chains to ${iso.chainUrl}.` })
            void load('refresh')
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

function BootPathGuide() {
  const steps = [
    { label: 'Server BMC', icon: Server },
    { label: 'Swallow Boot ISO', icon: Disc },
    { label: 'Site DHCP', icon: Network },
    { label: 'MAAS rack', icon: Server },
  ]

  return (
    <div className="sw-boot-iso-guide">
      <Stack className="sw-boot-iso-guide__copy" gap="2">
        <Text className="sw-boot-iso-eyebrow">Optional network path</Text>
        <Heading as="h2" id="boot-iso-setup-title" size="lg">
          External-network boot
        </Heading>
        <Text color="fg.muted">
          Most Sites do not need this. Use a Boot ISO only when a Server receives DHCP outside the
          provisioner network and cannot start its normal network-boot path.
        </Text>
      </Stack>
      <ol className="sw-boot-iso-flow" aria-label="External-network boot path">
        {steps.map((step, index) => {
          const Icon = step.icon
          return (
            <li key={step.label}>
              <div className="sw-boot-iso-flow__step">
                <Icon size={17} aria-hidden />
                <span>{step.label}</span>
              </div>
              {index < steps.length - 1 && <ArrowRight className="sw-boot-iso-flow__arrow" size={16} aria-hidden />}
            </li>
          )
        })}
      </ol>
      <Text className="sw-boot-iso-guide__note" color="fg.muted" fontSize="sm">
        The BMC mounts the ISO, iPXE obtains a Site DHCP lease, then chains to the selected MAAS rack.
      </Text>
    </div>
  )
}

interface BootISOCardProps {
  iso: BootISO
  integrationName: string
  siteName: string
  showSite: boolean
  onViewScript: () => void
  onDelete: () => void
}

function BootISOCard({
  iso,
  integrationName,
  siteName,
  showSite,
  onViewScript,
  onDelete,
}: BootISOCardProps) {
  const blocked = deleteBlockedReason(iso)
  const served = Boolean(iso.url)
  const headingId = `boot-iso-${iso.id}-title`

  return (
    <article className="sw-boot-iso-card" role="listitem" aria-labelledby={headingId}>
      <div className="sw-boot-iso-card__main">
        <div className="sw-boot-iso-card__identity">
          <Heading as="h3" id={headingId} size="md">
            {iso.name}
          </Heading>
          <HStack gap="2" wrap="wrap">
            <StatusBadge status={served ? 'active' : 'warning'} label={served ? 'Served' : 'Not served'} />
            <StatusBadge status={iso.inUseBy > 0 ? 'info' : 'unknown'} label={inUseLabel(iso)} />
          </HStack>
        </div>

        <dl className="sw-boot-iso-card__facts">
          <div>
            <dt>Provisioner Integration</dt>
            <dd>
              <RouterLink to={integrationHref(iso.integrationId, iso.siteId)}>{integrationName}</RouterLink>
              {showSite && (
                <Text as="span" color="fg.muted">
                  {' · '}
                  <RouterLink to={siteHref(iso.siteId)}>{siteName}</RouterLink>
                </Text>
              )}
            </dd>
          </div>
          <div>
            <dt>Boot path</dt>
            <dd className="sw-boot-iso-card__path">
              <span>Site DHCP</span>
              <ArrowRight size={14} aria-hidden />
              <span className="sw-mono">{iso.chainUrl}</span>
            </dd>
          </div>
        </dl>
      </div>

      <div className="sw-boot-iso-card__actions">
        <Tooltip
          content="This ISO has no served URL. Review the installation's Boot Media base URL."
          disabled={served}
        >
          <span>
            {served ? (
              <Button asChild variant="outline" size="sm">
                <a href={iso.url ?? undefined} download aria-label={`Download ${iso.name}`}>
                  <Download size={15} aria-hidden />
                  Download
                </a>
              </Button>
            ) : (
              <Button variant="outline" size="sm" disabled aria-label={`Download ${iso.name} unavailable`}>
                <Download size={15} aria-hidden />
                Download
              </Button>
            )}
          </span>
        </Tooltip>
        <Menu.Root>
          <Menu.Trigger asChild>
            <Button variant="outline" size="sm" aria-label={`More actions for ${iso.name}`}>
              <EllipsisVertical size={15} aria-hidden />
              More
            </Button>
          </Menu.Trigger>
          <Portal>
            <Menu.Positioner>
              <Menu.Content>
                <Menu.Item value="view-script" onClick={onViewScript}>
                  <FileCode2 size={15} aria-hidden />
                  View iPXE script
                </Menu.Item>
                <Menu.Item value="delete" color="fg.error" disabled={Boolean(blocked)} onClick={onDelete}>
                  <Trash2 size={15} aria-hidden />
                  <Box>
                    <Text>Delete Boot ISO</Text>
                    {blocked && (
                      <Text color="fg.muted" fontSize="xs" maxW="18rem">
                        {blocked}
                      </Text>
                    )}
                  </Box>
                </Menu.Item>
              </Menu.Content>
            </Menu.Positioner>
          </Portal>
        </Menu.Root>
      </div>

      <details className="sw-boot-iso-card__details">
        <summary>Technical details</summary>
        <DescriptionList
          items={[
            { label: 'Boot ISO ID', value: <CopyableValue value={iso.id} label="Copy Boot ISO ID" /> },
            { label: 'Rack address', value: <Text as="span" className="sw-mono">{iso.rackAddress}</Text> },
            { label: 'Chain URL', value: <CopyableValue value={iso.chainUrl} label="Copy chain URL" /> },
            {
              label: 'ISO URL',
              value: served ? (
                <CopyableValue value={iso.url ?? ''} label="Copy ISO URL" />
              ) : (
                <Text as="span" color="fg.muted">Not served</Text>
              ),
            },
            { label: 'SHA-256', value: <CopyableValue value={iso.sha256} label="Copy SHA-256" compact /> },
            { label: 'iPXE version', value: iso.ipxeVersion },
            { label: 'Size', value: formatBytes(iso.sizeBytes) },
            {
              label: 'Built',
              value: `${formatDateTime(iso.createdAt)}${iso.createdBy ? ` by ${iso.createdBy}` : ''}`,
            },
          ]}
        />
      </details>
    </article>
  )
}

function CopyableValue({ value, label, compact = false }: { value: string; label: string; compact?: boolean }) {
  return (
    <span className="sw-boot-iso-copyable">
      <Text as="span" className="sw-mono" fontSize={compact ? 'xs' : 'sm'}>
        {value}
      </Text>
      <CopyButton value={value} label={label} />
    </span>
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
