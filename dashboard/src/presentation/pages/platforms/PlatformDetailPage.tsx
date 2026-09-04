import { useCallback, useState } from 'react'
import { Alert, AlertVariant, Button, Flex, Label } from '@patternfly/react-core'
import { SyncAltIcon } from '@patternfly/react-icons'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { useNavigate, useParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { platformLifecycleLabel, platformLifecycleStatus } from '@/domain/platform/lifecycle'
import type { Platform, KubernetesTopology } from '@/domain/platform/types'
import { serverDisplayName, serverPrimaryAddress, type Server } from '@/domain/server/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { SectionHeader, StatStrip, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatDateTime, formatRelative } from '@/shared/utils/time'
import { PlatformLifecycleActions } from './PlatformLifecycleActions'
import { usePlatformDetail } from './usePlatformDetail'

/** Operator label for a deployment topology value. */
function topologyLabel(topology: KubernetesTopology): string {
  switch (topology) {
    case 'standalone':
      return 'Standalone'
    case 'multi-node':
      return 'Multi-node (non-HA)'
    case 'high-availability':
      return 'High availability'
  }
}

/**
 * Platform drill-down combining readiness, lifecycle progress, members, and related work.
 *
 * Active deployment/uninstall state is polled by the detail hook, while raw automation
 * output remains a secondary drill-down. Slurm never inherits Kubernetes terminology.
 */
export function PlatformDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { scopedHref } = useSiteScope()
  const { platforms } = useApp()
  const { showToast } = useToast()
  const state = usePlatformDetail(id)
  const [syncing, setSyncing] = useState(false)

  const sync = useCallback(async () => {
    if (!id) return
    setSyncing(true)
    try {
      const report = await platforms.syncPlatform(id)
      showToast({
        tone: report.error ? 'error' : 'success',
        title: report.error ? 'Sync failed' : 'Membership synced',
        description: report.error ?? `${report.matched} of ${report.members} members matched to Servers.`,
      })
      if (state.status === 'ready') state.data.reload()
    } catch (error) {
      showToast({
        tone: 'error',
        title: 'Sync failed',
        description: error instanceof Error ? error.message : 'Could not sync membership.',
      })
    } finally {
      setSyncing(false)
    }
  }, [platforms, id, showToast, state])

  if (state.status === 'loading') {
    return <><PageHeader title="Platform" /><LoadingState rows={6} /></>
  }
  if (state.status === 'not-found') {
    return <><PageHeader title="Platform" /><EmptyState title="Platform not found" message="This platform no longer exists." /></>
  }
  if (state.status === 'error') {
    return <><PageHeader title="Platform" /><ErrorState message={state.message} /></>
  }

  const { platform, members, operations, reload } = state.data
  const controllers = members.filter((server) => server.membership?.role === 'control-plane')
  const workers = members.filter((server) => server.membership?.role !== 'control-plane')
  const isKubernetes = platform.type === 'kubernetes'
  const intendedControllers = platform.deployment?.roleAssignments.filter(
    (assignment) => assignment.role === 'control-plane',
  ) ?? []
  const intendedWorkers = platform.deployment?.roleAssignments.filter(
    (assignment) => assignment.role === 'worker',
  ) ?? []
  const workloadCapable = platform.deployment?.roleAssignments.filter(
    (assignment) => assignment.role === 'worker' || assignment.runWorkloads,
  ) ?? []
  const workloadControllers = intendedControllers.filter((assignment) => assignment.runWorkloads)
  const lifecycleOperation = operations.find(
    (operation) => operation.id === platform.lifecycleOperationId,
  ) ?? operations.find((operation) => operation.kind === 'deploy-kubernetes')
  const targetServerIds = lifecycleOperation?.targetServerIds

  return (
    <div className="operator-page">
      <PageHeader
        title={platform.name}
        subtitle={`${platform.type} platform, GPU stack owned by ${platform.gpuStackOwner}, exporters managed by ${platform.exporterOwner}`}
        breadcrumbs={[
          { label: 'Platforms', href: scopedHref('/platforms') },
          { label: platform.name },
        ]}
        metadata={
          <Flex gap={{ default: 'gapSm' }} flexWrap={{ default: 'wrap' }}>
            <Label color={isKubernetes ? 'blue' : 'grey'}>{platform.type}</Label>
            <StatusBadge
              status={platformLifecycleStatus(platform.lifecycleState)}
              label={platformLifecycleLabel(platform.lifecycleState)}
            />
          </Flex>
        }
        actions={
          <>
            <Button
              variant="secondary"
              icon={<SyncAltIcon />}
              onClick={() => void sync()}
              isLoading={syncing}
              isDisabled={syncing || !platform.integrationId}
            >
              Sync now
            </Button>
            <PlatformLifecycleActions
              platform={platform}
              targetServerIds={targetServerIds}
              onRepairStarted={reload}
            />
          </>
        }
      />
      <LifecycleNotice
        platform={platform}
        onOpenOperation={(operationId) => navigate(scopedHref('/operations/' + operationId))}
      />
      {platform.sync.lastError && platform.lifecycleState !== 'uninstalled' && (
        <Alert variant={AlertVariant.danger} title="Membership sync is failing" isInline>
          {platform.sync.lastError}. The member list may be stale; last success{' '}
          {formatRelative(platform.sync.lastSucceededAt ?? undefined)}.
        </Alert>
      )}
      <StatStrip
        items={[
          { label: 'Lifecycle', value: platformLifecycleLabel(platform.lifecycleState) },
          ...(isKubernetes && platform.deployment
            ? [
                { label: 'Topology', value: topologyLabel(platform.deployment.topology) },
                {
                  label: 'Control-plane',
                  value: intendedControllers.length,
                  detail: workloadControllers.length > 0
                    ? workloadControllers.length + (workloadControllers.length === 1
                        ? ' also runs workloads'
                        : ' also run workloads')
                    : 'Dedicated control-plane',
                },
                {
                  label: 'Workload-capable',
                  value: workloadCapable.length,
                  detail: intendedWorkers.length === 0
                    ? 'No worker-only nodes'
                    : intendedWorkers.length + (intendedWorkers.length === 1
                        ? ' worker-only node'
                        : ' worker-only nodes'),
                },
              ]
            : [
                { label: isKubernetes ? 'Control-plane' : 'Managers', value: controllers.length },
                { label: isKubernetes ? 'Worker nodes' : 'Compute members', value: workers.length },
              ]),
          {
            label: 'Matched members',
            value: platform.sync.matchedCount,
            detail: `${platform.sync.memberCount} reported`,
            tone: platform.sync.matchedCount < platform.sync.memberCount ? 'warning' : 'neutral',
          },
          {
            label: 'Last synced',
            value: platform.sync.lastSucceededAt
              ? formatRelative(platform.sync.lastSucceededAt)
              : 'No data',
          },
        ]}
      />
      <section className="sw-section">
        <SectionHeader
          title="Members"
          description={
            isKubernetes
              ? 'Kubernetes membership correlated to Server projections.'
              : 'Platform membership correlated to Server projections.'
          }
        />
        <MemberTable
          members={members}
          isKubernetes={isKubernetes}
          deployment={platform.deployment}
          onSelect={(server) => navigate(scopedHref(`/servers/${server.id}`))}
        />
      </section>
      <section className="sw-section">
        <SectionHeader
          title="Related operations"
          description="Deployment, uninstall, and retry history for this Platform."
          actions={<Button variant="link" icon={<SyncAltIcon />} onClick={reload}>Refresh</Button>}
        />
        {operations.length === 0 ? (
          <EmptyState title="No operations" message="No automation has run against this platform." />
        ) : (
          <StickyTableFrame>
            <Table aria-label="Related operations" variant="compact">
              <Thead><Tr><Th>Intent</Th><Th>Kind</Th><Th>Status</Th><Th>Requested</Th></Tr></Thead>
              <Tbody>
                {operations.map((operation) => (
                  <Tr
                    key={operation.id}
                    isClickable
                    onRowClick={() => navigate(scopedHref(`/operations/${operation.id}`))}
                  >
                    <Td dataLabel="Intent">{operation.intent || operation.execution.playbook}</Td>
                    <Td dataLabel="Kind">{operation.kind}</Td>
                    <Td dataLabel="Status"><StatusBadge status={operation.execution.status} /></Td>
                    <Td dataLabel="Requested">{formatDateTime(operation.requestedAt)}</Td>
                  </Tr>
                ))}
              </Tbody>
            </Table>
          </StickyTableFrame>
        )}
      </section>
    </div>
  )
}

