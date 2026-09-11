import { useCallback, useState } from 'react'
import { Alert, AlertVariant, Button, Flex, Label } from '@patternfly/react-core'
import { SyncAltIcon } from '@patternfly/react-icons'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { useNavigate, useParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { isOrchestrationOperation, type Operation } from '@/domain/operation/types'
import { platformLifecycleLabel, platformLifecycleStatus } from '@/domain/platform/lifecycle'
import type { Platform } from '@/domain/platform/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { SectionHeader, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatDateTime, formatRelative } from '@/shared/utils/time'
import { KubernetesPlatformView } from './KubernetesPlatformView'
import { PlatformLifecycleActions } from './PlatformLifecycleActions'
import { SlurmPlatformView } from './SlurmPlatformView'
import { usePlatformDetail } from './usePlatformDetail'

/**
 * Platform drill-down shell.
 *
 * Slurm and Kubernetes are structurally different, so this page is a shared shell (header,
 * lifecycle notice, sync, lifecycle actions, related operations) around a per-type body view:
 * KubernetesPlatformView or SlurmPlatformView. Type-specific summaries, member/node tables,
 * and any Slurm-native live reads live in those views, not here, so neither type inherits the
 * other's vocabulary or state model.
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

  const { platform, members, loginNodes, operations, reload } = state.data
  const isKubernetes = platform.type === 'kubernetes'
  const lifecycleOperation = operations.find(
    (operation) => operation.id === platform.lifecycleOperationId,
  ) ?? operations.find(
    (operation) => operation.kind === 'deploy-kubernetes' || operation.kind === 'configure-slurm',
  )
  const targetServerIds = lifecycleOperation?.targetServerIds
  const openServer = (serverId: string) => navigate(scopedHref(`/servers/${serverId}`))

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
              operation={lifecycleOperation}
              targetServerIds={targetServerIds}
              onRepairStarted={reload}
            />
          </>
        }
      />
      <LifecycleNotice
        platform={platform}
        operation={lifecycleOperation}
        onOpenOperation={(operationId) => navigate(scopedHref('/workflows/' + operationId))}
      />
      {platform.sync.lastError && platform.lifecycleState !== 'uninstalled' && (
        <Alert variant={AlertVariant.danger} title="Membership sync is failing" isInline>
          {platform.sync.lastError}. The member list may be stale; last success{' '}
          {formatRelative(platform.sync.lastSucceededAt ?? undefined)}.
        </Alert>
      )}

      {isKubernetes ? (
        <KubernetesPlatformView platform={platform} members={members} onSelect={(server) => openServer(server.id)} />
      ) : (
        <SlurmPlatformView platform={platform} members={members} loginNodes={loginNodes} onSelect={(server) => openServer(server.id)} />
      )}

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
                    onRowClick={() => navigate(scopedHref(`/workflows/${operation.id}`))}
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
 * Shared by both platform types; it reads only lifecycle state and the failed Step, neither of
 * which is type-specific.
 */
function LifecycleNotice({
  platform,
  operation,
  onOpenOperation,
}: {
  platform: Platform
  operation?: Operation
  onOpenOperation: (operationId: string) => void
}) {
  const operationId = platform.lifecycleOperationId
  const failureMessage = deploymentFailureMessage(operation)
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
          {/* pre-wrap keeps the executor's per-host failure lines legible; see sw-error-detail. */}
          <span className="sw-error-detail">{failureMessage}</span> {details}
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
      return <Alert variant={AlertVariant.info} title="Platform is uninstalled" isInline>The platform was removed from the original targets. This record remains until you delete it.</Alert>
    default:
      if (!platform.integrationId) {
        return <Alert variant={AlertVariant.info} title="Platform is not reachable" isInline>No platform integration is attached, so membership cannot be read.</Alert>
      }
      return null
  }
}

/**
 * Describes recovery from the durable Step that actually failed. A readiness failure before the
 * Ansible Step cannot have left partial platform state, while a started install Step still
 * requires the more cautious warning. Platform-type neutral: it names the install-platform Step
 * shared by both deployment kinds.
 */
function deploymentFailureMessage(operation?: Operation): string {
  if (!operation || !isOrchestrationOperation(operation)) {
    return 'Review the automation details, then use Repair deployment to rerun the original configuration.'
  }
  const failedStep = operation.steps?.find((step) =>
    step.status === 'failed' || step.status === 'requires_attention',
  )
  if (failedStep?.kind === 'wait-for-ssh') {
    return `${failedStep.error?.message ?? 'SSH readiness verification failed.'} This older Operation can only recheck SSH; it cannot repair a missing provider address or redeploy the operating system. Correct the provider network state first, or release and redeploy the affected Server.`
  }
  const installStep = operation.steps?.find((step) => step.id === 'install-platform')
  if (installStep && (installStep.status === 'pending' || installStep.status === 'skipped')) {
    const reason = failedStep?.error?.message ?? 'Machine preparation did not complete.'
    return `${reason} Platform installation did not start. Resolve the network or SSH readiness issue, then use Repair deployment to retry the failed Step.`
  }
  // A failed install-platform Step is the common Ansible failure. Lead with the executor's
  // enriched error (failing task, host, stderr/stdout tail) so the operator sees the cause on
  // the Platform page itself; the automation-details link and Stdout tab hold the full log.
  const reason = failedStep?.error?.message?.trim()
  const guidance = 'Hosts may contain partial platform state. Review the failed Step, then use Repair deployment to retry only unfinished work.'
  return reason ? `${reason}\n${guidance}` : guidance
}
