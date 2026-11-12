import { useCallback, useEffect, useState } from 'react'
import { Badge, Button, HStack, Stack, Table, Text } from '@chakra-ui/react'
import { ChartLine, ExternalLink, RefreshCw } from 'lucide-react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { operationStatus, type Operation } from '@/domain/operation/types'
import { platformMemberCounts } from '@/domain/platform/membership'
import { wantsPlatformGrafana } from '@/domain/platform/monitoring'
import { platformCurrentSpec } from '@/domain/platform/spec'
import type { Platform } from '@/domain/platform/types'
import type { Server } from '@/domain/server/types'
import { platformConfigurationSections, platformModuleUI, platformMonitoringEntries, type PlatformMonitoringServerStates } from '@/modules/registry'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { Pagination } from '@/presentation/components/Pagination'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { KeyValueGrid, SectionHeader, SectionSurface, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
import { Select } from '@/presentation/components/ui/select'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatDateTime, formatRelative } from '@/shared/utils/time'
import { PlatformDetailsCard } from './PlatformDetailsCard'
import { PlatformLifecycleBadge } from './PlatformLifecycleBadge'
import { describePlatformSpecState } from './platformSpecState'
import { platformTopologyLabel } from './platformDetailShared'
import { usePlatformMembers, type PlatformMembersState } from './usePlatformMembers'
import { usePlatformWorkspaceContext } from './PlatformWorkspaceContext'
import { RecentValidationRuns } from '@/presentation/components/validation/RecentValidationRuns'

const ACTIVITY_PAGE_SIZE_KEY = 'swallow.platform.activity.page-size'
const ACTIVITY_PAGE_SIZES = [5, 10, 15, 20] as const

function useMembersForRoute(platform: Platform): PlatformMembersState {
  const { siteId } = useSiteScope()
  return usePlatformMembers(platform, siteId)
}

/**
 * Summary-first Platform Overview. Durable posture remains visible if member correlation fails;
 * the failure is rendered next to its retry instead of becoming a misleading zero-member state.
 */
export function PlatformOverviewRoute() {
  const { platform } = usePlatformWorkspaceContext()
  const module = platformModuleUI(platform.type)
  const members = useMembersForRoute(platform)
  const counts = platformMemberCounts(platform)
  const specState = describePlatformSpecState(platform)
  const currentSpec = platformCurrentSpec(platform)
  const { scopedHref } = useSiteScope()

  return (
    <div className="sw-platform-workspace__content">
      <SectionSurface
        title="Runtime posture"
        description="The durable lifecycle, applied intent, membership correlation, and freshness of this runtime."
        className="sw-platform-posture"
      >
        <KeyValueGrid
          items={[
            { label: 'Lifecycle', value: <PlatformLifecycleBadge state={platform.lifecycleState} /> },
            { label: 'Topology', value: platformTopologyLabel(platform, module, 'External or legacy runtime') },
            { label: 'Members', value: `${counts.matched} matched · ${counts.total} total` },
            { label: 'Specification', value: specState.summary },
            { label: 'Applied generation', value: currentSpec ? `${platform.status.observedGeneration} of ${platform.generation}` : 'Not adopted' },
            { label: 'Last synchronized', value: platform.sync.lastSucceededAt ? formatRelative(platform.sync.lastSucceededAt) : 'No successful sync' },
          ]}
        />
        {specState.details.length > 0 && (
          <Stack mt="4" gap="1">
            {specState.details.map((detail) => <Text key={detail} color="fg.muted" fontSize="sm">{detail}</Text>)}
          </Stack>
        )}
      </SectionSurface>

      {members.status === 'loading' && <LoadingState rows={2} />}
      {members.status === 'error' && (
        <Alert status="warning" title="Platform members could not be loaded">
          {members.message}{members.requestId ? ' Request ID: ' + members.requestId + '.' : ''}
          <Button variant="plain" size="sm" onClick={members.reload}><RefreshCw size={14} />Retry</Button>
        </Alert>
      )}
      {members.status === 'ready' && (
        <>
          {members.data.warning && (
            <Alert status="warning" title="Some Platform Servers could not be resolved">
              {members.data.warning.message}
              {members.data.warning.requestId ? ' Request ID: ' + members.data.warning.requestId + '.' : ''}
              <Button variant="plain" size="sm" onClick={members.data.reload}><RefreshCw size={14} />Retry</Button>
            </Alert>
          )}
          {module && (
            <module.Access
              platform={platform}
              members={members.data.members}
              loginNodes={members.data.loginNodes}
              onSelectServer={() => undefined}
            />
          )}
        </>
      )}

      <SectionSurface
        title="Capabilities"
        description="Management surfaces provided by the installed Platform module."
        actions={
          <HStack gap="2" wrap="wrap">
            <Button asChild variant="outline" size="sm"><Link to={scopedHref(`/platforms/${encodeURIComponent(platform.id)}/configuration`, platform.siteId)}>Configuration</Link></Button>
            <Button asChild variant="outline" size="sm"><Link to={scopedHref(`/platforms/${encodeURIComponent(platform.id)}/monitoring`, platform.siteId)}>Monitoring</Link></Button>
          </HStack>
        }
      >
        <HStack gap="2" wrap="wrap">
          {(module?.capabilities ?? ['Lifecycle and membership']).map((capability) => (
            <Badge key={capability} variant="subtle" colorPalette={module?.badgePalette ?? 'gray'}>{capability}</Badge>
          ))}
        </HStack>
      </SectionSurface>
      {/* Renders nothing while the Validation area is hidden (an experimental feature). */}
      <RecentValidationRuns platformId={platform.id} />
    </div>
  )
}

