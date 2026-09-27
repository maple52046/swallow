import { useState } from 'react'
import { Badge, Box, Button, Flex, Heading, HStack, Table, Text } from '@chakra-ui/react'
import {
  ArrowUpRight,
  Boxes,
  CircleCheckBig,
  LoaderCircle,
  Network,
  Plus,
  Settings,
  TriangleAlert,
} from 'lucide-react'
import { Link as RouterLink, useNavigate, useSearchParams } from 'react-router-dom'
import { platformLifecycleLabel, platformLifecycleStatus, platformUninstallDisabledReason } from '@/domain/platform/lifecycle'
import type { Platform, PlatformLifecycleState, PlatformType } from '@/domain/platform/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { SelectionToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { ResponsiveDataView, ResourceCard, ResourceCardField } from '@/presentation/components/ResponsiveDataView'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatRelative } from '@/shared/utils/time'
import { PlatformBulkActionDialog } from './PlatformBulkActionDialog'
import { usePlatforms } from './usePlatforms'
import type { PlatformBulkAction } from './usePlatformBulkActions'

/** Shareable URL filter values; the empty string represents the unfiltered state. */
type PlatformTypeFilter = PlatformType | ''
type PlatformStatusFilter = 'attention' | 'in_progress' | ''

/** Selection belongs to one Site/filter signature so hidden runtimes cannot remain actionable. */
interface PlatformSelectionState {
  filterKey: string
  ids: ReadonlySet<string>
}

const PLATFORM_TYPE_FILTERS: ReadonlyArray<{ value: PlatformTypeFilter; label: string }> = [
  { value: '', label: 'All types' },
  { value: 'kubernetes', label: 'Kubernetes' },
  { value: 'slurm', label: 'Slurm' },
]

const PLATFORM_STATUS_FILTERS: ReadonlyArray<{ value: PlatformStatusFilter; label: string }> = [
  { value: '', label: 'All states' },
  { value: 'attention', label: 'Needs attention' },
  { value: 'in_progress', label: 'In progress' },
]

/** Number of observed runtime members that do not map to a managed Server. */
function unmatchedMembers(platform: Platform): number {
  return Math.max(0, platform.sync.memberCount - platform.sync.matchedCount)
}

/**
 * Treats every explicit runtime problem as actionable without manufacturing a
 * combined health axis. The UI continues to show the contributing lifecycle,
 * connectivity, sync, and membership facts independently.
 */
function needsAttention(platform: Platform): boolean {
  const lifecycleFailed = platform.lifecycleState === 'deploy_failed' || platform.lifecycleState === 'uninstall_failed'
  const unreachable = platform.lifecycleState !== 'uninstalled' && !platform.integrationId
  return lifecycleFailed || unreachable || Boolean(platform.sync.lastError) || unmatchedMembers(platform) > 0
}

/** Groups non-terminal lifecycle states for filtering and activity treatment. */
function isLifecycleInProgress(state: PlatformLifecycleState): boolean {
  return state === 'deploying' || state === 'uninstalling'
}

function isLifecycleFailed(state: PlatformLifecycleState): boolean {
  return state === 'deploy_failed' || state === 'uninstall_failed'
}

/** Ranks active pipeline work first, then failures and runtime issues, while archived records sink. */
function platformPriority(platform: Platform): number {
  if (isLifecycleInProgress(platform.lifecycleState)) return 0
  if (isLifecycleFailed(platform.lifecycleState)) return 1
  if (needsAttention(platform)) return 2
  if (platform.lifecycleState === 'active') return 3
  if (platform.lifecycleState === 'registered') return 4
  return 5
}

/** Unknown URL type values fail safely to the unfiltered state. */
function parseTypeFilter(value: string | null): PlatformTypeFilter {
  return value === 'kubernetes' || value === 'slurm' ? value : ''
}

/** Unknown URL status values fail safely to the unfiltered state. */
function parseStatusFilter(value: string | null): PlatformStatusFilter {
  return value === 'attention' || value === 'in_progress' ? value : ''
}

