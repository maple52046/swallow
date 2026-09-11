import { useState } from 'react'
import { Badge, Box, Button, Table } from '@chakra-ui/react'
import { Copy } from 'lucide-react'
import { copyText } from '@/presentation/utils/clipboard'
import { StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { actionLabel } from './serverActions'
import { failedServerActionOutcomes, type ServerActionRunResult } from './serverActionResults'

interface ServerActionResultDialogProps {
  result: ServerActionRunResult
  onClose: () => void
}

function formatResult(result: ServerActionRunResult): string {
  const failures = failedServerActionOutcomes(result)
  const lines = [
    `${actionLabel(result.action)}: ${result.succeeded} accepted, ${failures.length} failed`,
    `Completed: ${result.completedAt}`,
    '',
  ]
  result.outcomes.forEach((outcome) => {
    lines.push(`${outcome.serverName} (${outcome.serverId}): ${outcome.accepted ? 'Accepted' : 'Failed'}`)
    if (!outcome.accepted) {
      lines.push(`Code: ${outcome.code ?? '-'}`)
      lines.push(`HTTP status: ${outcome.httpStatus ?? '-'}`)
      lines.push(`Request ID: ${outcome.requestId ?? '-'}`)
      lines.push(`Message: ${outcome.message ?? '-'}`)
    }
    lines.push('')
  })
  return lines.join('\n').trimEnd()
}

/**
 * Shows every target outcome for a Server action and the request ID needed to find its
 * corresponding backend log entry. Copy exports the full result as plain text (best-effort
 * on insecure origins) so an operator can attach it to an incident.
 */
export function ServerActionResultDialog({ result, onClose }: ServerActionResultDialogProps) {
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle')
  const failures = failedServerActionOutcomes(result)
  const action = actionLabel(result.action)

  const copy = async () => {
    // copyText falls back to a legacy copy on insecure origins (LAN HTTP) where the async
    // Clipboard API is unavailable, so this works regardless of how the dashboard is served.
    setCopyState((await copyText(formatResult(result))) ? 'copied' : 'failed')
  }

  return (
    <Modal
      open
      onClose={onClose}
      size="xl"
      title={`${action} result`}
      description={`${result.succeeded} accepted, ${failures.length} failed across ${result.total} target${result.total === 1 ? '' : 's'}.`}
      footer={
        <>
          <Button variant="outline" onClick={() => void copy()}>
            <Copy size={16} />
            {copyState === 'copied' ? 'Copied' : 'Copy details'}
          </Button>
          <Button colorPalette="brand" onClick={onClose}>
            Done
          </Button>
        </>
      }
    >
      <div className="sw-action-result-content">
        {failures.length > 0 && (
          <Alert
            status={result.succeeded > 0 ? 'warning' : 'error'}
            title={result.succeeded > 0 ? `${action} was only partially accepted` : `${action} failed`}
          >
            Request IDs correlate these provider failures with the API server logs.
          </Alert>
        )}
        {copyState === 'failed' && (
          <Alert status="error" title="Could not copy result details">
            Clipboard access is unavailable in this browser.
          </Alert>
        )}
        <StickyTableFrame>
          <Table.Root size="sm" aria-label={`${action} target results`}>
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Server</Table.ColumnHeader>
                <Table.ColumnHeader>Result</Table.ColumnHeader>
                <Table.ColumnHeader>Error code</Table.ColumnHeader>
                <Table.ColumnHeader>HTTP</Table.ColumnHeader>
                <Table.ColumnHeader>Request ID</Table.ColumnHeader>
                <Table.ColumnHeader>Message</Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {result.outcomes.map((outcome) => (
                <Table.Row key={outcome.serverId}>
                  <Table.Cell>
                    <strong>{outcome.serverName}</strong>
                    <Box className="mono sw-action-result-server-id">{outcome.serverId}</Box>
                  </Table.Cell>
                  <Table.Cell>
                    <Badge colorPalette={outcome.accepted ? 'green' : 'red'} variant="subtle">
                      {outcome.accepted ? 'Accepted' : 'Failed'}
                    </Badge>
                  </Table.Cell>
                  <Table.Cell className="mono">{outcome.code ?? '-'}</Table.Cell>
                  <Table.Cell>{outcome.httpStatus ?? '-'}</Table.Cell>
                  <Table.Cell className="mono">{outcome.requestId ?? '-'}</Table.Cell>
                  <Table.Cell>{outcome.message ?? '-'}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      </div>
    </Modal>
  )
}
