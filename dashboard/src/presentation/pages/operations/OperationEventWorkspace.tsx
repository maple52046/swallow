import { useMemo, useState } from 'react'
import { Flex, FormSelect, FormSelectOption, Label, SearchInput } from '@patternfly/react-core'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import type { OperationEvents } from '@/domain/operation/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { StatusBadge } from '@/presentation/components/StatusBadge'

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
  return <div className="sw-event-workspace">
    <Flex gap={{ default: 'gapSm' }} flexWrap={{ default: 'wrap' }} alignItems={{ default: 'alignItemsCenter' }}><Label color="green">{events.okCount} ok</Label><Label color="blue">{events.changedCount} changed</Label><Label color={events.failedCount ? 'red' : 'grey'}>{events.failedCount} failed</Label><span>{filtered.length} of {events.events.length} events</span></Flex>
    <div className="sw-workspace-toolbar"><SearchInput value={query} onChange={(_event, value) => setQuery(value)} onClear={() => setQuery('')} placeholder="Search play, task, or host" aria-label="Search operation events" /><FormSelect value={status} onChange={(_event, value) => setStatus(value)} aria-label="Filter events by status"><FormSelectOption value="all" label="All results" />{statuses.map((value) => <FormSelectOption key={value} value={value} label={value} />)}</FormSelect></div>
    <StickyTableFrame><Table aria-label="Operation events" variant="compact"><Thead><Tr><Th>Play</Th><Th>Task</Th><Th>Host</Th><Th>Result</Th></Tr></Thead><Tbody>{filtered.map((event, index) => <Tr key={`${event.host}-${event.task}-${index}`}><Td dataLabel="Play">{event.play || 'No data'}</Td><Td dataLabel="Task">{event.task || 'No data'}</Td><Td dataLabel="Host" className="mono">{event.host || 'No data'}</Td><Td dataLabel="Result"><StatusBadge status={event.status} /> {event.changed && <Label color="blue" variant="outline">changed</Label>}</Td></Tr>)}</Tbody></Table></StickyTableFrame>
    {filtered.length === 0 && <div className="sw-section-empty">No events match these filters.</div>}
  </div>
}
