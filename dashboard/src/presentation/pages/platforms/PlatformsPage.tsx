import { useState } from 'react'
import {
  Badge,
  Button,
  Checkbox,
  Dropdown,
  DropdownItem,
  DropdownList,
  Label,
  MenuToggle,
  ToolbarGroup,
  ToolbarItem,
  Tooltip,
} from '@patternfly/react-core'
import { PlusIcon } from '@patternfly/react-icons'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { ListFilter } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import {
  platformLifecycleLabel,
  platformLifecycleStatus,
  platformUninstallDisabledReason,
} from '@/domain/platform/lifecycle'
import type { Platform, PlatformLifecycleState, PlatformType } from '@/domain/platform/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { DataToolbar, StatStrip, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatRelative } from '@/shared/utils/time'
import { PlatformBulkActionDialog } from './PlatformBulkActionDialog'
import { usePlatforms } from './usePlatforms'
import type { PlatformBulkAction } from './usePlatformBulkActions'

function issueScore(platform: Platform): number {
  const lifecycleIssue =
    platform.lifecycleState === 'deploy_failed' || platform.lifecycleState === 'uninstall_failed'
      ? 6
      : platform.lifecycleState === 'deploying' || platform.lifecycleState === 'uninstalling'
        ? 2
        : 0
  const reachabilityIssue =
    platform.lifecycleState !== 'uninstalled' && !platform.integrationId ? 2 : 0
  return lifecycleIssue + reachabilityIssue + (platform.sync.lastError ? 2 : 0)
    + Math.max(0, platform.sync.memberCount - platform.sync.matchedCount)
}

function needsAttention(platform: Platform): boolean {
  return issueScore(platform) >= 4
}

/**
 * Coarse lifecycle ordering: in-progress deployments float to the top, uninstalled platforms
 * always sink to the bottom, and everything else sits between. Within a band the existing
 * issue-first, then name, ordering still applies.
 */
function lifecycleRank(state: PlatformLifecycleState): number {
  if (state === 'deploying') return 0
  if (state === 'uninstalled') return 2
  return 1
}

const PLATFORM_TYPES: { value: PlatformType; label: string }[] = [
  { value: 'kubernetes', label: 'Kubernetes' },
  { value: 'slurm', label: 'Slurm' },
]

