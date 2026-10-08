import { useCallback, useEffect, useMemo, useState } from 'react'
import { Badge, Button, Heading, HStack, Menu, Portal, Table, Text } from '@chakra-ui/react'
import { ChevronDown, Plus, Settings } from 'lucide-react'
import { Link as RouterLink, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  loadSoftwareDetailWorkspace,
  type SoftwareDetailWorkingSet,
} from '@/application/usecases/software/loadSoftwareWorkspace'
import { useApp } from '@/di/AppProvider'
import { serverDisplayName } from '@/domain/server/types'
import { isSoftwareKind, type SoftwareAssignment } from '@/domain/software/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { InventorySurface, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { Pagination } from '@/presentation/components/Pagination'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { Alert } from '@/presentation/components/ui/alert'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatDateTime } from '@/shared/utils/time'
import { InstallSoftwareDialog } from './InstallSoftwareDialog'
import { UninstallSoftwareDialog } from './UninstallSoftwareDialog'
import {
  filterSoftwareDeployments,
  isSoftwareAssignmentChanging,
  softwareAssignmentSummary,
  softwareCatalogPresentation,
  softwareDeploymentRows,
  type SoftwareDeploymentLens,
  type SoftwareDeploymentRow,
} from './softwareCatalogPresentation'
import { assignmentStateLabel, assignmentStatePalette, softwareSettingsPath } from './softwarePresentation'
import './software.css'

type DetailState =
  | { status: 'loading' }
  | { status: 'not-found' }
  | { status: 'error'; message: string }
  | { status: 'ready'; data: SoftwareDetailWorkingSet; refreshError?: string }

const PAGE_SIZE = 10
const REFRESH_INTERVAL_MS = 5_000
const VALID_LENSES = new Set<SoftwareDeploymentLens>(['all', 'installed', 'changing', 'failed'])

/**
 * Routed Managed Software workspace for identity, capabilities, deployments, and actions.
 *
 * Assignments are joined to the active Site's deployed Server working set before rendering, so an
 * all-Sites route can disambiguate names while a Site-scoped route never leaks another Site's
 * assignment. Background refresh retains the last good rows and reports staleness non-blockingly.
 */