/**
 * Explains lifecycle in Platform language and keeps raw automation detail a secondary action.
 */
function LifecycleNotice({
  platform,
  onOpenOperation,
}: {
  platform: Platform
  onOpenOperation: (operationId: string) => void
}) {
  const operationId = platform.lifecycleOperationId
  const details = operationId ? (
    <Button
      variant="link"
      isInline
      onClick={() => onOpenOperation(operationId)}
    >
      View automation details
    </Button>
  ) : null

  switch (platform.lifecycleState) {
    case 'deploying':
      return (
        <Alert variant={AlertVariant.info} title="Platform deployment is running" isInline>
          Swallow is configuring {platform.name}. Lifecycle and membership update here automatically. {details}
        </Alert>
      )
    case 'deploy_failed':
      return (
        <Alert variant={AlertVariant.danger} title="Platform deployment failed" isInline>
          Hosts may contain partial k0s state. Review the automation details, then use
          Repair deployment to rerun the original configuration. {details}
        </Alert>
      )
    case 'uninstalling':
      return (
        <Alert variant={AlertVariant.warning} title="Platform uninstall is running" isInline>
          The Swallow record remains available while original deployment targets are cleaned. {details}
        </Alert>
      )
    case 'uninstall_failed':
      return (
        <Alert variant={AlertVariant.danger} title="Platform uninstall failed" isInline>
          Hosts may be in mixed states. Inspect the automation output before retrying. {details}
        </Alert>
      )
    case 'uninstalled':
      return <Alert variant={AlertVariant.info} title="Platform is uninstalled" isInline>k0s was removed from the original targets. This record remains until you delete it.</Alert>
    default:
      if (!platform.integrationId) {
        return <Alert variant={AlertVariant.info} title="Platform is not reachable" isInline>No platform integration is attached, so membership cannot be read.</Alert>
      }
      return null
  }
}

