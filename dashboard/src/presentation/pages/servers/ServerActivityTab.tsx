import { useCallback, useEffect, useState } from 'react'
import { Button, IconButton, Table, Text, VisuallyHidden } from '@chakra-ui/react'
import { Eye, RefreshCw, RotateCcw } from 'lucide-react'
import { Link } from 'react-router-dom'
import type { Operation } from '@/domain/operation/types'
import type { ProvisioningTask } from '@/domain/provisioning/types'
import type { ProviderEvents } from '@/domain/server/types'
import { useApp } from '@/di/AppProvider'
import { SectionHeader, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { formatDateTime } from '@/shared/utils/time'
import { ServerActionResultDialog } from './ServerActionResultDialog'
import { actionLabel } from './serverActions'
import {
  listStoredServerActionResults,
  SERVER_ACTION_RESULT_RECORDED_EVENT,
  type ServerActionRunResult,
} from './serverActionResults'
import { useServerDetailContext } from './useServerDetail'

type LoadState<T> = { status: 'loading' } | { status: 'error'; message: string } | { status: 'ready'; data: T }

/**
 * Composes the three truthful activity sources for one Server: live provider events, durable
 * automation Operations, and current-tab synchronous action diagnostics. Each source has its
 * own loading/error/empty state, so one unavailable source never blanks the whole tab.
 */
export function ServerActivityTab() {
  const { server } = useServerDetailContext()
  const { servers, operations } = useApp()
  const { scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [provider, setProvider] = useState<LoadState<ProviderEvents>>({ status: 'loading' })
  const [related, setRelated] = useState<LoadState<Operation[]>>({ status: 'loading' })
  const [tasks, setTasks] = useState<LoadState<ProvisioningTask[]>>({ status: 'loading' })
  const [sessionResults, setSessionResults] = useState<ServerActionRunResult[]>(() => listStoredServerActionResults(server.id))
  const [selectedResult, setSelectedResult] = useState<ServerActionRunResult | null>(null)
  const [retryingTaskId, setRetryingTaskId] = useState('')
  const [nonce, setNonce] = useState(0)
  const refresh = useCallback(() => {
    setProvider({ status: 'loading' })
    setRelated({ status: 'loading' })
    setTasks({ status: 'loading' })
    setNonce((value) => value + 1)
  }, [])

  useEffect(() => {
    const sync = () => setSessionResults(listStoredServerActionResults(server.id))
    window.addEventListener(SERVER_ACTION_RESULT_RECORDED_EVENT, sync)
    return () => window.removeEventListener(SERVER_ACTION_RESULT_RECORDED_EVENT, sync)
  }, [server.id])

  useEffect(() => {
    let cancelled = false
    servers.getProviderEvents(server.id, 50).then(
      (data) => {
        if (!cancelled) setProvider({ status: 'ready', data })
      },
      (error: Error) => {
        if (!cancelled) setProvider({ status: 'error', message: error.message })
      },
    )
    operations.listOperations({ serverId: server.id, page: 1, pageSize: 50 }).then(
      (data) => {
        if (!cancelled) setRelated({ status: 'ready', data: data.items })
      },
      (error: Error) => {
        if (!cancelled) setRelated({ status: 'error', message: error.message })
      },
    )
    servers.listProvisioningTasks(server.id).then(
      (data) => {
        if (!cancelled) setTasks({ status: 'ready', data })
      },
      (error: Error) => {
        if (!cancelled) setTasks({ status: 'error', message: error.message })
      },
    )

    return () => {
      cancelled = true
    }
  }, [nonce, operations, server.id, servers])

  const retryCleanup = async (taskId: string) => {
    setRetryingTaskId(taskId)
    try {
      await servers.retryProvisioningTask(taskId)
      showToast({ tone: 'success', title: 'Cleanup retry queued' })
      refresh()
    } catch (error) {
      showToast({ tone: 'error', title: 'Could not retry cleanup', description: error instanceof Error ? error.message : 'Unknown error' })
    } finally {
      setRetryingTaskId('')
    }
  }

  return (
    <div className="sw-activity-stack">
      <section className="sw-section">
        <SectionHeader
          title="Current browser session"
          description="Synchronous provider actions performed in this browser tab. Request IDs correlate failures with API server logs."
        />
        {sessionResults.length === 0 ? (
          <div className="sw-section-empty">No Server actions have been recorded in this browser tab.</div>
        ) : (
          <StickyTableFrame>
            <Table.Root size="sm" aria-label="Current browser session Server actions">
              <Table.Header>
                <Table.Row><Table.ColumnHeader>Time</Table.ColumnHeader><Table.ColumnHeader>Action</Table.ColumnHeader><Table.ColumnHeader>Result</Table.ColumnHeader><Table.ColumnHeader>Message</Table.ColumnHeader><Table.ColumnHeader>Request ID</Table.ColumnHeader><Table.ColumnHeader><VisuallyHidden>Details</VisuallyHidden></Table.ColumnHeader></Table.Row>
              </Table.Header>
              <Table.Body>
                {sessionResults.map((result) => {
                  const outcome = result.outcomes[0]
                  return (
                    <Table.Row key={`${result.completedAt}:${result.action}:${outcome.requestId ?? outcome.serverId}`}>
                      <Table.Cell>{formatDateTime(result.completedAt)}</Table.Cell>
                      <Table.Cell>{actionLabel(result.action)}</Table.Cell>
                      <Table.Cell><StatusBadge status={outcome.accepted ? 'succeeded' : 'failed'} label={outcome.accepted ? 'Accepted' : 'Failed'} /></Table.Cell>
                      <Table.Cell>{outcome.message ?? '-'}</Table.Cell>
                      <Table.Cell className="mono">{outcome.requestId ?? '-'}</Table.Cell>
                      <Table.Cell textAlign="end">
                        <Tooltip content={`View ${actionLabel(result.action)} details`}>
                          <IconButton
                            variant="ghost"
                            size="sm"
                            aria-label={`View ${actionLabel(result.action)} details`}
                            onClick={() => setSelectedResult(result)}
                          >
                            <Eye size={18} />
                          </IconButton>
                        </Tooltip>
                      </Table.Cell>
                    </Table.Row>
                  )
                })}
              </Table.Body>
            </Table.Root>
          </StickyTableFrame>
        )}
      </section>

      <section className="sw-section">
        <SectionHeader
          title="Provisioning tasks"
          description="Durable Swallow follow-up such as post-Release static IP cleanup. Retry resumes cleanup and never repeats Release."
        />
        {tasks.status === 'loading' && <div className="sw-section-empty">Loading provisioning tasks...</div>}
        {tasks.status === 'error' && (
          <div className="sw-activity-alert">
            <Alert status="warning" title="Provisioning tasks are unavailable">
              {tasks.message}
            </Alert>
          </div>
        )}
        {tasks.status === 'ready' && tasks.data.length === 0 && <div className="sw-section-empty">No durable provisioning tasks for this Server.</div>}
        {tasks.status === 'ready' && tasks.data.length > 0 && (
          <StickyTableFrame>
            <Table.Root size="sm" aria-label="Provisioning tasks">
              <Table.Header>
                <Table.Row><Table.ColumnHeader>Status</Table.ColumnHeader><Table.ColumnHeader>Task</Table.ColumnHeader><Table.ColumnHeader>Phase</Table.ColumnHeader><Table.ColumnHeader>Updated</Table.ColumnHeader><Table.ColumnHeader>Error</Table.ColumnHeader><Table.ColumnHeader>Request ID</Table.ColumnHeader><Table.ColumnHeader><VisuallyHidden>Actions</VisuallyHidden></Table.ColumnHeader></Table.Row>
              </Table.Header>
              <Table.Body>
                {tasks.data.map((task) => (
                  <Table.Row key={task.id}>
                    <Table.Cell><StatusBadge status={task.status} /></Table.Cell>
                    <Table.Cell>
                      Release network cleanup
                      <Text as="small" display="block" color="fg.muted" className="mono">{task.id}</Text>
                    </Table.Cell>
                    <Table.Cell>{task.phase.replaceAll('_', ' ')}</Table.Cell>
                    <Table.Cell>{formatDateTime(task.updatedAt)}</Table.Cell>
                    <Table.Cell>{task.error || '-'}</Table.Cell>
                    <Table.Cell className="mono">{task.requestId || '-'}</Table.Cell>
                    <Table.Cell textAlign="end">
                      {task.retryable && (
                        <Button variant="outline" size="sm" loading={retryingTaskId === task.id} disabled={Boolean(retryingTaskId)} onClick={() => void retryCleanup(task.id)}>
                          <RotateCcw size={16} />
                          Retry cleanup
                        </Button>
                      )}
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table.Root>
          </StickyTableFrame>
        )}
      </section>

      <section className="sw-section">
        <SectionHeader
          title="Provider events"
          description="Live machine history retained by the provisioner. This is not a complete Swallow audit log."
          actions={
            <Button variant="outline" onClick={refresh}>
              <RefreshCw size={16} />
              Refresh
            </Button>
          }
        />
        {provider.status === 'loading' && <div className="sw-section-empty">Loading provider events...</div>}
        {provider.status === 'error' && (
          <div className="sw-activity-alert">
            <Alert status="warning" title="Provider events are unavailable">
              {provider.message}
            </Alert>
          </div>
        )}
        {provider.status === 'ready' && !provider.data.supported && <div className="sw-section-empty">This provisioner does not expose machine events.</div>}
        {provider.status === 'ready' && provider.data.supported && provider.data.events.length === 0 && <div className="sw-section-empty">No provider events are retained for this Server.</div>}
        {provider.status === 'ready' && provider.data.events.length > 0 && (
          <StickyTableFrame>
            <Table.Root size="sm" aria-label="Provider events">
              <Table.Header>
                <Table.Row><Table.ColumnHeader>Time</Table.ColumnHeader><Table.ColumnHeader>Level</Table.ColumnHeader><Table.ColumnHeader>Type</Table.ColumnHeader><Table.ColumnHeader>Message</Table.ColumnHeader><Table.ColumnHeader>Actor</Table.ColumnHeader></Table.Row>
              </Table.Header>
              <Table.Body>
                {provider.data.events.map((event) => (
                  <Table.Row key={event.id}>
                    <Table.Cell>{formatDateTime(event.occurredAt)}</Table.Cell>
                    <Table.Cell><StatusBadge status={event.level} /></Table.Cell>
                    <Table.Cell>{event.type || '-'}</Table.Cell>
                    <Table.Cell>{event.message || '-'}</Table.Cell>
                    <Table.Cell>{event.actor || '-'}</Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table.Root>
          </StickyTableFrame>
        )}
      </section>

      <section className="sw-section">
        <SectionHeader title="Related Operations" description="Durable automation runs that include this Server. Open one for retained events and stdout." />
        {related.status === 'loading' && <div className="sw-section-empty">Loading related Operations...</div>}
        {related.status === 'error' && (
          <div className="sw-activity-alert">
            <Alert status="warning" title="Related Operations are unavailable">
              {related.message}
            </Alert>
          </div>
        )}
        {related.status === 'ready' && related.data.length === 0 && <div className="sw-section-empty">No durable Operations include this Server.</div>}
        {related.status === 'ready' && related.data.length > 0 && (
          <StickyTableFrame>
            <Table.Root size="sm" aria-label="Related Operations">
              <Table.Header>
                <Table.Row><Table.ColumnHeader>Status</Table.ColumnHeader><Table.ColumnHeader>Operation</Table.ColumnHeader><Table.ColumnHeader>Kind</Table.ColumnHeader><Table.ColumnHeader>Requested</Table.ColumnHeader><Table.ColumnHeader>Requested by</Table.ColumnHeader></Table.Row>
              </Table.Header>
              <Table.Body>
                {related.data.map((operation) => (
                  <Table.Row key={operation.id}>
                    <Table.Cell><StatusBadge status={operation.execution.status} /></Table.Cell>
                    <Table.Cell>
                      <Link to={scopedHref(`/workflows/${operation.id}`)}>{operation.intent || operation.execution.playbook}</Link>
                      <Text as="small" display="block" color="fg.muted" className="mono">{operation.id}</Text>
                    </Table.Cell>
                    <Table.Cell>{operation.kind}</Table.Cell>
                    <Table.Cell>{formatDateTime(operation.requestedAt)}</Table.Cell>
                    <Table.Cell>{operation.requestedBy || 'system'}</Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table.Root>
          </StickyTableFrame>
        )}
      </section>

      {selectedResult && <ServerActionResultDialog result={selectedResult} onClose={() => setSelectedResult(null)} />}
    </div>
  )
}
