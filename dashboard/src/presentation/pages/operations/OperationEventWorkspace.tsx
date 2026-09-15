import { useMemo, useState } from 'react'
import { Badge, Box, HStack, Table, Text } from '@chakra-ui/react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import type { OperationEvents, TaskEvent } from '@/domain/operation/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { SearchInput } from '@/presentation/components/ui/search-input'

// How many failing hosts to name inline on a collapsed group before summarizing the rest. Enough
// to act on at a glance, bounded so a task that failed on hundreds of hosts stays one readable line.
const MAX_INLINE_FAILED_HOSTS = 5

/** One task and its per-host results. A task run across many hosts becomes one collapsible group. */
interface TaskGroup {
  key: string
  play: string
  task: string
  events: TaskEvent[]
}

interface GroupCounts {
  ok: number
  changed: number
  failed: number
  unreachable: number
  skipped: number
  total: number
}

function isErrorEvent(event: TaskEvent): boolean {
  return event.status === 'failed' || event.status === 'unreachable'
}

/**
 * Collapses the per-host results of the same task into one group, so a task that ran on hundreds of
 * hosts is one row instead of hundreds. Grouping is by contiguous run of the same play+task
 * (ansible emits a task's host results together), so two identically named tasks in different roles
 * stay separate.
 */
function groupEventsByTask(events: TaskEvent[]): TaskGroup[] {
  const groups: TaskGroup[] = []
  for (const event of events) {
    const last = groups[groups.length - 1]
    if (last && last.play === event.play && last.task === event.task) {
      last.events.push(event)
    } else {
      groups.push({ key: `${groups.length}:${event.play}:${event.task}`, play: event.play, task: event.task, events: [event] })
    }
  }
  return groups
}

function groupCounts(events: TaskEvent[]): GroupCounts {
  const counts: GroupCounts = { ok: 0, changed: 0, failed: 0, unreachable: 0, skipped: 0, total: events.length }
  for (const event of events) {
    if (event.status === 'ok') counts.ok++
    else if (event.status === 'failed') counts.failed++
    else if (event.status === 'unreachable') counts.unreachable++
    else if (event.status === 'skipped') counts.skipped++
    if (event.changed) counts.changed++
  }
  return counts
}

/** Worst status in a group, for the summary badge: any failure dominates, then unreachable, then ok. */
function groupStatus(counts: GroupCounts): string {
  if (counts.failed > 0) return 'failed'
  if (counts.unreachable > 0) return 'unreachable'
  if (counts.ok > 0) return 'ok'
  return 'skipped'
}

function failedHostNames(events: TaskEvent[]): string[] {
  return events.filter(isErrorEvent).map((event) => event.host || 'No data')
}

function eventMatchesQuery(event: TaskEvent, needle: string): boolean {
  if (!needle) return true
  return [event.play, event.task, event.host].some((value) => value.toLocaleLowerCase().includes(needle))
}

/**
 * Task-grouped, error-forward view of one run's Ansible events.
 *
 * The per-host fan-out is folded into one row per task so the list stays readable at hundreds of
 * nodes. A task with any failure surfaces its status, counts, and (while collapsed) the failing
 * host names inline; all-good tasks collapse by default and failed ones open with their failing
 * hosts sorted first. "Errors only" narrows to failures. Non-Ansible Steps carry no events, so this
 * renders the empty state for them unchanged.
 */
export function OperationEventWorkspace({ events, running }: { events: OperationEvents | null; running: boolean }) {
  const [query, setQuery] = useState('')
  const [errorsOnly, setErrorsOnly] = useState(false)
  const [overrides, setOverrides] = useState<Record<string, boolean>>({})

  const groups = useMemo(() => (events ? groupEventsByTask(events.events) : []), [events])
  const needle = query.trim().toLocaleLowerCase()
  const visibleGroups = useMemo(
    () =>
      groups.filter((group) => {
        const counts = groupCounts(group.events)
        if (errorsOnly && counts.failed + counts.unreachable === 0) return false
        if (!needle) return true
        if ([group.play, group.task].some((value) => value.toLocaleLowerCase().includes(needle))) return true
        return group.events.some((event) => event.host.toLocaleLowerCase().includes(needle))
      }),
    [groups, errorsOnly, needle],
  )

  if (!events || events.events.length === 0) {
    return (
      <EmptyState
        title={running ? 'Waiting for the first task' : 'No task events'}
        message={running ? 'Task results appear as the run progresses.' : 'This run recorded no task events.'}
      />
    )
  }

  const toggle = (key: string, currentlyCollapsed: boolean) =>
    setOverrides((previous) => ({ ...previous, [key]: !currentlyCollapsed }))
  const totalFailed = events.failedCount + events.unreachableCount

  return (
    <div className="sw-event-workspace">
      <HStack gap="2" wrap="wrap">
        <Badge colorPalette="green" variant="subtle">{events.okCount} ok</Badge>
        <Badge colorPalette="blue" variant="subtle">{events.changedCount} changed</Badge>
        <Badge colorPalette={events.failedCount ? 'red' : 'gray'} variant="subtle">{events.failedCount} failed</Badge>
        {events.unreachableCount > 0 && <Badge colorPalette="orange" variant="subtle">{events.unreachableCount} unreachable</Badge>}
        {events.skippedCount > 0 && <Badge colorPalette="gray" variant="subtle">{events.skippedCount} skipped</Badge>}
        <Text as="span" color="fg.muted">
          {visibleGroups.length} of {groups.length} tasks
        </Text>
      </HStack>
      <div className="sw-workspace-toolbar">
        <SearchInput value={query} onChange={setQuery} placeholder="Search play, task, or host" aria-label="Search operation events" maxW="420px" />
        <Checkbox checked={errorsOnly} onCheckedChange={setErrorsOnly} disabled={totalFailed === 0}>
          Errors only
        </Checkbox>
      </div>
      {visibleGroups.length === 0 ? (
        <div className="sw-section-empty">No tasks match these filters.</div>
      ) : (
        <div className="sw-event-groups">
          {visibleGroups.map((group) => (
            <TaskGroupRow
              key={group.key}
              group={group}
              needle={needle}
              errorsOnly={errorsOnly}
              override={overrides[group.key]}
              onToggle={toggle}
            />
          ))}
        </div>
      )}
    </div>
  )
}