/**
 * Applied and desired Platform configuration. Module facts and accelerator mutations are composed
 * here, while member lookup failure leaves durable spec/conditions visible.
 */
export function PlatformConfigurationRoute() {
  const { platform, reload } = usePlatformWorkspaceContext()
  const navigate = useNavigate()
  const module = platformModuleUI(platform.type)
  const sections = platformConfigurationSections(platform.type)
  const members = useMembersForRoute(platform)
  const { scopedHref } = useSiteScope()
  const resolvedMembers = members.status === 'ready' ? members.data.members : []

  return (
    <div className="sw-platform-workspace__content">
      <PlatformDetailsCard platform={platform} module={module} />
      <SectionSurface
        title="Member role assignments"
        description="Desired Server roles from the Platform spec. Live membership remains a separate observed axis."
      >
        {platform.spec?.members.length ? (
          <Stack gap="2">
            {platform.spec.members.map((member) => {
              const server = resolvedMembers.find((candidate) => candidate.id === member.serverId)
              return (
                <HStack key={member.serverId} justify="space-between" gap="3" wrap="wrap">
                  <Link to={scopedHref(`/servers/${encodeURIComponent(member.serverId)}`, platform.siteId)}>
                    {server?.status.hostname || member.serverId}
                  </Link>
                  <HStack gap="1" wrap="wrap">
                    {member.roles.map((role) => <Badge key={role} variant="subtle">{role}</Badge>)}
                  </HStack>
                </HStack>
              )
            })}
          </Stack>
        ) : (
          <Text color="fg.muted">No desired member assignments are recorded.</Text>
        )}
        {members.status === 'loading' && <Text mt="3" color="fg.muted">Resolving Server identities…</Text>}
        {members.status === 'error' && (
          <Alert status="warning" title="Server identities could not be resolved" mt="3">
            {members.message}{members.requestId ? ' Request ID: ' + members.requestId + '.' : ''}
            <Button variant="plain" size="sm" onClick={members.reload}>Retry</Button>
          </Alert>
        )}
        {members.status === 'ready' && members.data.warning && (
          <Alert status="warning" title="Some Server identities could not be resolved" mt="3">
            {members.data.warning.message}
            {members.data.warning.requestId ? ' Request ID: ' + members.data.warning.requestId + '.' : ''}
          </Alert>
        )}
      </SectionSurface>
      {sections.map((Section, index) => (
        <Section
          key={index}
          platform={platform}
          members={resolvedMembers}
          onChanged={() => reload()}
          onOpenWorkflow={(workflowId) => navigate(scopedHref('/workflows/' + workflowId, platform.siteId))}
        />
      ))}
    </div>
  )
}

/**
 * A monitoring component's per-Server breakdown (for example Omnistat's hardware counters on each
 * compute node), as the module projected it from the Platform record. The module's control for the
 * part (its on/off switch) sits under the title, so the setting is changed next to its outcome. Each
 * Server links to its page by its hostname once the members have loaded, else by its id; the state
 * is a text badge and the reason plain text, so nothing depends on colour. A summary stands in while
 * no Server has reported.
 */