export function SoftwareDetailPage() {
  const { kind: requestedKind } = useParams<{ kind: string }>()
  const { software, servers } = useApp()
  const { siteId, sites, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [state, setState] = useState<DetailState>({ status: 'loading' })
  const [installTarget, setInstallTarget] = useState<'new' | SoftwareDeploymentRow | null>(null)
  const [uninstalling, setUninstalling] = useState<SoftwareAssignment | null>(null)
  const kind = requestedKind && isSoftwareKind(requestedKind) ? requestedKind : null

  const load = useCallback(async (background = false) => {
    if (!kind) {
      setState({ status: 'not-found' })
      return
    }
    if (!background) setState({ status: 'loading' })
    try {
      const data = await loadSoftwareDetailWorkspace(software, servers, kind, siteId)
      setState(data.entry ? { status: 'ready', data } : { status: 'not-found' })
    } catch (caught) {
      const message = caught instanceof Error ? caught.message : 'Software details could not be loaded.'
      setState((current) => background && current.status === 'ready'
        ? { ...current, refreshError: message }
        : { status: 'error', message })
    }
  }, [kind, siteId, software, servers])

  useEffect(() => {
    let cancelled = false
    if (!kind) {
      void Promise.resolve().then(() => {
        if (!cancelled) setState({ status: 'not-found' })
      })
      return () => {
        cancelled = true
      }
    }
    void Promise.resolve().then(() => {
      if (!cancelled) setState({ status: 'loading' })
    })
    void loadSoftwareDetailWorkspace(software, servers, kind, siteId).then(
      (data) => {
        if (!cancelled) setState(data.entry ? { status: 'ready', data } : { status: 'not-found' })
      },
      (caught: unknown) => {
        if (!cancelled) {
          setState({
            status: 'error',
            message: caught instanceof Error ? caught.message : 'Software details could not be loaded.',
          })
        }
      },
    )
    return () => {
      cancelled = true
    }
  }, [kind, siteId, software, servers])

  const scopedRows = useMemo(() => {
    if (state.status !== 'ready' || !kind) return []
    return softwareDeploymentRows(
      state.data.assignments.filter((assignment) => assignment.kind === kind),
      state.data.servers,
    )
  }, [kind, state])
  const hasChanging = scopedRows.some(({ assignment }) => isSoftwareAssignmentChanging(assignment))

  useEffect(() => {
    if (!hasChanging) return
    const timer = window.setTimeout(() => void load(true), REFRESH_INTERVAL_MS)
    return () => window.clearTimeout(timer)
  }, [hasChanging, load, state])

  if (state.status === 'loading') return <LoadingState rows={6} />
  if (state.status === 'error') return <ErrorState message={state.message} onRetry={() => void load()} />
  if (state.status === 'not-found' || !kind || !state.data.entry) {
    return (
      <div className="operator-page">
        <PageHeader title="Software not found" breadcrumbs={[{ label: 'Software', href: scopedHref('/software') }, { label: 'Not found' }]} />
        <EmptyState
          title="Software not found"
          message="This software kind is not published by the active catalog."
          action={{ label: 'Back to Software', onClick: () => navigate(scopedHref('/software')) }}
        />
      </div>
    )
  }

  const { entry } = state.data
  const presentation = softwareCatalogPresentation(entry.kind)
  const Icon = presentation.icon
  const query = searchParams.get('q') ?? ''
  const requestedLens = searchParams.get('state') as SoftwareDeploymentLens | null
  const lens = requestedLens && VALID_LENSES.has(requestedLens) ? requestedLens : 'all'
  const rawPage = Number.parseInt(searchParams.get('page') ?? '1', 10)
  const filteredRows = filterSoftwareDeployments(scopedRows, query, lens)
  const totalPages = Math.max(1, Math.ceil(filteredRows.length / PAGE_SIZE))
  const page = Number.isFinite(rawPage) ? Math.min(Math.max(rawPage, 1), totalPages) : 1
  const pageRows = filteredRows.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)
  const rangeStart = filteredRows.length === 0 ? 0 : (page - 1) * PAGE_SIZE + 1
  const rangeEnd = Math.min(page * PAGE_SIZE, filteredRows.length)

  const updateParams = (mutate: (next: URLSearchParams) => void) => {
    setSearchParams((current) => {
      const next = new URLSearchParams(current)
      mutate(next)
      return next
    })
  }
  const setFilter = (nextLens: SoftwareDeploymentLens) => updateParams((next) => {
    if (nextLens === 'all') next.delete('state')
    else next.set('state', nextLens)
    next.delete('page')
  })
  const clearFilters = () => updateParams((next) => {
    next.delete('q')
    next.delete('state')
    next.delete('page')
  })
  const siteName = (id: string) => sites.find((site) => site.id === id)?.name ?? id
  const serverLabel = (id: string) => {
    const server = state.data.servers.find((candidate) => candidate.id === id)
    return server ? serverDisplayName(server) : id
  }

  const handleLaunched = (operationId: string, title: string) => {
    setInstallTarget(null)
    setUninstalling(null)
    showToast({ tone: 'success', title })
    navigate(scopedHref(`/workflows/${operationId}`))
  }

  return (
    <div className="operator-page sw-software-detail-page">
      <PageHeader
        title={entry.label}
        subtitle={presentation.description}
        breadcrumbs={[{ label: 'Software', href: scopedHref('/software') }, { label: entry.label }]}
        metadata={<Badge variant="subtle" colorPalette="gray">{presentation.category === 'container-runtimes' ? 'Container runtime' : 'Storage & file sharing'}</Badge>}
        stackActionsOnMobile
        actions={
          <>
            {presentation.hasSettings && (
              <Button asChild variant="outline">
                <RouterLink to={scopedHref(softwareSettingsPath(entry.kind))}>
                  <Settings size={16} aria-hidden />
                  Settings
                </RouterLink>
              </Button>
            )}
            <Button colorPalette="brand" onClick={() => setInstallTarget('new')}>
              <Plus size={16} aria-hidden />
              Install software
            </Button>
          </>
        }
      />

      {state.refreshError && (
        <Alert status="warning" title="Deployment data may be out of date">
          <HStack justify="space-between" gap="3" wrap="wrap">
            <Text>{state.refreshError}</Text>
            <Button size="xs" variant="outline" onClick={() => void load(true)}>Retry</Button>
          </HStack>
        </Alert>
      )}

      <section className="sw-software-detail-hero" aria-labelledby="software-capabilities-title">
        <div className="sw-software-detail-hero__identity">
          <span className="sw-software-detail-hero__icon" aria-hidden><Icon size={25} /></span>
          <div>
            <Text className="sw-software-detail-hero__eyebrow">Managed Software</Text>
            <Heading as="h2" id="software-capabilities-title" size="lg">What Swallow manages</Heading>
          </div>
        </div>
        <div className="sw-software-capability-grid">
          <SoftwareCapability label="Capabilities" value={presentation.capabilities.join(' · ')} />
          <SoftwareCapability
            label="Roles"
            value={entry.roles.length > 0 ? entry.roles.join(' and ') : 'No role variants'}
          />
          <SoftwareCapability
            label="Compatibility"
            value={[
              entry.mutuallyExclusiveWith.length > 0 ? `Cannot coexist with ${entry.mutuallyExclusiveWith.join(', ')}` : 'No catalog conflicts',
              entry.refusedForKubernetesMembers ? 'Not available to Kubernetes members' : 'Platform membership allowed',
            ].join(' · ')}
          />
        </div>
      </section>

      <InventorySurface
        headingId="software-deployments-title"
        eyebrow="Server deployments"
        title="Deployments"
        summary={`Showing ${rangeStart}–${rangeEnd} of ${filteredRows.length} deployments`}
        className="sw-software-deployments"
        toolbar={
          <div className="sw-software-deployment-toolbar">
            <SearchInput
              value={query}
              onChange={(value) => updateParams((next) => {
                if (value) next.set('q', value)
                else next.delete('q')
                next.delete('page')
              })}
              placeholder="Search deployed Servers"
              aria-label="Search software deployments"
            />
            <HStack gap="1" wrap="wrap" aria-label="Deployment status filters">
              {(['all', 'installed', 'changing', 'failed'] as const).map((value) => (
                <Button key={value} size="sm" variant={lens === value ? 'solid' : 'ghost'} colorPalette={lens === value ? 'brand' : 'gray'} onClick={() => setFilter(value)}>
                  {value === 'all' ? 'All' : value[0].toUpperCase() + value.slice(1)}
                </Button>
              ))}
            </HStack>
          </div>
        }
      >
        {scopedRows.length === 0 ? (
          <EmptyState
            title={`No ${entry.label} deployments yet`}
            message="Install this software on one or more deployed Servers to manage it here."
          />
        ) : filteredRows.length === 0 ? (
          <EmptyState
            title="No deployments match these filters"
            message="Try another Server name or deployment state."
            action={{ label: 'Clear filters', onClick: clearFilters }}
          />
        ) : (
          <>
            <ResponsiveDataView
              desktop={
                <StickyTableFrame>
                  <Table.Root size="sm" aria-label={`${entry.label} deployments`} className="sw-software-deployment-table">
                    <Table.Header>
                      <Table.Row>
                        <Table.ColumnHeader>Server</Table.ColumnHeader>
                        <Table.ColumnHeader>Configuration</Table.ColumnHeader>
                        <Table.ColumnHeader>State</Table.ColumnHeader>
                        <Table.ColumnHeader>Last applied</Table.ColumnHeader>
                        <Table.ColumnHeader>Workflow</Table.ColumnHeader>
                        <Table.ColumnHeader aria-label="Deployment actions" />
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {pageRows.map((row) => (
                        <SoftwareDeploymentTableRow
                          key={row.assignment.serverId}
                          row={row}
                          showSite={!siteId}
                          siteName={siteName(row.server.source.siteId)}
                          scopedHref={scopedHref}
                          onReconfigure={() => setInstallTarget(row)}
                          onUninstall={() => setUninstalling(row.assignment)}
                        />
                      ))}
                    </Table.Body>
                  </Table.Root>
                </StickyTableFrame>
              }
              mobile={
                <div className="sw-resource-card-list" aria-label={`${entry.label} deployments`}>
                  {pageRows.map((row) => (
                    <SoftwareDeploymentCard
                      key={row.assignment.serverId}
                      row={row}
                      showSite={!siteId}
                      siteName={siteName(row.server.source.siteId)}
                      scopedHref={scopedHref}
                      onReconfigure={() => setInstallTarget(row)}
                      onUninstall={() => setUninstalling(row.assignment)}
                    />
                  ))}
                </div>
              }
            />
            <div className="sw-software-pagination">
              <Pagination
                total={totalPages}
                value={page}
                onChange={(nextPage) => updateParams((next) => {
                  if (nextPage > 1) next.set('page', String(nextPage))
                  else next.delete('page')
                })}
              />
            </div>
          </>
        )}
      </InventorySurface>

      {installTarget && (
        <InstallSoftwareDialog
          catalog={state.data.catalog}
          assignments={state.data.assignments}
          servers={installTarget === 'new' ? state.data.servers : [installTarget.server]}
          fixedKind={entry.kind}
          fixedServer={installTarget === 'new' ? undefined : installTarget.server}
          onClose={() => setInstallTarget(null)}
          onLaunched={(operationId) => handleLaunched(operationId, installTarget === 'new' ? 'Software install started' : 'Software configuration started')}
        />
      )}
      {uninstalling && (
        <UninstallSoftwareDialog
          assignment={uninstalling}
          serverLabel={serverLabel(uninstalling.serverId)}
          onClose={() => setUninstalling(null)}
          onLaunched={(operationId) => handleLaunched(operationId, 'Software uninstall started')}
        />
      )}
    </div>
  )
}