/** One collapsible task group: a summary header plus, when expanded, its per-host results. */
function TaskGroupRow({
  group,
  needle,
  errorsOnly,
  override,
  onToggle,
}: {
  group: TaskGroup
  needle: string
  errorsOnly: boolean
  override: boolean | undefined
  onToggle: (key: string, collapsed: boolean) => void
}) {
  const counts = groupCounts(group.events)
  const hasError = counts.failed + counts.unreachable > 0
  // Default: collapse a clean task, open one with failures. An explicit user toggle overrides it.
  const collapsed = override ?? !hasError
  const failed = failedHostNames(group.events)
  const rows = useMemo(() => {
    const filtered = group.events.filter(
      (event) => (!errorsOnly || isErrorEvent(event)) && eventMatchesQuery(event, needle),
    )
    // Errors first so a failure is visible at the top of an expanded task; sort is stable.
    return [...filtered].sort((a, b) => Number(isErrorEvent(b)) - Number(isErrorEvent(a)))
  }, [group.events, errorsOnly, needle])

  return (
    <Box className="sw-event-group" data-error={hasError || undefined}>
      <button
        type="button"
        className="sw-event-group__header"
        aria-expanded={!collapsed}
        onClick={() => onToggle(group.key, collapsed)}
      >
        {collapsed ? <ChevronRight size={16} /> : <ChevronDown size={16} />}
        <span className="sw-event-group__task">
          <strong>{group.task || 'No data'}</strong>
          {group.play && (
            <Text as="span" color="fg.muted" fontSize="xs">
              {group.play}
            </Text>
          )}
        </span>
        <StatusBadge status={groupStatus(counts)} />
        <span className="sw-event-group__counts">
          <Badge colorPalette="green" variant="subtle">{counts.ok} ok</Badge>
          {counts.changed > 0 && <Badge colorPalette="blue" variant="subtle">{counts.changed} changed</Badge>}
          {counts.failed > 0 && <Badge colorPalette="red" variant="subtle">{counts.failed} failed</Badge>}
          {counts.unreachable > 0 && <Badge colorPalette="orange" variant="subtle">{counts.unreachable} unreachable</Badge>}
          {counts.skipped > 0 && <Badge colorPalette="gray" variant="subtle">{counts.skipped} skipped</Badge>}
        </span>
        <Text as="span" color="fg.muted" className="sw-event-group__hosts">
          {counts.total} {counts.total === 1 ? 'host' : 'hosts'}
        </Text>
      </button>

      {collapsed && hasError && (
        <div className="sw-event-group__failed">
          failed: {failed.slice(0, MAX_INLINE_FAILED_HOSTS).join(', ')}
          {failed.length > MAX_INLINE_FAILED_HOSTS ? ` +${failed.length - MAX_INLINE_FAILED_HOSTS} more` : ''}
        </div>
      )}

      {!collapsed && (
        <div className="sw-event-group__hostlist">
          <Table.Root size="sm" aria-label={`${group.task || 'task'} hosts`}>
            <Table.Body>
              {rows.map((event, index) => (
                <Table.Row key={`${event.host}-${index}`} data-error={isErrorEvent(event) || undefined}>
                  <Table.Cell className="mono">{event.host || 'No data'}</Table.Cell>
                  <Table.Cell>
                    <HStack gap="2">
                      <StatusBadge status={event.status} />
                      {event.changed && <Badge colorPalette="blue" variant="outline">changed</Badge>}
                    </HStack>
                  </Table.Cell>
                </Table.Row>
              ))}
              {rows.length === 0 && (
                <Table.Row>
                  <Table.Cell colSpan={2}>
                    <Text color="fg.muted">No hosts match these filters.</Text>
                  </Table.Cell>
                </Table.Row>
              )}
            </Table.Body>
          </Table.Root>
        </div>
      )}
    </Box>
  )
}