function MonitoringServerStates({ states, platform, members, serverHref, onChanged, onOpenWorkflow }: {
  states: PlatformMonitoringServerStates
  platform: Platform
  members: Server[]
  serverHref: (serverId: string) => string
  onChanged: () => void
  onOpenWorkflow: (workflowId: string) => void
}) {
  const Control = states.Control
  return (
    <Stack gap="1" role="group" aria-label={states.title}>
      <Text fontWeight="medium" fontSize="sm">{states.title}</Text>
      {Control && <Control platform={platform} members={members} onChanged={onChanged} onOpenWorkflow={onOpenWorkflow} />}
      <Text color="fg.muted" fontSize="sm">{states.summary}</Text>
      {states.servers.length > 0 && (
        <ul className="sw-platform-monitoring-servers">
          {states.servers.map((server) => {
            const member = members.find((candidate) => candidate.id === server.serverId)
            return (
              <li key={server.serverId}>
                <Link to={serverHref(server.serverId)}>{member?.status.hostname || server.serverId}</Link>
                <StatusBadge status={server.state} label={server.statusLabel} />
                {server.reason && <Text as="span" color="fg.muted" fontSize="sm">{server.reason}</Text>}
              </li>
            )
          })}
        </ul>
      )}
    </Stack>
  )
}

/** Monitoring entries and installation state; charts remain in the dedicated Monitoring console. */
export function PlatformMonitoringRoute() {
  const { platform, reload } = usePlatformWorkspaceContext()
  const { scopedHref } = useSiteScope()
  const navigate = useNavigate()
  // Only to name the Servers of a per-Server breakdown; a failed or slow load falls back to ids.
  const members = useMembersForRoute(platform)
  const resolvedMembers = members.status === 'ready' ? members.data.members : []
  const chartsHref = scopedHref(
    `/monitoring?${new URLSearchParams({ chartScope: 'platform', chartScopeId: platform.id }).toString()}`,
    platform.siteId,
  )
  const grafana = platform.monitoringLinks.find((link) => link.name.toLowerCase().includes('grafana'))
  const otherLinks = platform.monitoringLinks.filter((link) => link !== grafana)
  const features = platformMonitoringEntries(platform.type, platform)

  return (
    <div className="sw-platform-workspace__content">
      <SectionSurface
        title="Monitoring entries"
        description="Open monitoring tools without embedding their dashboards into the control plane."
      >
        <div className="sw-platform-monitoring-entries">
          <div>
            <Text fontWeight="semibold">Swallow usage charts</Text>
            <Text color="fg.muted" fontSize="sm">Basic Server usage scoped to this Platform.</Text>
            <Button asChild variant="outline" size="sm" mt="3"><Link to={chartsHref}><ChartLine size={16} />Open monitoring</Link></Button>
          </div>
          <div>
            <Text fontWeight="semibold">Platform Grafana</Text>
            <Text color="fg.muted" fontSize="sm">
              {wantsPlatformGrafana(platform.spec) ? 'The Platform specification includes its own Grafana.' : 'No Platform Grafana is requested.'}
            </Text>
            {grafana && (
              <Button asChild variant="outline" size="sm" mt="3">
                <a href={grafana.url} target="_blank" rel="noreferrer" aria-label="Open Platform Grafana">Grafana<ExternalLink size={14} aria-hidden /></a>
              </Button>
            )}
          </div>
        </div>
      </SectionSurface>

      <SectionSurface title="Monitoring components" description="Recorded installation state from the Platform workflows.">
        {features.length === 0 ? (
          <Text color="fg.muted">No module-managed monitoring component is recorded.</Text>
        ) : (
          <Stack gap="3">
            {features.map((feature) => (
              <Stack key={feature.name} aria-label={feature.name} gap="2">
                <HStack justify="space-between" gap="3" wrap="wrap">
                  <div>
                    <Text fontWeight="semibold">{feature.name}</Text>
                    <Text color="fg.muted" fontSize="sm">{feature.detail}</Text>
                    {feature.serverId && (
                      <Link to={scopedHref(`/servers/${encodeURIComponent(feature.serverId)}`, platform.siteId)}>Open primary controller</Link>
                    )}
                  </div>
                  <HStack gap="2">
                    <StatusBadge status={feature.state} label={feature.statusLabel} />
                    {feature.workflowId && <Link to={scopedHref(`/workflows/${feature.workflowId}`, platform.siteId)}>View workflow</Link>}
                  </HStack>
                </HStack>
                {feature.warning && (
                  <Alert status="warning" title={feature.warning.title}>{feature.warning.message}</Alert>
                )}
                {feature.serverStates && (
                  <MonitoringServerStates
                    states={feature.serverStates}
                    platform={platform}
                    members={resolvedMembers}
                    serverHref={(serverId) => scopedHref(`/servers/${encodeURIComponent(serverId)}`, platform.siteId)}
                    onChanged={() => reload()}
                    onOpenWorkflow={(workflowId) => navigate(scopedHref('/workflows/' + workflowId, platform.siteId))}
                  />
                )}
              </Stack>
            ))}
          </Stack>
        )}
      </SectionSurface>

      {otherLinks.length > 0 && (
        <SectionSurface title="Other monitoring links">
          <HStack gap="2" wrap="wrap">
            {otherLinks.map((link) => (
              <Button key={link.name} asChild variant="outline" size="sm">
                <a href={link.url} target="_blank" rel="noreferrer">{link.name}<ExternalLink size={14} /></a>
              </Button>
            ))}
          </HStack>
        </SectionSurface>
      )}
    </div>
  )
}