/** One contract-backed capability fact in the Software hero. */
function SoftwareCapability({ label, value }: { label: string; value: string }) {
  return <div><Text as="dt">{label}</Text><Text as="dd">{value}</Text></div>
}

interface DeploymentPresentationProps {
  row: SoftwareDeploymentRow
  showSite: boolean
  siteName: string
  scopedHref: (path: string) => string
  onReconfigure: () => void
  onUninstall: () => void
}

/** Contextual primary and overflow actions shared by desktop rows and mobile cards. */
function SoftwareDeploymentActions({ row, scopedHref, onReconfigure, onUninstall }: DeploymentPresentationProps) {
  const { assignment, server } = row
  const changing = isSoftwareAssignmentChanging(assignment)
  return (
    <HStack gap="2" justify="flex-end" wrap="wrap">
      {assignment.state === 'installed' ? (
        <Button asChild size="sm" variant="outline"><RouterLink to={scopedHref(`/servers/${server.id}/summary`)}>Open server</RouterLink></Button>
      ) : (
        <Button asChild size="sm" variant={assignment.state === 'failed' ? 'solid' : 'outline'} colorPalette={assignment.state === 'failed' ? 'red' : 'brand'}>
          <RouterLink to={scopedHref(`/workflows/${assignment.lastWorkflowId}`)}>View workflow</RouterLink>
        </Button>
      )}
      {!changing && (
        <Menu.Root positioning={{ placement: 'bottom-end' }}>
          <Menu.Trigger asChild><Button size="sm" variant="ghost">More <ChevronDown size={14} aria-hidden /></Button></Menu.Trigger>
          <Portal><Menu.Positioner><Menu.Content>
            <Menu.Item value="configure" onSelect={onReconfigure}>{assignment.state === 'failed' ? 'Retry install' : 'Reconfigure'}</Menu.Item>
            <Menu.Item value="uninstall" color="red.fg" onSelect={onUninstall}>Uninstall</Menu.Item>
          </Menu.Content></Menu.Positioner></Portal>
        </Menu.Root>
      )}
    </HStack>
  )
}

