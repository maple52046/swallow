import { useMemo, useState } from 'react'
import { Badge, HStack, Table, Text } from '@chakra-ui/react'
import type { OperationEvents } from '@/domain/operation/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { NativeSelect } from '@/presentation/components/ui/native-select'
import { SearchInput } from '@/presentation/components/ui/search-input'

/** Filterable AWX-style task event workspace for one retained Operation run. */
export function OperationEventWorkspace({ events, running }: { events: OperationEvents | null; running: boolean }) {
  const [status, setStatus] = useState('all')
  const [query, setQuery] = useState('')
  const filtered = useMemo(() => {
    if (!events) return []
    const needle = query.trim().toLocaleLowerCase()
    return events.events.filter((event) => (status === 'all' || event.status === status) && (!needle || [event.play, event.task, event.host, event.status].some((value) => value.toLocaleLowerCase().includes(needle))))
  }, [events, query, status])
  if (!events || events.events.length === 0) return <EmptyState title={running ? 'Waiting for the first task' : 'No task events'} message={running ? 'Task results appear as the run progresses.' : 'This run recorded no task events.'} />
  const statuses = [...new Set(events.events.map((event) => event.status))].sort()
  return (
    <div className="sw-event-workspace">
      <HStack gap="2" wrap="wrap">
        <Badge colorPalette="green" variant="subtle">{events.okCount} ok</Badge>
        <Badge colorPalette="blue" variant="subtle">{events.changedCount} changed</Badge>
        <Badge colorPalette={events.failedCount ? 'red' : 'gray'} variant="subtle">{events.failedCount} failed</Badge>
        <Text as="span" color="fg.muted">{filtered.length} of {events.events.length} events</Text>
      </HStack>
      <div className="sw-workspace-toolbar">
        <SearchInput value={query} onChange={setQuery} placeholder="Search play, task, or host" aria-label="Search operation events" maxW="420px" />
        <NativeSelect value={status} onChange={setStatus} aria-label="Filter events by status">
          <option value="all">All results</option>
          {statuses.map((value) => <option key={value} value={value}>{value}</option>)}
        </NativeSelect>
      </div>
      <StickyTableFrame>
        <Table.Root size="sm" aria-label="Operation events">
          <Table.Header>
            <Table.Row><Table.ColumnHeader>Play</Table.ColumnHeader><Table.ColumnHeader>Task</Table.ColumnHeader><Table.ColumnHeader>Host</Table.ColumnHeader><Table.ColumnHeader>Result</Table.ColumnHeader></Table.Row>
          </Table.Header>
          <Table.Body>
            {filtered.map((event, index) => (
              <Table.Row key={`${event.host}-${event.task}-${index}`}>
                <Table.Cell>{event.play || 'No data'}</Table.Cell>
                <Table.Cell>{event.task || 'No data'}</Table.Cell>
                <Table.Cell className="mono">{event.host || 'No data'}</Table.Cell>
                <Table.Cell>
                  <HStack gap="2">
                    <StatusBadge status={event.status} />
                    {event.changed && <Badge colorPalette="blue" variant="outline">changed</Badge>}
                  </HStack>
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table.Root>
      </StickyTableFrame>
      {filtered.length === 0 && <div className="sw-section-empty">No events match these filters.</div>}
    </div>
  )
}
