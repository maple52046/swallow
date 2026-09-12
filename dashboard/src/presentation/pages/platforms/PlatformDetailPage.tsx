import { useCallback, useState } from 'react'
import { Badge, Button, HStack, Table, Tabs } from '@chakra-ui/react'
import { RefreshCw } from 'lucide-react'
import { useNavigate, useParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { isOrchestrationOperation, type Operation } from '@/domain/operation/types'
import { platformLifecycleLabel, platformLifecycleStatus } from '@/domain/platform/lifecycle'
import type { Platform } from '@/domain/platform/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { SectionHeader, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { ResourceCard, ResourceCardField, ResponsiveDataView } from '@/presentation/components/ResponsiveDataView'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
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
      showToast({ tone: 'error', title: 'Sync failed', description: error instanceof Error ? error.message : 'Could not sync membership.' })
    } finally {
      setSyncing(false)
    }
  }, [platforms, id, showToast, state])

  if (state.status === 'loading') {
    return (
      <>
        <PageHeader title="Platform" />
        <LoadingState rows={6} />
      </>
    )
  }
  if (state.status === 'not-found') {
    return (
      <>
        <PageHeader title="Platform" />
        <EmptyState title="Platform not found" message="This platform no longer exists." />
      </>
    )
  }
  if (state.status === 'error') {
    return (
      <>
        <PageHeader title="Platform" />
        <ErrorState message={state.message} />
      </>
    )
  }

  const { platform, members, loginNodes, operations, reload } = state.data
  const isKubernetes = platform.type === 'kubernetes'
  const lifecycleOperation =
    operations.find((operation) => operation.id === platform.lifecycleOperationId) ??
    operations.find((operation) => operation.kind === 'deploy-kubernetes' || operation.kind === 'configure-slurm')
  const targetServerIds = lifecycleOperation?.targetServerIds
  const openServer = (serverId: string) => navigate(scopedHref(`/servers/${serverId}`))

  return (
    <div className="operator-page">
      <PageHeader
        title={platform.name}
        breadcrumbs={[{ label: 'Platforms', href: scopedHref('/platforms') }]}
        stackActionsOnMobile
        metadata={
          <HStack gap="2" wrap="wrap">
            <Badge colorPalette={isKubernetes ? 'blue' : 'gray'} variant="subtle">
              {platform.type}
            </Badge>
            <StatusBadge status={platformLifecycleStatus(platform.lifecycleState)} label={platformLifecycleLabel(platform.lifecycleState)} />
          </HStack>
        }
        actions={
          <>
            <Button variant="outline" onClick={() => void sync()} loading={syncing} disabled={syncing || !platform.integrationId}>
              <RefreshCw size={16} />
              Sync now
            </Button>
            <PlatformLifecycleActions platform={platform} operation={lifecycleOperation} targetServerIds={targetServerIds} onRepairStarted={reload} />
          </>
        }
      />
      <Tabs.Root defaultValue="overview" aria-label="Platform details">
        <Tabs.List>
          <Tabs.Trigger value="overview">Overview</Tabs.Trigger>
          <Tabs.Trigger value="activity">Activity</Tabs.Trigger>
        </Tabs.List>
        <Tabs.Content value="overview">
          <div className="sw-platform-overview">
            <LifecycleNotice
              platform={platform}
              operation={lifecycleOperation}
              onOpenOperation={(operationId) => navigate(scopedHref('/workflows/' + operationId))}
            />
            {platform.sync.lastError && platform.lifecycleState !== 'uninstalled' && (
              <Alert status="error" title="Membership sync is failing">
                {platform.sync.lastError}. The member list may be stale; last success {formatRelative(platform.sync.lastSucceededAt ?? undefined)}.
              </Alert>
            )}
            {isKubernetes ? (
              <KubernetesPlatformView platform={platform} members={members} onSelect={(server) => openServer(server.id)} />
            ) : (
              <SlurmPlatformView platform={platform} members={members} loginNodes={loginNodes} onSelect={(server) => openServer(server.id)} />
            )}
            <PlatformConfiguration platform={platform} />
          </div>
        </Tabs.Content>
        <Tabs.Content value="activity">
          <section className="sw-section">
            <SectionHeader
              title="Related workflows"
              actions={
                <Button variant="plain" size="sm" onClick={reload}>
                  <RefreshCw size={16} />
                  Refresh
                </Button>
              }
            />
            {operations.length === 0 ? (
              <EmptyState title="No workflows" message="No workflow has run for this platform." />
            ) : (
              <ResponsiveDataView
                desktop={
                  <StickyTableFrame>
                    <Table.Root size="sm" aria-label="Related workflows">
                      <Table.Header>
                        <Table.Row><Table.ColumnHeader>Intent</Table.ColumnHeader><Table.ColumnHeader>Kind</Table.ColumnHeader><Table.ColumnHeader>Status</Table.ColumnHeader><Table.ColumnHeader>Requested</Table.ColumnHeader></Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {operations.map((operation) => (
                          <Table.Row key={operation.id} cursor="pointer" _hover={{ bg: 'bg.subtle' }} onClick={() => navigate(scopedHref(`/workflows/${operation.id}`))}>
                            <Table.Cell>{operation.intent || operation.execution.playbook}</Table.Cell>
                            <Table.Cell>{operation.kind}</Table.Cell>
                            <Table.Cell><StatusBadge status={operation.execution.status} /></Table.Cell>
                            <Table.Cell>{formatDateTime(operation.requestedAt)}</Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                    </Table.Root>
                  </StickyTableFrame>
                }
                mobile={
                  <div className="sw-resource-card-list">
                    {operations.map((operation) => (
                      <ResourceCard
                        key={operation.id}
                        title={operation.intent || operation.execution.playbook}
                        description={operation.kind}
                        status={<StatusBadge status={operation.execution.status} />}
                        actions={
                          <Button variant="outline" size="sm" onClick={() => navigate(scopedHref(`/workflows/${operation.id}`))}>
                            Open workflow
                          </Button>
                        }
                      >
                        <ResourceCardField label="Requested">{formatDateTime(operation.requestedAt)}</ResourceCardField>
                      </ResourceCard>
                    ))}
                  </div>
                }
              />
            )}
          </section>
        </Tabs.Content>
      </Tabs.Root>
    </div>
  )
}

/**
 * Secondary platform settings shown after operational state and membership.
 * Type already appears beside the page title, so this group keeps only the
 * configuration facts an operator may need while investigating the platform.
 */
function PlatformConfiguration({ platform }: { platform: Platform }) {
  const items = [
    { label: 'GPU stack', value: platform.gpuStackOwner },
    { label: 'Exporters', value: platform.exporterOwner },
    { label: 'Integration', value: platform.integrationId || 'Not connected' },
  ]

  return (
    <section className="sw-section">
      <SectionHeader title="Configuration" />
      <dl className="sw-platform-facts">
        {items.map((item) => (
          <div key={item.label}>
            <dt>{item.label}</dt>
            <dd>{item.value}</dd>
          </div>
        ))}
      </dl>
    </section>
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
    <Button variant="plain" size="sm" px="0" h="auto" colorPalette="brand" onClick={() => onOpenOperation(operationId)}>
      View automation details
    </Button>
  ) : null

  switch (platform.lifecycleState) {
    case 'deploying':
      return (
        <Alert status="info" title="Platform deployment is running">
          Swallow is configuring {platform.name}. Lifecycle and membership update here automatically. {details}
        </Alert>
      )
    case 'deploy_failed':
      return (
        <Alert status="error" title="Platform deployment failed">
          {/* pre-wrap keeps the executor's per-host failure lines legible; see sw-error-detail. */}
          <span className="sw-error-detail">{failureMessage}</span> {details}
        </Alert>
      )
    case 'uninstalling':
      return (
        <Alert status="warning" title="Platform uninstall is running">
          The Swallow record remains available while original deployment targets are cleaned. {details}
        </Alert>
      )
    case 'uninstall_failed':
      return (
        <Alert status="error" title="Platform uninstall failed">
          Hosts may be in mixed states. Inspect the automation output before retrying. {details}
        </Alert>
      )
    case 'uninstalled':
      return (
        <Alert status="info" title="Platform is uninstalled">
          The platform was removed from the original targets. This record remains until you delete it.
        </Alert>
      )
    default:
      if (!platform.integrationId) {
        return (
          <Alert status="info" title="Platform is not reachable">
            No platform integration is attached, so membership cannot be read.
          </Alert>
        )
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
  const failedStep = operation.steps?.find((step) => step.status === 'failed' || step.status === 'requires_attention')
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