/** Member rows whose terminology stays neutral for Slurm. */
function MemberTable({
  members,
  isKubernetes,
  deployment,
  onSelect,
}: {
  members: Server[]
  isKubernetes: boolean
  deployment: Platform['deployment']
  onSelect: (server: Server) => void
}) {
  if (members.length === 0) {
    return <EmptyState title="No members" message="No Server currently reports membership in this platform." />
  }
  return (
    <StickyTableFrame>
      <Table aria-label="Platform members" variant="compact">
        <Thead>
          <Tr><Th>{isKubernetes ? 'Node' : 'Member'}</Th><Th>Role</Th><Th>State</Th><Th>Address</Th></Tr>
        </Thead>
        <Tbody>
          {members.map((server) => {
            const assignment = deployment?.roleAssignments.find(
              (candidate) => candidate.serverId === server.id,
            )
            const controllerRunsWorkloads = assignment?.role === 'control-plane' &&
              assignment.runWorkloads
            return (
              <Tr key={server.id} isClickable onRowClick={() => onSelect(server)}>
                <Td dataLabel={isKubernetes ? 'Node' : 'Member'}>
                  <strong>{server.membership?.nodeName || serverDisplayName(server)}</strong>
                </Td>
                <Td dataLabel="Role">
                  <Flex gap={{ default: 'gapSm' }} alignItems={{ default: 'alignItemsCenter' }}>
                    <StatusBadge
                      status={server.membership?.role === 'control-plane' ? 'info' : 'neutral'}
                      label={server.membership?.role || 'unknown'}
                    />
                    {controllerRunsWorkloads && <Label color="green">Runs workloads</Label>}
                  </Flex>
                </Td>
                <Td dataLabel="State">
                  <StatusBadge
                    status={server.membership?.state === 'ready' ? 'succeeded' : 'warning'}
                    label={server.membership?.state || 'unknown'}
                  />
                </Td>
                <Td dataLabel="Address" className="mono">{serverPrimaryAddress(server) ?? '-'}</Td>
              </Tr>
            )
          })}
        </Tbody>
      </Table>
    </StickyTableFrame>
  )
}