function platformTypeLabel(type: PlatformType): string {
  return type === 'kubernetes' ? 'Kubernetes' : 'Slurm'
}

function platformOriginLabel(platform: Platform): string {
  return platform.origin === 'deployed' ? 'Swallow-deployed' : 'External record'
}

/** Exposes workflow navigation only while the durable lifecycle is operationally relevant. */
function exposesLifecycleWorkflow(platform: Platform): boolean {
  return Boolean(
    platform.lifecycleOperationId &&
      (isLifecycleInProgress(platform.lifecycleState) || isLifecycleFailed(platform.lifecycleState)),
  )
}

/**
 * Runtime identity shared by desktop rows and mobile cards.
 *
 * The canonical Platform name remains the navigation target while type and
 * origin provide the PaaS context without becoming additional status axes.
 */
function PlatformIdentity({ platform, href }: { platform: Platform; href: string }) {
  const TypeIcon = platform.type === 'kubernetes' ? Boxes : Network
  return (
    <div className="sw-platform-identity">
      <span className="sw-platform-type-icon" data-platform-type={platform.type} aria-hidden>
        <TypeIcon size={18} />
      </span>
      <span className="sw-platform-identity__copy">
        <RouterLink className="sw-platform-name" to={href}>{platform.name}</RouterLink>
        <span>{platformTypeLabel(platform.type)} · {platformOriginLabel(platform)}</span>
      </span>
    </div>
  )
}

/** Keeps lifecycle state and its durable workflow adjacent without implying workflow progress. */
function PlatformLifecycle({
  platform,
  workflowHref,
}: {
  platform: Platform
  workflowHref: string | null
}) {
  return (
    <div className="sw-platform-lifecycle">
      <div className="sw-platform-lifecycle__status">
        {isLifecycleInProgress(platform.lifecycleState) && (
          <LoaderCircle className="sw-platform-lifecycle__spinner" size={14} aria-hidden />
        )}
        <StatusBadge
          status={platformLifecycleStatus(platform.lifecycleState)}
          label={platformLifecycleLabel(platform.lifecycleState)}
        />
      </div>
      {workflowHref && (
        <RouterLink className="sw-platform-workflow-link" to={workflowHref}>
          View workflow
          <ArrowUpRight size={13} aria-hidden />
        </RouterLink>
      )}
    </div>
  )
}

/** Connectivity stays separate from lifecycle and explains unavailable runtime management. */
function PlatformConnectivity({ platform }: { platform: Platform }) {
  if (platform.lifecycleState === 'uninstalled') {
    return <Text as="span" color="fg.muted">Not applicable</Text>
  }
  if (!platform.integrationId) return <StatusBadge status="unreachable" label="Unreachable" />
  if (platform.sync.lastError) return <StatusBadge status="failed" label="Sync failed" />
  return <StatusBadge status="connected" label="Connected" />
}

/** Membership exposes both the observed total and any members Swallow cannot match. */
function PlatformMembership({ platform }: { platform: Platform }) {
  if (!platform.integrationId) return <Text as="span" color="fg.muted">Not available</Text>
  const unmatched = unmatchedMembers(platform)
  return (
    <div className="sw-platform-membership">
      <span>{platform.sync.matchedCount} / {platform.sync.memberCount}</span>
      {unmatched > 0 && (
        <Badge colorPalette="orange" variant="subtle">{unmatched} unmatched</Badge>
      )}
    </div>
  )
}

/**
 * Whole-Site control-plane summary.
 *
 * Facts intentionally ignore list filters so operators cannot mistake a search
 * result for the health of the complete Site runtime fleet.
 */
