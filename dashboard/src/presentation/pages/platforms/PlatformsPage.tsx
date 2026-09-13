import { useState } from 'react'
import { Badge, Button, HStack, Menu, Portal, Table } from '@chakra-ui/react'
import { Boxes, ListFilter, LoaderCircle, Plus, Settings, TriangleAlert, Unplug } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { platformLifecycleLabel, platformLifecycleStatus, platformUninstallDisabledReason } from '@/domain/platform/lifecycle'
import type { Platform, PlatformLifecycleState, PlatformType } from '@/domain/platform/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DataToolbar, MetricGrid, SelectionToolbar, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { ResponsiveDataView, ResourceCard, ResourceCardField } from '@/presentation/components/ResponsiveDataView'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatRelative } from '@/shared/utils/time'
import { PlatformBulkActionDialog } from './PlatformBulkActionDialog'
import { usePlatforms } from './usePlatforms'
import type { PlatformBulkAction } from './usePlatformBulkActions'

/** Ranks current problems without collapsing lifecycle and connectivity into one status. */
function issueScore(platform: Platform): number {
  const lifecycleIssue = platform.lifecycleState === 'deploy_failed' || platform.lifecycleState === 'uninstall_failed'
    ? 6
    : platform.lifecycleState === 'deploying' || platform.lifecycleState === 'uninstalling' ? 2 : 0
  const reachabilityIssue = platform.lifecycleState !== 'uninstalled' && !platform.integrationId ? 2 : 0
  return lifecycleIssue + reachabilityIssue + (platform.sync.lastError ? 2 : 0) + Math.max(0, platform.sync.memberCount - platform.sync.matchedCount)
}

function needsAttention(platform: Platform): boolean {
  return issueScore(platform) >= 4
}

/** Keeps in-progress deployments visible and sinks uninstalled records. */
function lifecycleRank(state: PlatformLifecycleState): number {
  if (state === 'deploying') return 0
  if (state === 'uninstalled') return 2
  return 1
}

const PLATFORM_TYPES: { value: PlatformType; label: string }[] = [
  { value: 'kubernetes', label: 'Kubernetes' },
  { value: 'slurm', label: 'Slurm' },
]

/**
 * Platform inventory ordered by lifecycle activity and operational attention.
 *
 * Selection, filtering, and bulk lifecycle actions remain identical across the
 * desktop table and mobile cards. Connectivity remains separate from lifecycle.
 */
