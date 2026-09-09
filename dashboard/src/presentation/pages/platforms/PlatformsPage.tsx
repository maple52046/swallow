import { Button, Label } from '@patternfly/react-core'
import { PlusIcon } from '@patternfly/react-icons'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { useNavigate } from 'react-router-dom'
import { platformLifecycleLabel, platformLifecycleStatus } from '@/domain/platform/lifecycle'
import type { Platform } from '@/domain/platform/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { StatStrip, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatRelative } from '@/shared/utils/time'
import { usePlatforms } from './usePlatforms'

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

/** Multi-platform inventory ordered by operation lifecycle and membership issues. */
export function PlatformsPage() {
  const navigate = useNavigate()
  const { siteId, scopedHref } = useSiteScope()
  const { state } = usePlatforms(siteId)
  const platforms = state.status === 'ready'
    ? [...state.platforms].sort(
      (a, b) => issueScore(b) - issueScore(a) || a.name.localeCompare(b.name),
    )
    : []
  const attention = platforms.filter(needsAttention).length
  const uninstalling = platforms.filter(
    (platform) => platform.lifecycleState === 'uninstalling',
  ).length
  const unmatched = platforms.reduce(
    (sum, platform) => sum + Math.max(0, platform.sync.memberCount - platform.sync.matchedCount),
    0,
  )

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
      {state.status === 'ready' && platforms.length === 0 && (
        <EmptyState
          title="No platforms"
          message="Deploy a Kubernetes or Slurm platform onto your Servers to get started."
          action={{
            label: 'Deploy platform',
            onClick: () => navigate(scopedHref('/platforms/deploy')),
          }}
        />
      )}
      {platforms.length > 0 && (
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
          <StickyTableFrame>
            <Table aria-label="Platforms" variant="compact" isStriped>
              <Thead>
                <Tr>
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
                      onRowClick={() => navigate(scopedHref(`/platforms/${platform.id}`))}
                    >
                      <Td dataLabel="Name">
                        <strong>{platform.name}</strong>
                        <small>{platform.origin === 'deployed' ? 'Swallow deployed' : 'Registered'}</small>
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
        </>
      )}
    </div>
  )
}
