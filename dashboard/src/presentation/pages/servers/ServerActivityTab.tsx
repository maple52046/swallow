import { useCallback, useEffect, useState } from 'react'
import { Alert, AlertVariant, Button } from '@patternfly/react-core'
import { SyncAltIcon } from '@patternfly/react-icons'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { Link } from 'react-router-dom'
import type { Operation } from '@/domain/operation/types'
import type { ProviderEvents } from '@/domain/server/types'
import { useApp } from '@/di/AppProvider'
import { SectionHeader, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { StatusBadge } from '@/presentation/components/StatusBadge'
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

type LoadState<T> =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; data: T }

/**
 * Composes the three truthful activity sources for one Server: live provider events,
 * durable automation Operations, and current-tab synchronous action diagnostics.
 */
export function ServerActivityTab() {
  const { server } = useServerDetailContext()
  const { servers, operations } = useApp()
  const { scopedHref } = useSiteScope()
  const [provider, setProvider] = useState<LoadState<ProviderEvents>>({ status: 'loading' })
  const [related, setRelated] = useState<LoadState<Operation[]>>({ status: 'loading' })
  const [sessionResults, setSessionResults] = useState<ServerActionRunResult[]>(
    () => listStoredServerActionResults(server.id),
  )
  const [selectedResult, setSelectedResult] = useState<ServerActionRunResult | null>(null)
  const [nonce, setNonce] = useState(0)
  const refresh = useCallback(() => {
    setProvider({ status: 'loading' })
    setRelated({ status: 'loading' })
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
      (data) => { if (!cancelled) setProvider({ status: 'ready', data }) },
      (error: Error) => { if (!cancelled) setProvider({ status: 'error', message: error.message }) },
    )
    operations.listOperations({ serverId: server.id, page: 1, pageSize: 50 }).then(
      (data) => { if (!cancelled) setRelated({ status: 'ready', data: data.items }) },
      (error: Error) => { if (!cancelled) setRelated({ status: 'error', message: error.message }) },
    )

    return () => { cancelled = true }
  }, [nonce, operations, server.id, servers])

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
            <Table aria-label="Current browser session Server actions" variant="compact">
              <Thead><Tr><Th>Time</Th><Th>Action</Th><Th>Result</Th><Th>Message</Th><Th>Request ID</Th><Th screenReaderText="Details" /></Tr></Thead>
              <Tbody>{sessionResults.map((result) => {
                const outcome = result.outcomes[0]
                return <Tr key={`${result.completedAt}:${result.action}:${outcome.requestId ?? outcome.serverId}`}>
                  <Td dataLabel="Time">{formatDateTime(result.completedAt)}</Td>
                  <Td dataLabel="Action">{actionLabel(result.action)}</Td>
                  <Td dataLabel="Result"><StatusBadge status={outcome.accepted ? 'succeeded' : 'failed'} label={outcome.accepted ? 'Accepted' : 'Failed'} /></Td>
                  <Td dataLabel="Message">{outcome.message ?? '-'}</Td>
                  <Td dataLabel="Request ID" className="mono">{outcome.requestId ?? '-'}</Td>
                  <Td isActionCell><Button variant="link" isInline onClick={() => setSelectedResult(result)}>View details</Button></Td>
                </Tr>
              })}</Tbody>
            </Table>
          </StickyTableFrame>
        )}
      </section>

      <section className="sw-section">
        <SectionHeader
          title="Provider events"
          description="Live machine history retained by the provisioner. This is not a complete Swallow audit log."
          actions={<Button variant="secondary" icon={<SyncAltIcon />} onClick={refresh}>Refresh</Button>}
        />
        {provider.status === 'loading' && <div className="sw-section-empty">Loading provider events...</div>}
        {provider.status === 'error' && <div className="sw-activity-alert"><Alert variant={AlertVariant.warning} title="Provider events are unavailable" isInline>{provider.message}</Alert></div>}
        {provider.status === 'ready' && !provider.data.supported && <div className="sw-section-empty">This provisioner does not expose machine events.</div>}
        {provider.status === 'ready' && provider.data.supported && provider.data.events.length === 0 && <div className="sw-section-empty">No provider events are retained for this Server.</div>}
        {provider.status === 'ready' && provider.data.events.length > 0 && (
          <StickyTableFrame>
            <Table aria-label="Provider events" variant="compact">
              <Thead><Tr><Th>Time</Th><Th>Level</Th><Th>Type</Th><Th>Message</Th><Th>Actor</Th></Tr></Thead>
              <Tbody>{provider.data.events.map((event) => <Tr key={event.id}>
                <Td dataLabel="Time">{formatDateTime(event.occurredAt)}</Td>
                <Td dataLabel="Level"><StatusBadge status={event.level} /></Td>
                <Td dataLabel="Type">{event.type || '-'}</Td>
                <Td dataLabel="Message">{event.message || '-'}</Td>
                <Td dataLabel="Actor">{event.actor || '-'}</Td>
              </Tr>)}</Tbody>
            </Table>
          </StickyTableFrame>
        )}
      </section>

      <section className="sw-section">
        <SectionHeader title="Related Operations" description="Durable automation runs that include this Server. Open one for retained events and stdout." />
        {related.status === 'loading' && <div className="sw-section-empty">Loading related Operations...</div>}
        {related.status === 'error' && <div className="sw-activity-alert"><Alert variant={AlertVariant.warning} title="Related Operations are unavailable" isInline>{related.message}</Alert></div>}
        {related.status === 'ready' && related.data.length === 0 && <div className="sw-section-empty">No durable Operations include this Server.</div>}
        {related.status === 'ready' && related.data.length > 0 && (
          <StickyTableFrame>
            <Table aria-label="Related Operations" variant="compact">
              <Thead><Tr><Th>Status</Th><Th>Operation</Th><Th>Kind</Th><Th>Requested</Th><Th>Requested by</Th></Tr></Thead>
              <Tbody>{related.data.map((operation) => <Tr key={operation.id}>
                <Td dataLabel="Status"><StatusBadge status={operation.execution.status} /></Td>
                <Td dataLabel="Operation"><Link to={scopedHref(`/operations/${operation.id}`)}>{operation.intent || operation.execution.playbook}</Link><small className="mono">{operation.id}</small></Td>
                <Td dataLabel="Kind">{operation.kind}</Td>
                <Td dataLabel="Requested">{formatDateTime(operation.requestedAt)}</Td>
                <Td dataLabel="Requested by">{operation.requestedBy || 'system'}</Td>
              </Tr>)}</Tbody>
            </Table>
          </StickyTableFrame>
        )}
      </section>

      {selectedResult && <ServerActionResultDialog result={selectedResult} onClose={() => setSelectedResult(null)} />}
    </div>
  )
}
