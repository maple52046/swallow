import { Badge, Flex, Table, Text } from '@radix-ui/themes'
import type { OperationEvents } from '@/domain/operation/types'
import { StatusBadge } from './StatusBadge'
import { EmptyState } from './EmptyState'

interface OperationProgressProps {
  /** Task events for a run, or null when the events read failed. */
  events: OperationEvents | null
  /** Whether the run is still active, so the empty state can say "waiting" vs "none". */
  running: boolean
}

/**
 * The shared task-level progress view for a swallow-owned run.
 *
 * Used by the operation detail page and reused on the cluster detail page so a deployment's
 * progress looks the same wherever it is shown (coding-style DRY gate). It shows a per-host,
 * per-task list in execution order with a count summary; a "changed" task is marked in
 * addition to its status, never by colour alone. Task output is intentionally absent — the
 * backend omits it because it can contain secrets.
 */
export function OperationProgress({ events, running }: OperationProgressProps) {
  if (!events || events.events.length === 0) {
    return (
      <EmptyState
        title={running ? 'Waiting for the first task' : 'No task events'}
        message={
          running
            ? 'The run has started; task results will appear here as it progresses.'
            : 'This run recorded no task events.'
        }
      />
    )
  }

  return (
    <Flex direction="column" gap="3">
      <Flex gap="2" align="center" wrap="wrap" aria-label="Task result summary">
        <Badge color="green" variant="soft">
          {events.okCount} ok
        </Badge>
        <Badge color="blue" variant="soft">
          {events.changedCount} changed
        </Badge>
        <Badge color={events.failedCount > 0 ? 'red' : 'gray'} variant="soft">
          {events.failedCount} failed
        </Badge>
      </Flex>

      <Table.Root variant="surface" size="1">
        <Table.Header>
          <Table.Row>
            <Table.ColumnHeaderCell>Play</Table.ColumnHeaderCell>
            <Table.ColumnHeaderCell>Task</Table.ColumnHeaderCell>
            <Table.ColumnHeaderCell>Host</Table.ColumnHeaderCell>
            <Table.ColumnHeaderCell>Result</Table.ColumnHeaderCell>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {events.events.map((event, index) => (
            <Table.Row key={`${event.host}-${index}`}>
              <Table.Cell>
                <Text size="1" color="gray">
                  {event.play || '—'}
                </Text>
              </Table.Cell>
              <Table.Cell>
                <Text size="2">{event.task || '—'}</Text>
              </Table.Cell>
              <Table.Cell>
                <Text size="1" style={{ fontFamily: 'var(--code-font-family, monospace)' }}>
                  {event.host}
                </Text>
              </Table.Cell>
              <Table.Cell>
                <Flex gap="1" align="center" wrap="nowrap">
                  <StatusBadge status={event.status} />
                  {event.changed && (
                    <Badge color="blue" variant="outline">
                      changed
                    </Badge>
                  )}
                </Flex>
              </Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </Table.Root>
    </Flex>
  )
}