function PlatformFleetOverview({ platforms }: { platforms: readonly Platform[] }) {
  const attention = platforms.filter(needsAttention).length
  const active = platforms.filter((platform) => platform.lifecycleState === 'active').length
  const inProgress = platforms.filter((platform) => isLifecycleInProgress(platform.lifecycleState)).length
  const failed = platforms.filter((platform) => isLifecycleFailed(platform.lifecycleState)).length
  const unmatched = platforms.reduce((total, platform) => total + unmatchedMembers(platform), 0)
  const hasAttention = attention > 0
  const headline = hasAttention
    ? `${attention} platform${attention === 1 ? '' : 's'} need attention`
    : 'All platforms are operating normally'
  const detail = hasAttention
    ? 'Review lifecycle, connectivity, and membership signals below.'
    : inProgress > 0
      ? `${inProgress} lifecycle change${inProgress === 1 ? ' is' : 's are'} in progress.`
      : 'Runtime lifecycle, connectivity, and membership are clear.'
  const OverviewIcon = hasAttention ? TriangleAlert : CircleCheckBig
  const facts = [
    { label: 'Total', value: platforms.length },
    { label: 'Active', value: active },
    { label: 'In progress', value: inProgress },
    { label: 'Failed', value: failed },
    { label: 'Unmatched', value: unmatched },
  ]

  return (
    <Box
      as="section"
      className="sw-platform-fleet-overview"
      data-tone={hasAttention ? 'warning' : 'success'}
      aria-labelledby="platform-fleet-overview-title"
    >
      <div className="sw-platform-fleet-overview__lead">
        <span className="sw-platform-fleet-overview__icon" aria-hidden>
          <OverviewIcon size={20} />
        </span>
        <div>
          <Text className="sw-platform-eyebrow">PaaS control plane</Text>
          <Heading as="h2" id="platform-fleet-overview-title" size="lg">{headline}</Heading>
          <Text color="fg.muted">{detail}</Text>
        </div>
      </div>
      <Box as="dl" className="sw-platform-fleet-facts">
        {facts.map((fact) => (
          <Box as="div" key={fact.label}>
            <Text as="dt">{fact.label}</Text>
            <Text as="dd">{fact.value}</Text>
          </Box>
        ))}
      </Box>
    </Box>
  )
}

/** One mobile runtime card with the same status axes and commands as the desktop row. */
function PlatformRuntimeCard({
  platform,
  selected,
  platformHref,
  workflowHref,
  onToggle,
}: {
  platform: Platform
  selected: boolean
  platformHref: string
  workflowHref: string | null
  onToggle: () => void
}) {
  const lastSync = platform.sync.lastSucceededAt ? formatRelative(platform.sync.lastSucceededAt) : 'Never'
  const actionLabel = isLifecycleFailed(platform.lifecycleState) ? 'Review platform' : 'Open platform'

  return (
    <div className="sw-platform-runtime-card" data-attention={needsAttention(platform) || undefined}>
      <ResourceCard
        selected={selected}
        title={
          <HStack align="flex-start" gap="3">
            <Checkbox
              id={`select-platform-card-${platform.id}`}
              aria-label={`Select ${platform.name}`}
              checked={selected}
              onCheckedChange={onToggle}
            />
            <PlatformIdentity platform={platform} href={platformHref} />
          </HStack>
        }
        status={
          <StatusBadge
            status={platformLifecycleStatus(platform.lifecycleState)}
            label={platformLifecycleLabel(platform.lifecycleState)}
          />
        }
        actions={
          <>
            <Button asChild colorPalette="brand" size="sm">
              <RouterLink to={platformHref}>
                {actionLabel}
                <ArrowUpRight size={14} />
              </RouterLink>
            </Button>
            {workflowHref && (
              <Button asChild variant="outline" size="sm">
                <RouterLink to={workflowHref}>View workflow</RouterLink>
              </Button>
            )}
          </>
        }
      >
        <ResourceCardField label="Connectivity"><PlatformConnectivity platform={platform} /></ResourceCardField>
        <ResourceCardField label="Members"><PlatformMembership platform={platform} /></ResourceCardField>
        <ResourceCardField label="Last sync">{lastSync}</ResourceCardField>
        <ResourceCardField label="Origin">{platformOriginLabel(platform)}</ResourceCardField>
      </ResourceCard>
    </div>
  )
}

