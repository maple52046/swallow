import { useId, useState, type ReactNode } from 'react'
import { Box, Button, Stack, Table, Text } from '@chakra-ui/react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { Link as RouterLink } from 'react-router-dom'
import type { OperationNormalizedError, OperationResourceReference, OperationTaskDiagnostic } from '@/domain/operation/types'
import { KeyValueGrid, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { Alert } from '@/presentation/components/ui/alert'
import { formatDateTime } from '@/shared/utils/time'
import { affectedTargetText, failureClassText, firstLine, waitBudgetText } from './taskDiagnosticPresentation'

interface TaskFailureSummaryProps {
  /** The selected Step's latest normalized error, as the workflows contract returns it. */
  error: OperationNormalizedError
  /** The Step waits for the user (`requires_attention`) rather than having failed; the alert is a warning. */
  attention: boolean
  /**
   * The console route of an affected resource (Site-scoped by the caller), or `undefined` when the
   * console has no page for its kind; the target is then plain text.
   */
  targetHref?: (target: OperationResourceReference) => string | undefined
}

/**
 * The selected Step's failure on the Workflow detail page, in the order a user needs it: the user
 * layer first — what happened (the error's message), who is affected (the diagnostic's target, a
 * link when it has a page), and the next step (its remediation), with the code and stage beneath —
 * and the admin layer of its Task Diagnostic (failure class, operation, endpoint, cause, attempts,
 * elapsed time against the budget, last observation, provider events) behind a disclosure button.
 *
 * The disclosure is a real button with `aria-expanded` and `aria-controls` naming the details
 * region, and starts closed so the user layer is read first. An error without a diagnostic (every
 * error stored before diagnostics existed) shows only the user layer it has, with no disclosure.
 * All text comes from the API already redacted and is shown as is, never parsed; status is carried
 * by the alert's indicator and title text, never by colour alone.
 */
export function TaskFailureSummary({ error, attention, targetHref }: TaskFailureSummaryProps) {
  const detailsId = useId()
  const [open, setOpen] = useState(false)
  const diagnostic = error.diagnostic ?? undefined
  const target = diagnostic?.target
  const href = target ? targetHref?.(target) : undefined
  return (
    <Stack gap="2" className="sw-task-failure">
      <Alert status={attention ? 'warning' : 'error'} title={firstLine(error.message) || error.code}>
        <Stack gap="1">
          {target && (
            <Text>
              <strong>Affected: </strong>
              {href ? <RouterLink to={href}>{affectedTargetText(target)}</RouterLink> : affectedTargetText(target)}
            </Text>
          )}
          {diagnostic?.remediation && (
            <Text>
              <strong>Next step: </strong>
              {diagnostic.remediation}
            </Text>
          )}
          <Text fontSize="xs" color="fg.muted">
            Code <span className="mono">{error.code}</span>
            {error.stage ? ` · stage ${error.stage}` : ''}
          </Text>
        </Stack>
      </Alert>
      {diagnostic && (
        <Box>
          <Button
            variant="ghost"
            size="sm"
            aria-expanded={open}
            aria-controls={detailsId}
            onClick={() => setOpen((value) => !value)}
          >
            {open ? <ChevronDown size={16} aria-hidden /> : <ChevronRight size={16} aria-hidden />}
            {open ? 'Hide diagnostic details' : 'Show diagnostic details'}
          </Button>
          <Box id={detailsId} role="region" aria-label="Failure diagnostic details" hidden={!open} pt="2">
            {open && <DiagnosticDetails diagnostic={diagnostic} />}
          </Box>
        </Box>
      )}
    </Stack>
  )
}

/**
 * The admin layer of a Task Diagnostic: only the facts the Runner recorded are listed, so an absent
 * field is never shown as an empty or zero value. The failure class is spelled out with what it
 * means, the cause keeps its line breaks, and provider events are a labelled table that scrolls
 * inside its frame on a phone.
 */
function DiagnosticDetails({ diagnostic }: { diagnostic: OperationTaskDiagnostic }) {
  const failureClass = failureClassText(diagnostic.failureClass)
  const wait = waitBudgetText(diagnostic.elapsedSeconds, diagnostic.budgetSeconds)
  const items: Array<{ label: string; value: ReactNode }> = [
    { label: 'Failure class', value: failureClass.meaning ? `${failureClass.label} — ${failureClass.meaning}` : failureClass.label },
  ]
  if (diagnostic.operation) items.push({ label: 'Operation', value: diagnostic.operation })
  if (diagnostic.endpoint) {
    items.push({
      label: 'Endpoint',
      value: (
        <Text as="span" className="mono" overflowWrap="anywhere">
          {diagnostic.endpoint}
        </Text>
      ),
    })
  }
  if (diagnostic.attempts) items.push({ label: 'Attempts', value: String(diagnostic.attempts) })
  if (wait) items.push({ label: diagnostic.budgetSeconds ? 'Elapsed / budget' : 'Elapsed', value: wait })
  if (diagnostic.observation) {
    items.push({
      label: 'Last observation',
      value: diagnostic.observedAt ? `${diagnostic.observation} (${formatDateTime(diagnostic.observedAt)})` : diagnostic.observation,
    })
  }
  return (
    <Stack gap="3">
      <KeyValueGrid items={items} />
      {diagnostic.cause && (
        <Stack gap="1">
          <Text fontWeight="semibold" fontSize="sm">
            Cause
          </Text>
          <Text as="pre" className="sw-error-detail mono" fontSize="xs">
            {diagnostic.cause}
          </Text>
        </Stack>
      )}
      {diagnostic.providerEvents && diagnostic.providerEvents.length > 0 && (
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="Provider events around the failure">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Time</Table.ColumnHeader>
                <Table.ColumnHeader>Type</Table.ColumnHeader>
                <Table.ColumnHeader>Description</Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {/* Provider events carry no id and never reorder within one diagnostic (the API sends
                  them oldest first and the list is replaced whole), so the index is a stable key. */}
              {diagnostic.providerEvents.map((event, index) => (
                <Table.Row key={`${index}-${event.type}`}>
                  <Table.Cell>{event.at ? formatDateTime(event.at) : '-'}</Table.Cell>
                  <Table.Cell>{event.type || '-'}</Table.Cell>
                  <Table.Cell>{event.description || '-'}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      )}
    </Stack>
  )
}
