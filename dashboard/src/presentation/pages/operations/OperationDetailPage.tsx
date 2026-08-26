import { useCallback, useState, type ReactNode } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Box, Button, Card, Flex, Grid, Heading, Tabs, Text } from '@radix-ui/themes'
import { ReloadIcon } from '@radix-ui/react-icons'
import { useApp } from '@/di/AppProvider'
import { PageHeader } from '@/presentation/components/PageHeader'
import { LoadingState } from '@/presentation/components/LoadingState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { EmptyState } from '@/presentation/components/EmptyState'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { OperationProgress } from '@/presentation/components/OperationProgress'
import { useToast } from '@/presentation/components/radix/toast/toastContext'
import { formatDateTime } from '@/shared/utils/time'
import { isTerminalStatus } from '@/domain/operation/types'
import { useOperation } from './useOperation'
import { OperationLogs } from './OperationLogs'

/**
 * One operation: its status, targets, live task progress, logs, and a retry.
 *
 * Progress polls while the run is active (see useOperation) and stops on its own once the
 * run finishes. Retry is offered only for a finished operation and always creates a new
 * one, which this view then navigates to, so the original's record stays intact.
 */
export function OperationDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { operations } = useApp()
  const { showToast } = useToast()
  const state = useOperation(id)
  const [retrying, setRetrying] = useState(false)

  const onRetry = useCallback(async () => {
    if (!id) return
    setRetrying(true)
    try {
      const created = await operations.retryOperation(id)
      showToast({
        tone: 'success',
        title: 'Retry started',
        description: 'A new operation is running with the same targets.',
      })
      navigate(`/operations/${created.id}`)
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Could not start the retry.'
      showToast({ tone: 'error', title: 'Retry failed', description: message })
    } finally {
      setRetrying(false)
    }
  }, [id, operations, showToast, navigate])

  if (state.status === 'loading') {
    return (
      <Box>
        <PageHeader title="Operation" />
        <LoadingState rows={6} />
      </Box>
    )
  }
  if (state.status === 'not-found') {
    return (
      <Box>
        <PageHeader title="Operation" />
        <EmptyState title="Operation not found" message="This operation no longer exists." />
      </Box>
    )
  }
  if (state.status === 'error') {
    return (
      <Box>
        <PageHeader title="Operation" />
        <ErrorState message={state.message} />
      </Box>
    )
  }

  const { operation, events, reload } = state.data
  const terminal = isTerminalStatus(operation.execution.status)

  return (
    <Box>
      <PageHeader
        title={operation.intent || operation.execution.playbook}
        subtitle={`${operation.kind} · playbook ${operation.execution.playbook}`}
        actions={
          <Flex gap="2" align="center">
            <StatusBadge status={operation.execution.status} />
            <Button variant="soft" onClick={reload} aria-label="Refresh">
              <ReloadIcon />
              Refresh
            </Button>
            {terminal && (
              <Button onClick={onRetry} loading={retrying} disabled={retrying}>
                <ReloadIcon />
                Retry
              </Button>
            )}
          </Flex>
        }
      />

      <Grid columns={{ initial: '1', md: '2' }} gap="3" mb="4">
        <Card>
          <Heading size="3" mb="2">
            Details
          </Heading>
          <Flex direction="column" gap="1">
            <DetailRow label="Status">
              <StatusBadge status={operation.execution.status} />
            </DetailRow>
            {operation.execution.statusReason && (
              <DetailRow label="Reason">
                <Text size="2">{operation.execution.statusReason}</Text>
              </DetailRow>
            )}
            <DetailRow label="Targets">
              <Text size="2">{operation.targetServerIds.length} servers</Text>
            </DetailRow>
            {operation.clusterId && (
              <DetailRow label="Cluster">
                <Text size="2" style={{ fontFamily: 'monospace' }}>
                  {operation.clusterId}
                </Text>
              </DetailRow>
            )}
            {operation.retryOfOperationId && (
              <DetailRow label="Retry of">
                <Button
                  size="1"
                  variant="ghost"
                  onClick={() => navigate(`/operations/${operation.retryOfOperationId}`)}
                >
                  earlier operation
                </Button>
              </DetailRow>
            )}
          </Flex>
        </Card>

        <Card>
          <Heading size="3" mb="2">
            Timing
          </Heading>
          <Flex direction="column" gap="1">
            <DetailRow label="Requested by">
              <Text size="2">{operation.requestedBy || '—'}</Text>
            </DetailRow>
            <DetailRow label="Requested at">
              <Text size="2">{formatDateTime(operation.requestedAt)}</Text>
            </DetailRow>
            <DetailRow label="Started">
              <Text size="2">{formatDateTime(operation.execution.startedAt ?? undefined)}</Text>
            </DetailRow>
            <DetailRow label="Finished">
              <Text size="2">{formatDateTime(operation.execution.finishedAt ?? undefined)}</Text>
            </DetailRow>
          </Flex>
        </Card>
      </Grid>

      <Card>
        <Tabs.Root defaultValue="progress">
          <Tabs.List>
            <Tabs.Trigger value="progress">Progress</Tabs.Trigger>
            <Tabs.Trigger value="logs">Logs</Tabs.Trigger>
          </Tabs.List>
          <Box pt="3">
            <Tabs.Content value="progress">
              <OperationProgress events={events} running={!terminal} />
            </Tabs.Content>
            <Tabs.Content value="logs">
              <OperationLogs operationId={operation.id} />
            </Tabs.Content>
          </Box>
        </Tabs.Root>
      </Card>
    </Box>
  )
}

/** A label/value line in the detail cards; keeps the two columns aligned across cards. */
function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Flex justify="between" align="center" gap="3">
      <Text size="2" color="gray">
        {label}
      </Text>
      {children}
    </Flex>
  )
}