/**
 * PaaS runtime inventory with URL-owned discovery filters and local selection.
 *
 * The selection is keyed by the current Site and filter signature. Browser
 * navigation therefore cannot revive hidden targets, while bulk commands still
 * operate on the complete visible working set.
 */
export function PlatformsPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const { siteId, scopedHref } = useSiteScope()
  const { state, reload } = usePlatforms(siteId)
  const [selectionState, setSelectionState] = useState<PlatformSelectionState>({
    filterKey: '',
    ids: new Set(),
  })
  const [bulkAction, setBulkAction] = useState<PlatformBulkAction | null>(null)

  const query = searchParams.get('q') ?? ''
  const typeFilter = parseTypeFilter(searchParams.get('type'))
  const statusFilter = parseStatusFilter(searchParams.get('status'))
  const filterKey = `${siteId ?? ''}|${query}|${typeFilter}|${statusFilter}`
  const selected = selectionState.filterKey === filterKey ? selectionState.ids : new Set<string>()

  const allPlatforms = state.status === 'ready' ? state.platforms : []
  const normalizedQuery = query.trim().toLocaleLowerCase()
  const platforms = [...allPlatforms]
    .filter((platform) => !normalizedQuery || platform.name.toLocaleLowerCase().includes(normalizedQuery))
    .filter((platform) => !typeFilter || platform.type === typeFilter)
    .filter((platform) => {
      if (statusFilter === 'attention') return needsAttention(platform)
      if (statusFilter === 'in_progress') return isLifecycleInProgress(platform.lifecycleState)
      return true
    })
    .sort((left, right) => (
      platformPriority(left) - platformPriority(right) ||
      unmatchedMembers(right) - unmatchedMembers(left) ||
      left.name.localeCompare(right.name)
    ))

  const setQueryParam = (key: 'q' | 'type' | 'status', value: string, replace = false) => {
    const next = new URLSearchParams(searchParams)
    if (value) next.set(key, value)
    else next.delete(key)
    setSearchParams(next, { replace })
  }

  const clearFilters = () => {
    const next = new URLSearchParams(searchParams)
    next.delete('q')
    next.delete('type')
    next.delete('status')
    setSearchParams(next)
  }

  const updateSelection = (update: (current: ReadonlySet<string>) => ReadonlySet<string>) => {
    setSelectionState((previous) => ({
      filterKey,
      ids: update(previous.filterKey === filterKey ? previous.ids : new Set()),
    }))
  }

  const toggleOne = (id: string) => {
    updateSelection((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const visibleIds = platforms.map((platform) => platform.id)
  const allSelected = visibleIds.length > 0 && visibleIds.every((id) => selected.has(id))
  const someSelected = visibleIds.some((id) => selected.has(id))
  const setAllVisible = (checked: boolean) => {
    setSelectionState({ filterKey, ids: checked ? new Set(visibleIds) : new Set() })
  }
  const clearSelection = () => setSelectionState({ filterKey, ids: new Set() })

  const selectedPlatforms = platforms.filter((platform) => selected.has(platform.id))
  const uninstallEligible = selectedPlatforms.filter((platform) => !platformUninstallDisabledReason(platform))
  const uninstallSkipped = selectedPlatforms.filter((platform) => Boolean(platformUninstallDisabledReason(platform)))
  const uninstallDisabledReason = uninstallEligible.length === 0 ? 'No selected platform can be uninstalled.' : undefined

  const onBulkDone = () => {
    setBulkAction(null)
    clearSelection()
    reload()
  }

  return (
    <div className="operator-page sw-platforms-page">
      <PageHeader
        title="Platforms"
        subtitle="Deploy and operate Kubernetes and Slurm runtimes from one PaaS control plane."
        stackActionsOnMobile
        actions={
          <>
            <Button variant="outline" onClick={() => navigate(scopedHref('/platforms/settings'))}>
              <Settings size={16} />
              Settings
            </Button>
            <Button colorPalette="brand" onClick={() => navigate(scopedHref('/platforms/deploy'))}>
              <Plus size={16} />
              Deploy platform
            </Button>
          </>
        }
      />

      {state.status === 'loading' && <LoadingState rows={4} />}
      {state.status === 'error' && <ErrorState message={state.message} />}
      {state.status === 'ready' && allPlatforms.length === 0 && (
        <EmptyState
          title="No platforms"
          message="Deploy Kubernetes or Slurm to create the first runtime in this Site."
          action={{ label: 'Deploy platform', onClick: () => navigate(scopedHref('/platforms/deploy')) }}
        />
      )}
      {state.status === 'ready' && allPlatforms.length > 0 && (
        <>
          <PlatformFleetOverview platforms={allPlatforms} />

          <Box as="section" className="sw-platform-inventory" aria-labelledby="platform-inventory-title">
            <div className="sw-platform-inventory__heading">
              <div>
                <Text className="sw-platform-eyebrow">Runtime inventory</Text>
                <Heading as="h2" id="platform-inventory-title" size="lg">Platform runtimes</Heading>
              </div>
              <Text
                className="sw-platform-result-count"
                color="fg.muted"
                role="status"
                aria-live="polite"
                aria-atomic="true"
              >
                Showing {platforms.length} of {allPlatforms.length} platforms
              </Text>
            </div>

            <div className="sw-platform-filter-bar">
              <SearchInput
                value={query}
                onChange={(value) => setQueryParam('q', value, true)}
                placeholder="Search platforms"
                aria-label="Search platforms"
                maxW="24rem"
                size="md"
              />
              <Flex className="sw-platform-filter-groups" align="center" gap="2" wrap="wrap">
                <Flex as="div" className="sw-platform-filter-group" role="group" aria-label="Filter by platform type">
                  {PLATFORM_TYPE_FILTERS.map((filter) => (
                    <Button
                      key={filter.label}
                      className="sw-platform-filter-chip"
                      variant="plain"
                      size="sm"
                      aria-pressed={typeFilter === filter.value}
                      data-active={typeFilter === filter.value || undefined}
                      onClick={() => setQueryParam('type', filter.value)}
                    >
                      {filter.label}
                    </Button>
                  ))}
                </Flex>
                <Flex as="div" className="sw-platform-filter-group" role="group" aria-label="Filter by operational state">
                  {PLATFORM_STATUS_FILTERS.map((filter) => (
                    <Button
                      key={filter.label}
                      className="sw-platform-filter-chip"
                      variant="plain"
                      size="sm"
                      aria-pressed={statusFilter === filter.value}
                      data-active={statusFilter === filter.value || undefined}
                      onClick={() => setQueryParam('status', filter.value)}
                    >
                      {filter.value === 'attention' && <TriangleAlert size={14} />}
                      {filter.value === 'in_progress' && <LoaderCircle size={14} />}
                      {filter.label}
                    </Button>
                  ))}
                </Flex>
              </Flex>
            </div>

            <SelectionToolbar count={selected.size} onClear={clearSelection}>
              <Tooltip content={uninstallDisabledReason ?? 'Uninstall selected platforms'}>
                <span>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={Boolean(uninstallDisabledReason)}
                    onClick={() => setBulkAction('uninstall')}
                  >
                    Uninstall
                  </Button>
                </span>
              </Tooltip>
              <Button colorPalette="red" size="sm" onClick={() => setBulkAction('delete')}>Delete</Button>
            </SelectionToolbar>

            {platforms.length === 0 ? (
              <div className="sw-platform-filter-empty">
                <EmptyState
                  title="No matching platforms"
                  message="Adjust the search or operational filters to see other runtimes."
                  action={{ label: 'Clear filters', onClick: clearFilters }}
                />
              </div>
            ) : (
              <ResponsiveDataView
                desktop={
                  <StickyTableFrame>
                    <Table.Root className="sw-platform-table" size="sm" aria-label="Platforms">
                      <Table.Header>
                        <Table.Row>
                          <Table.ColumnHeader className="sw-cell-center sw-col-select" aria-label="Row selection">
                            <Checkbox
                              id="select-all-platforms"
                              aria-label="Select all platforms"
                              checked={allSelected ? true : someSelected ? 'indeterminate' : false}
                              onCheckedChange={setAllVisible}
                            />
                          </Table.ColumnHeader>
                          <Table.ColumnHeader>Platform</Table.ColumnHeader>
                          <Table.ColumnHeader>Lifecycle</Table.ColumnHeader>
                          <Table.ColumnHeader>Connectivity</Table.ColumnHeader>
                          <Table.ColumnHeader>Members</Table.ColumnHeader>
                          <Table.ColumnHeader>Last sync</Table.ColumnHeader>
                          <Table.ColumnHeader aria-label="Open platform" />
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {platforms.map((platform) => {
                          const platformHref = scopedHref(`/platforms/${platform.id}`)
                          const workflowHref = exposesLifecycleWorkflow(platform) && platform.lifecycleOperationId
                            ? scopedHref(`/workflows/${platform.lifecycleOperationId}`)
                            : null
                          const actionLabel = isLifecycleFailed(platform.lifecycleState) ? 'Review platform' : 'Open platform'
                          return (
                            <Table.Row
                              key={platform.id}
                              className="sw-platform-row"
                              data-selected={selected.has(platform.id) || undefined}
                              data-attention={needsAttention(platform) || undefined}
                            >
                              <Table.Cell className="sw-cell-center sw-col-select">
                                <Checkbox
                                  id={`select-platform-${platform.id}`}
                                  aria-label={`Select ${platform.name}`}
                                  checked={selected.has(platform.id)}
                                  onCheckedChange={() => toggleOne(platform.id)}
                                />
                              </Table.Cell>
                              <Table.Cell><PlatformIdentity platform={platform} href={platformHref} /></Table.Cell>
                              <Table.Cell><PlatformLifecycle platform={platform} workflowHref={workflowHref} /></Table.Cell>
                              <Table.Cell><PlatformConnectivity platform={platform} /></Table.Cell>
                              <Table.Cell><PlatformMembership platform={platform} /></Table.Cell>
                              <Table.Cell>
                                <Text as="span" whiteSpace="nowrap">
                                  {platform.sync.lastSucceededAt ? formatRelative(platform.sync.lastSucceededAt) : 'Never'}
                                </Text>
                              </Table.Cell>
                              <Table.Cell className="sw-platform-table__action">
                                <Button asChild variant={isLifecycleFailed(platform.lifecycleState) ? 'solid' : 'outline'} colorPalette={isLifecycleFailed(platform.lifecycleState) ? 'brand' : undefined} size="sm">
                                  <RouterLink to={platformHref}>
                                    {actionLabel}
                                    <ArrowUpRight size={14} />
                                  </RouterLink>
                                </Button>
                              </Table.Cell>
                            </Table.Row>
                          )
                        })}
                      </Table.Body>
                    </Table.Root>
                  </StickyTableFrame>
                }
                mobile={
                  <div className="sw-resource-card-list" aria-label="Platforms">
                    {platforms.map((platform) => {
                      const platformHref = scopedHref(`/platforms/${platform.id}`)
                      const workflowHref = exposesLifecycleWorkflow(platform) && platform.lifecycleOperationId
                        ? scopedHref(`/workflows/${platform.lifecycleOperationId}`)
                        : null
                      return (
                        <PlatformRuntimeCard
                          key={platform.id}
                          platform={platform}
                          selected={selected.has(platform.id)}
                          platformHref={platformHref}
                          workflowHref={workflowHref}
                          onToggle={() => toggleOne(platform.id)}
                        />
                      )
                    })}
                  </div>
                }
              />
            )}
          </Box>
        </>
      )}

      {bulkAction && (
        <PlatformBulkActionDialog
          action={bulkAction}
          targets={bulkAction === 'uninstall' ? uninstallEligible : selectedPlatforms}
          skipped={bulkAction === 'uninstall' ? uninstallSkipped : []}
          onClose={() => setBulkAction(null)}
          onDone={onBulkDone}
        />
      )}
    </div>
  )
}