/** Multi-platform inventory ordered by lifecycle band then membership issues. */
export function PlatformsPage() {
  const navigate = useNavigate()
  const { siteId, scopedHref } = useSiteScope()
  const { state, reload } = usePlatforms(siteId)
  const [typeFilter, setTypeFilter] = useState<Set<PlatformType>>(new Set())
  const [typeMenuOpen, setTypeMenuOpen] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [bulkAction, setBulkAction] = useState<PlatformBulkAction | null>(null)

  const allPlatforms = state.status === 'ready' ? state.platforms : []
  const platforms = allPlatforms
    .filter((platform) => typeFilter.size === 0 || typeFilter.has(platform.type))
    .sort(
      (a, b) =>
        lifecycleRank(a.lifecycleState) - lifecycleRank(b.lifecycleState) ||
        issueScore(b) - issueScore(a) ||
        a.name.localeCompare(b.name),
    )

  const attention = platforms.filter(needsAttention).length
  const uninstalling = platforms.filter(
    (platform) => platform.lifecycleState === 'uninstalling',
  ).length
  const unmatched = platforms.reduce(
    (sum, platform) => sum + Math.max(0, platform.sync.memberCount - platform.sync.matchedCount),
    0,
  )

  // Changing the type filter clears selection so a bulk action can never target a hidden row.
  const toggleType = (type: PlatformType) => {
    setSelected(new Set())
    setTypeFilter((prev) => {
      const next = new Set(prev)
      if (next.has(type)) next.delete(type)
      else next.add(type)
      return next
    })
  }

  const toggleOne = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const visibleIds = platforms.map((platform) => platform.id)
  const allSelected = visibleIds.length > 0 && visibleIds.every((id) => selected.has(id))
  const someSelected = visibleIds.some((id) => selected.has(id))
  const setAllVisible = (checked: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev)
      for (const id of visibleIds) {
        if (checked) next.add(id)
        else next.delete(id)
      }
      return next
    })
  const clearSelection = () => setSelected(new Set())

  const selectedPlatforms = platforms.filter((platform) => selected.has(platform.id))
  const uninstallEligible = selectedPlatforms.filter(
    (platform) => !platformUninstallDisabledReason(platform),
  )
  const uninstallSkipped = selectedPlatforms.filter((platform) =>
    Boolean(platformUninstallDisabledReason(platform)),
  )
  const uninstallDisabledReason = uninstallEligible.length === 0
    ? 'None of the selected platforms can be uninstalled.'
    : undefined

  const onBulkDone = () => {
    setBulkAction(null)
    clearSelection()
    reload()
  }

  return (
    <div className="operator-page">
      <PageHeader
        title="Platforms"
        subtitle="Multi-platform lifecycle, readiness, membership, and automation context."
        actions={
          <Button
            icon={<PlusIcon />}
            onClick={() => navigate(scopedHref('/platforms/deploy'))}
          >
            Deploy platform
          </Button>
        }
      />
      {state.status === 'loading' && <LoadingState rows={4} />}
      {state.status === 'error' && <ErrorState message={state.message} />}
      {state.status === 'ready' && allPlatforms.length === 0 && (
        <EmptyState
          title="No platforms"
          message="Deploy a Kubernetes or Slurm platform onto your Servers to get started."
          action={{
            label: 'Deploy platform',
            onClick: () => navigate(scopedHref('/platforms/deploy')),
          }}
        />
      )}
      {state.status === 'ready' && allPlatforms.length > 0 && (
        <>
          <StatStrip
            items={[
              { label: 'Platforms', value: platforms.length },
              { label: 'Needs attention', value: attention, tone: attention ? 'warning' : 'neutral' },
              { label: 'Uninstalling', value: uninstalling },
              {
                label: 'Unmatched members',
                value: unmatched,
                tone: unmatched ? 'warning' : 'neutral',
              },
            ]}
          />
          <DataToolbar variant="plain">
            <ToolbarItem>
              <Dropdown
                isOpen={typeMenuOpen}
                onOpenChange={setTypeMenuOpen}
                onSelect={() => undefined}
                toggle={(ref) => (
                  <MenuToggle
                    ref={ref}
                    icon={<ListFilter size={16} />}
                    isExpanded={typeMenuOpen}
                    onClick={() => setTypeMenuOpen((value) => !value)}
                    badge={typeFilter.size > 0 ? <Badge isRead>{typeFilter.size}</Badge> : undefined}
                  >
                    Type
                  </MenuToggle>
                )}
              >
                <DropdownList>
                  {PLATFORM_TYPES.map((type) => (
                    <DropdownItem
                      key={type.value}
                      hasCheckbox
                      isSelected={typeFilter.has(type.value)}
                      onClick={() => toggleType(type.value)}
                    >
                      {type.label}
                    </DropdownItem>
                  ))}
                </DropdownList>
              </Dropdown>
            </ToolbarItem>
            {selected.size > 0 && (
              <ToolbarGroup variant="action-group">
                <ToolbarItem>
                  <strong>{selected.size} selected</strong>
                </ToolbarItem>
                <ToolbarItem>
                  <Tooltip content={uninstallDisabledReason ?? 'Uninstall the selected platforms'}>
                    <Button
                      variant="secondary"
                      isAriaDisabled={Boolean(uninstallDisabledReason)}
                      onClick={() => {
                        if (!uninstallDisabledReason) setBulkAction('uninstall')
                      }}
                    >
                      Uninstall
                    </Button>
                  </Tooltip>
                </ToolbarItem>
                <ToolbarItem>
                  <Button variant="danger" onClick={() => setBulkAction('delete')}>
                    Delete
                  </Button>
                </ToolbarItem>
                <ToolbarItem>
                  <Button variant="link" onClick={clearSelection}>Clear selection</Button>
                </ToolbarItem>
              </ToolbarGroup>
            )}
          </DataToolbar>
          {platforms.length === 0 ? (
            <EmptyState
              title="No platforms match this filter"
              message="No platform matches the selected type. Clear the type filter to see all platforms."
            />
          ) : (
            <StickyTableFrame>
              <Table aria-label="Platforms" variant="compact" isStriped>
                <Thead>
                  <Tr>
                    <Th className="sw-cell-center sw-col-select" aria-label="Row selection">
                      <Checkbox
                        id="select-all-platforms"
                        aria-label="Select all platforms"
                        isChecked={allSelected ? true : someSelected ? null : false}
                        onChange={(_event, checked) => setAllVisible(checked)}
                      />
                    </Th>
                    <Th>Name</Th>
                    <Th>Type</Th>
                    <Th>Lifecycle</Th>
                    <Th>Connectivity</Th>
                    <Th>Members</Th>
                    <Th>Membership freshness</Th>
                  </Tr>
                </Thead>
                <Tbody>
                  {platforms.map((platform) => {
                    const unmanaged = Math.max(
                      0,
                      platform.sync.memberCount - platform.sync.matchedCount,
                    )
                    return (
                      <Tr
                        key={platform.id}
                        isClickable
                        isRowSelected={selected.has(platform.id)}
                        onRowClick={() => navigate(scopedHref(`/platforms/${platform.id}`))}
                      >
                        <Td
                          className="sw-cell-center sw-col-select"
                          onClick={(event) => event.stopPropagation()}
                        >
                          <Checkbox
                            id={`select-platform-${platform.id}`}
                            aria-label={`Select ${platform.name}`}
                            isChecked={selected.has(platform.id)}
                            onChange={() => toggleOne(platform.id)}
                          />
                        </Td>
                        <Td dataLabel="Name">
                          <strong>{platform.name}</strong>
                        </Td>
                        <Td dataLabel="Type">
                          <Label color={platform.type === 'kubernetes' ? 'blue' : 'grey'}>
                            {platform.type}
                          </Label>
                        </Td>
                        <Td dataLabel="Lifecycle">
                          <StatusBadge
                            status={platformLifecycleStatus(platform.lifecycleState)}
                            label={platformLifecycleLabel(platform.lifecycleState)}
                          />
                        </Td>
                        <Td dataLabel="Connectivity">
                          {platform.lifecycleState === 'uninstalled' ? (
                            '-'
                          ) : !platform.integrationId ? (
                            <StatusBadge status="pending" label="Not reachable" />
                          ) : platform.sync.lastError ? (
                            <StatusBadge status="failed" label="Sync failing" />
                          ) : (
                            <StatusBadge status="ready" label="Connected" />
                          )}
                        </Td>
                        <Td dataLabel="Members">
                          {platform.integrationId ? (
                            <>
                              {platform.sync.matchedCount}/{platform.sync.memberCount}
                              {unmanaged > 0 && <Label color="orange">{unmanaged} unmatched</Label>}
                            </>
                          ) : '-'}
                        </Td>
                        <Td dataLabel="Membership freshness">
                          {platform.sync.lastSucceededAt
                            ? formatRelative(platform.sync.lastSucceededAt)
                            : '-'}
                        </Td>
                      </Tr>
                    )
                  })}
                </Tbody>
              </Table>
            </StickyTableFrame>
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
