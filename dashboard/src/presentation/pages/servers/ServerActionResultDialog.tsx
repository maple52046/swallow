import { useState } from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  Label,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
} from '@patternfly/react-core'
import { CopyIcon } from '@patternfly/react-icons'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { actionLabel } from './serverActions'
import {
  failedServerActionOutcomes,
  type ServerActionRunResult,
} from './serverActionResults'

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
 * Shows every target outcome for a Server action and the request ID needed to find
 * its corresponding backend log entry.
 */
export function ServerActionResultDialog({
  result,
  onClose,
}: ServerActionResultDialogProps) {
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle')
  const failures = failedServerActionOutcomes(result)
  const action = actionLabel(result.action)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(formatResult(result))
      setCopyState('copied')
    } catch {
      setCopyState('failed')
    }
  }

  return (
    <Modal
      isOpen
      onClose={onClose}
      variant="large"
      aria-labelledby="server-action-result-title"
    >
      <ModalHeader
        title={`${action} result`}
        labelId="server-action-result-title"
        description={`${result.succeeded} accepted, ${failures.length} failed across ${result.total} target${result.total === 1 ? '' : 's'}.`}
      />
      <ModalBody>
        <div className="sw-action-result-content">
          {failures.length > 0 && (
            <Alert
              variant={result.succeeded > 0 ? AlertVariant.warning : AlertVariant.danger}
              title={result.succeeded > 0 ? `${action} was only partially accepted` : `${action} failed`}
              isInline
            >
              Request IDs correlate these provider failures with the API server logs.
            </Alert>
          )}
          {copyState === 'failed' && (
            <Alert variant={AlertVariant.danger} title="Could not copy result details" isInline>
              Clipboard access is unavailable in this browser.
            </Alert>
          )}
          <StickyTableFrame>
            <Table aria-label={`${action} target results`} variant="compact" gridBreakPoint="grid-md">
              <Thead>
                <Tr>
                  <Th>Server</Th>
                  <Th>Result</Th>
                  <Th>Error code</Th>
                  <Th>HTTP</Th>
                  <Th>Request ID</Th>
                  <Th>Message</Th>
                </Tr>
              </Thead>
              <Tbody>
                {result.outcomes.map((outcome) => (
                  <Tr key={outcome.serverId}>
                    <Td dataLabel="Server">
                      <strong>{outcome.serverName}</strong>
                      <div className="mono sw-action-result-server-id">{outcome.serverId}</div>
                    </Td>
                    <Td dataLabel="Result">
                      <Label color={outcome.accepted ? 'green' : 'red'} isCompact>
                        {outcome.accepted ? 'Accepted' : 'Failed'}
                      </Label>
                    </Td>
                    <Td dataLabel="Error code" className="mono">{outcome.code ?? '-'}</Td>
                    <Td dataLabel="HTTP">{outcome.httpStatus ?? '-'}</Td>
                    <Td dataLabel="Request ID" className="mono">{outcome.requestId ?? '-'}</Td>
                    <Td dataLabel="Message">{outcome.message ?? '-'}</Td>
                  </Tr>
                ))}
              </Tbody>
            </Table>
          </StickyTableFrame>
        </div>
      </ModalBody>
      <ModalFooter>
        <Button
          variant="secondary"
          icon={<CopyIcon />}
          onClick={() => void copy()}
        >
          {copyState === 'copied' ? 'Copied' : 'Copy details'}
        </Button>
        <Button variant="primary" onClick={onClose}>Done</Button>
      </ModalFooter>
    </Modal>
  )
}