export function PlatformsPage() {
  const navigate = useNavigate()
  const { siteId, scopedHref } = useSiteScope()
  const { state, reload } = usePlatforms(siteId)
  const [typeFilter, setTypeFilter] = useState<Set<PlatformType>>(new Set())
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [bulkAction, setBulkAction] = useState<PlatformBulkAction | null>(null)

  const allPlatforms = state.status === 'ready' ? state.platforms : []
  const platforms = allPlatforms
    .filter((platform) => typeFilter.size === 0 || typeFilter.has(platform.type))
    .sort((a, b) => lifecycleRank(a.lifecycleState) - lifecycleRank(b.lifecycleState) || issueScore(b) - issueScore(a) || a.name.localeCompare(b.name))

  const attention = platforms.filter(needsAttention).length
  const uninstalling = platforms.filter((platform) => platform.lifecycleState === 'uninstalling').length
  const unmatched = platforms.reduce((sum, platform) => sum + Math.max(0, platform.sync.memberCount - platform.sync.matchedCount), 0)

  // Filtering clears selection so a hidden record can never receive a bulk action.
  const toggleType = (type: PlatformType) => {
    setSelected(new Set())
    setTypeFilter((previous) => {
      const next = new Set(previous)
      if (next.has(type)) next.delete(type)
      else next.add(type)
      return next
    })
  }

  const toggleOne = (id: string) => setSelected((previous) => {
    const next = new Set(previous)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  })

  const visibleIds = platforms.map((platform) => platform.id)
  const allSelected = visibleIds.length > 0 && visibleIds.every((id) => selected.has(id))
  const someSelected = visibleIds.some((id) => selected.has(id))
  const setAllVisible = (checked: boolean) => setSelected((previous) => {
    const next = new Set(previous)
    for (const id of visibleIds) {
      if (checked) next.add(id)
      else next.delete(id)
    }
    return next
  })
  const clearSelection = () => setSelected(new Set())

  const selectedPlatforms = platforms.filter((platform) => selected.has(platform.id))
  const uninstallEligible = selectedPlatforms.filter((platform) => !platformUninstallDisabledReason(platform))
  const uninstallSkipped = selectedPlatforms.filter((platform) => Boolean(platformUninstallDisabledReason(platform)))
  const uninstallDisabledReason = uninstallEligible.length === 0 ? 'No selected platform can be uninstalled.' : undefined

  const onBulkDone = () => {
    setBulkAction(null)
    clearSelection()
    reload()
  }

  const connectivity = (platform: Platform) => {
    if (platform.lifecycleState === 'uninstalled') return '-'
    if (!platform.integrationId) return <StatusBadge status="pending" label="Unreachable" />
    if (platform.sync.lastError) return <StatusBadge status="failed" label="Sync failed" />
    return <StatusBadge status="ready" label="Connected" />
  }

  return (
    <div className="operator-page">
      <PageHeader
        title="Platforms"
        actions={
          <HStack gap="2">
            <Button variant="outline" onClick={() => navigate(scopedHref('/platforms/settings'))}>
              <Settings size={16} />
              Settings
            </Button>
            <Button colorPalette="brand" onClick={() => navigate(scopedHref('/platforms/deploy'))}>
              <Plus size={16} />
              Deploy platform
            </Button>
          </HStack>
        }
      />

      {state.status === 'loading' && <LoadingState rows={4} />}
      {state.status === 'error' && <ErrorState message={state.message} />}
      {state.status === 'ready' && allPlatforms.length === 0 && (
        <EmptyState
          title="No platforms"
          message="Deploy Kubernetes or Slurm to get started."
          action={{ label: 'Deploy platform', onClick: () => navigate(scopedHref('/platforms/deploy')) }}
        />
      )}
      {state.status === 'ready' && allPlatforms.length > 0 && (
        <>
          <MetricGrid items={[
            { label: 'Platforms', value: platforms.length, icon: <Boxes size={16} /> },
            { label: 'Needs attention', value: attention, tone: attention ? 'warning' : 'neutral', icon: <TriangleAlert size={16} /> },
            { label: 'Uninstalling', value: uninstalling, icon: <LoaderCircle size={16} /> },
            { label: 'Unmatched members', value: unmatched, tone: unmatched ? 'warning' : 'neutral', icon: <Unplug size={16} /> },
          ]} />
          <DataToolbar variant="plain">
            <Menu.Root closeOnSelect={false}>
              <Menu.Trigger asChild>
                <Button variant="outline" size="sm">
                  <ListFilter size={16} />
                  Type {typeFilter.size > 0 && <Badge variant="subtle">{typeFilter.size}</Badge>}
                </Button>
              </Menu.Trigger>
              <Portal>
                <Menu.Positioner>
                  <Menu.Content>
                    {PLATFORM_TYPES.map((type) => (
                      <Menu.CheckboxItem key={type.value} value={type.value} checked={typeFilter.has(type.value)} onCheckedChange={() => toggleType(type.value)}>
                        {type.label}
                        <Menu.ItemIndicator />
                      </Menu.CheckboxItem>
                    ))}
                  </Menu.Content>
                </Menu.Positioner>
              </Portal>
            </Menu.Root>
            <SelectionToolbar count={selected.size} onClear={clearSelection}>
              <Tooltip content={uninstallDisabledReason ?? 'Uninstall selected platforms'}>
                <span>
                  <Button variant="outline" size="sm" disabled={Boolean(uninstallDisabledReason)} onClick={() => setBulkAction('uninstall')}>Uninstall</Button>
                </span>
              </Tooltip>
              <Button colorPalette="red" size="sm" onClick={() => setBulkAction('delete')}>Delete</Button>
            </SelectionToolbar>
          </DataToolbar>

          {platforms.length === 0 ? (
            <EmptyState title="No platforms" message="No results match this filter." />
          ) : (
            <ResponsiveDataView
              desktop={
                <StickyTableFrame>
                  <Table.Root size="sm" aria-label="Platforms">
                    <Table.Header>
                      <Table.Row>
                        <Table.ColumnHeader className="sw-cell-center sw-col-select" aria-label="Row selection">
                          <Checkbox
                            id="select-all-platforms"
                            aria-label="Select all platforms"
                            checked={allSelected ? true : someSelected ? 'indeterminate' : false}
                            onCheckedChange={(checked) => setAllVisible(checked)}
                          />
                        </Table.ColumnHeader>
                        <Table.ColumnHeader>Name</Table.ColumnHeader>
                        <Table.ColumnHeader>Type</Table.ColumnHeader>
                        <Table.ColumnHeader>Lifecycle</Table.ColumnHeader>
                        <Table.ColumnHeader>Connectivity</Table.ColumnHeader>
                        <Table.ColumnHeader>Members</Table.ColumnHeader>
                        <Table.ColumnHeader>Last sync</Table.ColumnHeader>
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {platforms.map((platform) => {
                        const unmanaged = Math.max(0, platform.sync.memberCount - platform.sync.matchedCount)
                        return (
                          <Table.Row key={platform.id} cursor="pointer" bg={selected.has(platform.id) ? 'brand.subtle' : undefined} onClick={() => navigate(scopedHref(`/platforms/${platform.id}`))}>
                            <Table.Cell className="sw-cell-center sw-col-select" onClick={(event) => event.stopPropagation()}>
                              <Checkbox id={`select-platform-${platform.id}`} aria-label={`Select ${platform.name}`} checked={selected.has(platform.id)} onCheckedChange={() => toggleOne(platform.id)} />
                            </Table.Cell>
                            <Table.Cell><strong>{platform.name}</strong></Table.Cell>
                            <Table.Cell><Badge colorPalette={platform.type === 'kubernetes' ? 'blue' : 'gray'} variant="subtle">{platform.type}</Badge></Table.Cell>
                            <Table.Cell><StatusBadge status={platformLifecycleStatus(platform.lifecycleState)} label={platformLifecycleLabel(platform.lifecycleState)} /></Table.Cell>
                            <Table.Cell>{connectivity(platform)}</Table.Cell>
                            <Table.Cell>
                              {platform.integrationId ? (
                                <HStack gap="2"><span>{platform.sync.matchedCount}/{platform.sync.memberCount}</span>{unmanaged > 0 && <Badge colorPalette="orange" variant="subtle">{unmanaged} unmatched</Badge>}</HStack>
                              ) : '-'}
                            </Table.Cell>
                            <Table.Cell>{platform.sync.lastSucceededAt ? formatRelative(platform.sync.lastSucceededAt) : '-'}</Table.Cell>
                          </Table.Row>
                        )
                      })}
                    </Table.Body>
                  </Table.Root>
                </StickyTableFrame>
              }
              mobile={
                <div className="sw-resource-card-list" aria-label="Platforms">
                  {platforms.map((platform) => (
                    <ResourceCard
                      key={platform.id}
                      selected={selected.has(platform.id)}
                      title={
                        <HStack gap="2">
                          <Checkbox id={`select-platform-card-${platform.id}`} aria-label={`Select ${platform.name}`} checked={selected.has(platform.id)} onCheckedChange={() => toggleOne(platform.id)} />
                          <span>{platform.name}</span>
                        </HStack>
                      }
                      status={<StatusBadge status={platformLifecycleStatus(platform.lifecycleState)} label={platformLifecycleLabel(platform.lifecycleState)} />}
                      actions={<Button variant="outline" size="sm" onClick={() => navigate(scopedHref(`/platforms/${platform.id}`))}>Open platform</Button>}
                    >
                      <ResourceCardField label="Type">{platform.type}</ResourceCardField>
                      <ResourceCardField label="Connectivity">{connectivity(platform)}</ResourceCardField>
                      <ResourceCardField label="Members">{platform.integrationId ? `${platform.sync.matchedCount}/${platform.sync.memberCount}` : '-'}</ResourceCardField>
                      <ResourceCardField label="Last sync">{platform.sync.lastSucceededAt ? formatRelative(platform.sync.lastSucceededAt) : '-'}</ResourceCardField>
                    </ResourceCard>
                  ))}
                </div>
              }
            />
          )}
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