interface ActivityState {
  loading: boolean
  page?: { items: Operation[]; total: number; page: number; pageSize: number }
  error?: DisplayedFailure
}

/** Server-paginated Platform Workflow history with URL-owned page and true detail links. */
export function PlatformActivityRoute() {
  const { platform } = usePlatformWorkspaceContext()
  const { operations } = useApp()
  const { scopedHref } = useSiteScope()
  const [searchParams, setSearchParams] = useSearchParams()
  const requestedPage = Math.max(1, Number(searchParams.get('page')) || 1)
  const [pageSize, setPageSize] = useState(() => {
    const stored = Number(localStorage.getItem(ACTIVITY_PAGE_SIZE_KEY))
    return ACTIVITY_PAGE_SIZES.includes(stored as typeof ACTIVITY_PAGE_SIZES[number]) ? stored : 10
  })
  const [revision, setRevision] = useState(0)
  const [state, setState] = useState<ActivityState>({ loading: true })

  useEffect(() => {
    let cancelled = false
    queueMicrotask(() => { if (!cancelled) setState({ loading: true }) })
    operations.listOperations({
      platformId: platform.id,
      siteId: platform.siteId,
      page: requestedPage,
      pageSize,
    }).then((page) => {
      if (cancelled) return
      const lastPage = Math.max(1, Math.ceil(page.total / pageSize))
      if (requestedPage > lastPage) {
        const next = new URLSearchParams(searchParams)
        if (lastPage === 1) next.delete('page')
        else next.set('page', String(lastPage))
        setSearchParams(next, { replace: true })
        return
      }
      setState({ loading: false, page })
    }).catch((error) => {
      if (!cancelled) setState({ loading: false, error: failureOf(error, 'Platform workflows could not be loaded.') })
    })
    return () => {
      cancelled = true
    }
  }, [operations, pageSize, platform.id, platform.siteId, requestedPage, revision, searchParams, setSearchParams])

  const setPage = useCallback((page: number) => {
    const next = new URLSearchParams(searchParams)
    if (page <= 1) next.delete('page')
    else next.set('page', String(page))
    setSearchParams(next)
  }, [searchParams, setSearchParams])

  const changePageSize = (next: number) => {
    localStorage.setItem(ACTIVITY_PAGE_SIZE_KEY, String(next))
    setPageSize(next)
    const params = new URLSearchParams(searchParams)
    params.delete('page')
    setSearchParams(params)
  }

  if (state.loading) return <LoadingState rows={8} />
  if (state.error) return <ErrorState message={state.error.message} requestId={state.error.requestId} onRetry={() => setRevision((value) => value + 1)} />
  const page = state.page!
  const totalPages = Math.max(1, Math.ceil(page.total / pageSize))
  const rangeStart = page.total === 0 ? 0 : (page.page - 1) * pageSize + 1
  const rangeEnd = Math.min(page.page * pageSize, page.total)

  return (
    <section className="sw-section">
      <SectionHeader
        title="Workflow activity"
        description={page.total ? `Showing ${rangeStart}–${rangeEnd} of ${page.total} workflows` : 'No Workflow has run for this Platform.'}
        actions={<Button variant="plain" size="sm" onClick={() => setRevision((value) => value + 1)}><RefreshCw size={16} />Refresh</Button>}
      />
      {page.items.length === 0 ? (
        <EmptyState title="No workflows" message="No Workflow has run for this Platform." />
      ) : (
        <>
          <ResponsiveDataView
            desktop={
              <StickyTableFrame>
                <Table.Root size="sm" aria-label="Platform workflows">
                  <Table.Header>
                    <Table.Row>
                      <Table.ColumnHeader>Workflow</Table.ColumnHeader>
                      <Table.ColumnHeader>Status</Table.ColumnHeader>
                      <Table.ColumnHeader>Kind</Table.ColumnHeader>
                      <Table.ColumnHeader>Requested</Table.ColumnHeader>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {page.items.map((operation) => (
                      <Table.Row key={operation.id}>
                        <Table.Cell>
                          <Link to={scopedHref(`/workflows/${operation.id}`, operation.siteId)}>
                            {operation.intent || operation.execution.playbook}
                          </Link>
                          <Text as="small" display="block" color="fg.muted" className="mono">{operation.id}</Text>
                        </Table.Cell>
                        <Table.Cell><StatusBadge status={operationStatus(operation)} /></Table.Cell>
                        <Table.Cell>{operation.kind}</Table.Cell>
                        <Table.Cell>{formatDateTime(operation.requestedAt)}</Table.Cell>
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table.Root>
              </StickyTableFrame>
            }
            mobile={
              <div className="sw-resource-card-list">
                {page.items.map((operation) => (
                  <ResourceCard
                    key={operation.id}
                    title={<Link to={scopedHref(`/workflows/${operation.id}`, operation.siteId)}>{operation.intent || operation.execution.playbook}</Link>}
                    description={operation.id}
                    status={<StatusBadge status={operationStatus(operation)} />}
                  >
                    <ResourceCardField label="Kind">{operation.kind}</ResourceCardField>
                    <ResourceCardField label="Requested">{formatDateTime(operation.requestedAt)}</ResourceCardField>
                  </ResourceCard>
                ))}
              </div>
            }
          />
          <div className="sw-activity-pagination">
            <Text color="fg.muted" fontSize="sm">Showing {rangeStart}–{rangeEnd} of {page.total}</Text>
            <div className="sw-activity-pagination__controls">
              <Select
                value={String(pageSize)}
                aria-label="Workflow rows per page"
                size="sm"
                width="8rem"
                onChange={(value) => changePageSize(Number(value))}
                options={ACTIVITY_PAGE_SIZES.map((size) => ({ value: String(size), label: `${size} rows` }))}
              />
              <Pagination total={totalPages} value={page.page} onChange={setPage} subject="workflows" />
            </div>
          </div>
        </>
      )}
    </section>
  )
}

/** Resolves one module-declared native child route without importing a module from presentation. */
export function PlatformModuleRoute({ route }: { route: string }) {
  const context = usePlatformWorkspaceContext()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const module = platformModuleUI(context.platform.type)
  const entry = module?.tabs.find((candidate) => candidate.value === route && candidate.available(context.platform))
  if (!entry) return <PlatformWorkspaceNotFound />
  if (entry.requiresMembers) return <MemberBackedModuleRoute entry={entry} />
  const Content = entry.Content
  return <Content platform={context.platform} members={[]} loginNodes={[]} onOpenServer={(serverId) => navigate(scopedHref('/servers/' + encodeURIComponent(serverId), context.platform.siteId))} />
}

function MemberBackedModuleRoute({ entry }: { entry: NonNullable<ReturnType<typeof platformModuleUI>>['tabs'][number] }) {
  const { platform } = usePlatformWorkspaceContext()
  const { scopedHref, siteId } = useSiteScope()
  const navigate = useNavigate()
  const members = usePlatformMembers(platform, siteId)
  if (members.status === 'loading') return <LoadingState rows={6} />
  if (members.status === 'error') return <ErrorState message={members.message} requestId={members.requestId} onRetry={members.reload} />
  const Content = entry.Content
  return (
    <>
      {members.data.warning && (
        <Alert status="warning" title="Some Platform Servers could not be resolved">
          {members.data.warning.message}
          {members.data.warning.requestId ? ' Request ID: ' + members.data.warning.requestId + '.' : ''}
          <Button variant="plain" size="sm" onClick={members.data.reload}>Retry</Button>
        </Alert>
      )}
      <Content
        platform={platform}
        members={members.data.members}
        loginNodes={members.data.loginNodes}
        onOpenServer={(serverId) => navigate(scopedHref('/servers/' + encodeURIComponent(serverId), platform.siteId))}
      />
    </>
  )
}

/** Unknown child routes remain inside the runtime workspace and preserve navigation context. */
export function PlatformWorkspaceNotFound() {
  return <EmptyState title="Workspace page not found" message="This Platform module does not provide the requested management page." />
}