/** Desktop representation of one Software Assignment. */
function SoftwareDeploymentTableRow(props: DeploymentPresentationProps) {
  const { row, showSite, siteName, scopedHref } = props
  return (
    <Table.Row data-tone={row.assignment.state === 'failed' ? 'attention' : isSoftwareAssignmentChanging(row.assignment) ? 'changing' : undefined}>
      <Table.Cell>
        <RouterLink to={scopedHref(`/servers/${row.server.id}/summary`)}><strong>{serverDisplayName(row.server)}</strong></RouterLink>
        {showSite && <Text color="fg.muted" fontSize="xs">{siteName}</Text>}
      </Table.Cell>
      <Table.Cell>{softwareAssignmentSummary(row.assignment)}</Table.Cell>
      <Table.Cell><Badge variant="subtle" colorPalette={assignmentStatePalette(row.assignment.state)}>{assignmentStateLabel(row.assignment.state)}</Badge></Table.Cell>
      <Table.Cell>{row.assignment.lastAppliedAt ? formatDateTime(row.assignment.lastAppliedAt) : 'Not yet'}</Table.Cell>
      <Table.Cell><RouterLink to={scopedHref(`/workflows/${row.assignment.lastWorkflowId}`)} className="sw-mono">{row.assignment.lastWorkflowId}</RouterLink></Table.Cell>
      <Table.Cell><SoftwareDeploymentActions {...props} /></Table.Cell>
    </Table.Row>
  )
}

/** Mobile representation of one Software Assignment with equivalent facts and actions. */
function SoftwareDeploymentCard(props: DeploymentPresentationProps) {
  const { row, showSite, siteName, scopedHref } = props
  return (
    <ResourceCard
      title={<RouterLink to={scopedHref(`/servers/${row.server.id}/summary`)}>{serverDisplayName(row.server)}</RouterLink>}
      description={showSite ? siteName : row.server.addresses[0]}
      status={<Badge variant="subtle" colorPalette={assignmentStatePalette(row.assignment.state)}>{assignmentStateLabel(row.assignment.state)}</Badge>}
      actions={<SoftwareDeploymentActions {...props} />}
    >
      <ResourceCardField label="Configuration">{softwareAssignmentSummary(row.assignment)}</ResourceCardField>
      <ResourceCardField label="Last applied">{row.assignment.lastAppliedAt ? formatDateTime(row.assignment.lastAppliedAt) : 'Not yet'}</ResourceCardField>
      <ResourceCardField label="Workflow"><RouterLink to={scopedHref(`/workflows/${row.assignment.lastWorkflowId}`)} className="sw-mono">{row.assignment.lastWorkflowId}</RouterLink></ResourceCardField>
    </ResourceCard>
  )
}
