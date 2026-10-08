import { useCallback, useEffect, useRef, useState } from 'react'
import { Button, IconButton, Table, Text, VisuallyHidden } from '@chakra-ui/react'
import { Eye, RefreshCw, RotateCcw } from 'lucide-react'
import { Link, useLocation } from 'react-router-dom'
import type { Operation } from '@/domain/operation/types'
import type { ProvisioningTask } from '@/domain/provisioning/types'
import type { ProviderEvents } from '@/domain/server/types'
import { useApp } from '@/di/AppProvider'
import { SectionHeader, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { Pagination } from '@/presentation/components/Pagination'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
import { Select } from '@/presentation/components/ui/select'
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
import { SERVER_ACTIVITY_SECTION_IDS, SERVER_ACTIVITY_SECTION_TITLES } from './serverActivitySections'
import { useServerDetailContext } from './useServerDetail'

type LoadState<T> = { status: 'loading' } | { status: 'error'; message: string } | { status: 'ready'; data: T }

const DEFAULT_ACTIVITY_PAGE_SIZE = 10
const ACTIVITY_PAGE_SIZES = [5, 10, 15, 20] as const
const ACTIVITY_PAGE_SIZE_OPTIONS = ACTIVITY_PAGE_SIZES.map((size) => ({
  value: String(size),
  label: `${size} rows`,
}))

interface ActivityPage<T> {
  items: T[]
  page: number
  totalPages: number
  rangeStart: number
  rangeEnd: number
  total: number
  pageSize: number
}

interface ActivitySectionPage {
  page: number
  pageSize: number
}

interface ActivityPages {
  serverId: string
  tasks: ActivitySectionPage
  provider: ActivitySectionPage
  related: ActivitySectionPage
}

function initialActivityPages(serverId: string): ActivityPages {
  const section = () => ({ page: 1, pageSize: DEFAULT_ACTIVITY_PAGE_SIZE })
  return { serverId, tasks: section(), provider: section(), related: section() }
}

/** Preserves source ordering while constraining one activity section to a compact page. */
function activityPage<T>(items: readonly T[], requestedPage: number, pageSize: number): ActivityPage<T> {
  const totalPages = Math.max(1, Math.ceil(items.length / pageSize))
  const page = Math.min(Math.max(requestedPage, 1), totalPages)
  const start = (page - 1) * pageSize
  return {
    items: items.slice(start, start + pageSize),
    page,
    totalPages,
    rangeStart: items.length === 0 ? 0 : start + 1,
    rangeEnd: Math.min(start + pageSize, items.length),
    total: items.length,
    pageSize,
  }
}

/**
 * Composes the three truthful activity sources for one Server: live provider events, durable
 * automation Operations, and current-tab synchronous action diagnostics. Each source has its
 * own loading/error/empty state, so one unavailable source never blanks the whole tab.
 *
 * Each section carries an id from `SERVER_ACTIVITY_SECTION_IDS`, so a `#activity-…` hash (the
 * Server list's View/Review activity icons) opens the tab on the section that records the work.
 */
export function ServerActivityTab() {
  const { server } = useServerDetailContext()
  const { servers, operations } = useApp()
  const { scopedHref } = useSiteScope()
  const { hash } = useLocation()
  const { showToast } = useToast()
  const [provider, setProvider] = useState<LoadState<ProviderEvents>>({ status: 'loading' })
  const [related, setRelated] = useState<LoadState<Operation[]>>({ status: 'loading' })
  const [tasks, setTasks] = useState<LoadState<ProvisioningTask[]>>({ status: 'loading' })
  const [sessionResults, setSessionResults] = useState<ServerActionRunResult[]>(() => listStoredServerActionResults(server.id))
  const [selectedResult, setSelectedResult] = useState<ServerActionRunResult | null>(null)
  const [retryingTaskId, setRetryingTaskId] = useState('')
  const [pages, setPages] = useState<ActivityPages>(() => initialActivityPages(server.id))
  const [nonce, setNonce] = useState(0)
  const activePages = pages.serverId === server.id ? pages : initialActivityPages(server.id)
  const setActivityPage = (section: 'tasks' | 'provider' | 'related', page: number) => {
    setPages((current) => {
      const base = current.serverId === server.id ? current : initialActivityPages(server.id)
      return { ...base, [section]: { ...base[section], page } }
    })
  }
  const setActivityPageSize = (section: 'tasks' | 'provider' | 'related', pageSize: number) => {
    setPages((current) => {
      const base = current.serverId === server.id ? current : initialActivityPages(server.id)
      return { ...base, [section]: { page: 1, pageSize } }
    })
  }
  const refresh = useCallback(() => {
    setProvider({ status: 'loading' })
    setRelated({ status: 'loading' })
    setTasks({ status: 'loading' })
    setPages((current) => {
      const base = current.serverId === server.id ? current : initialActivityPages(server.id)
      return {
        ...base,
        tasks: { ...base.tasks, page: 1 },
        provider: { ...base.provider, page: 1 },
        related: { ...base.related, page: 1 },
      }
    })
    setNonce((value) => value + 1)
  }, [server.id])

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

  const sourcesSettled = provider.status !== 'loading' && related.status !== 'loading' && tasks.status !== 'loading'
  const pagedTasks = tasks.status === 'ready'
    ? activityPage(tasks.data, activePages.tasks.page, activePages.tasks.pageSize)
    : null
  const pagedProviderEvents = provider.status === 'ready'
    ? activityPage(provider.data.events, activePages.provider.page, activePages.provider.pageSize)
    : null
  const pagedRelated = related.status === 'ready'
    ? activityPage(related.data, activePages.related.page, activePages.related.pageSize)
    : null
  const scrolledHashRef = useRef('')
  // Lands a section deep link. Scrolling waits until every source has settled because the
  // sections above the target change height when their rows arrive, and it runs once per hash
  // so a later Refresh never pulls the operator back to the section.
  useEffect(() => {
    if (!sourcesSettled || !hash || scrolledHashRef.current === hash) return
    scrolledHashRef.current = hash
    document.getElementById(hash.slice(1))?.scrollIntoView({ block: 'start' })
  }, [hash, sourcesSettled])

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
      <section className="sw-section" id={SERVER_ACTIVITY_SECTION_IDS.session}>
        <SectionHeader
          title={SERVER_ACTIVITY_SECTION_TITLES.session}
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

      <section className="sw-section" id={SERVER_ACTIVITY_SECTION_IDS['provisioning-tasks']}>
        <SectionHeader
          title={SERVER_ACTIVITY_SECTION_TITLES['provisioning-tasks']}
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
        {pagedTasks && pagedTasks.total > 0 && (
          <>
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Provisioning tasks">
                <Table.Header>
                  <Table.Row><Table.ColumnHeader>Status</Table.ColumnHeader><Table.ColumnHeader>Task</Table.ColumnHeader><Table.ColumnHeader>Phase</Table.ColumnHeader><Table.ColumnHeader>Updated</Table.ColumnHeader><Table.ColumnHeader>Error</Table.ColumnHeader><Table.ColumnHeader>Request ID</Table.ColumnHeader><Table.ColumnHeader><VisuallyHidden>Actions</VisuallyHidden></Table.ColumnHeader></Table.Row>
                </Table.Header>
                <Table.Body>
                  {pagedTasks.items.map((task) => (
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
            <ActivityPagination
              page={pagedTasks}
              subject="provisioning tasks"
              onChange={(page) => setActivityPage('tasks', page)}
              onPageSize={(pageSize) => setActivityPageSize('tasks', pageSize)}
            />
          </>
        )}
      </section>

      <section className="sw-section" id={SERVER_ACTIVITY_SECTION_IDS['provider-events']}>
        <SectionHeader
          title={SERVER_ACTIVITY_SECTION_TITLES['provider-events']}
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
        {pagedProviderEvents && pagedProviderEvents.total > 0 && (
          <>
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Provider events">
                <Table.Header>
                  <Table.Row><Table.ColumnHeader>Time</Table.ColumnHeader><Table.ColumnHeader>Level</Table.ColumnHeader><Table.ColumnHeader>Type</Table.ColumnHeader><Table.ColumnHeader>Message</Table.ColumnHeader><Table.ColumnHeader>Actor</Table.ColumnHeader></Table.Row>
                </Table.Header>
                <Table.Body>
                  {pagedProviderEvents.items.map((event) => (
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
            <ActivityPagination
              page={pagedProviderEvents}
              subject="provider events"
              onChange={(page) => setActivityPage('provider', page)}
              onPageSize={(pageSize) => setActivityPageSize('provider', pageSize)}
            />
          </>
        )}
      </section>

      <section className="sw-section" id={SERVER_ACTIVITY_SECTION_IDS['related-operations']}>
        <SectionHeader title={SERVER_ACTIVITY_SECTION_TITLES['related-operations']} description="Durable automation runs that include this Server. Open one for retained events and stdout." />
        {related.status === 'loading' && <div className="sw-section-empty">Loading related Operations...</div>}
        {related.status === 'error' && (
          <div className="sw-activity-alert">
            <Alert status="warning" title="Related Operations are unavailable">
              {related.message}
            </Alert>
          </div>
        )}
        {related.status === 'ready' && related.data.length === 0 && <div className="sw-section-empty">No durable Operations include this Server.</div>}
        {pagedRelated && pagedRelated.total > 0 && (
          <>
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Related Operations">
                <Table.Header>
                  <Table.Row><Table.ColumnHeader>Status</Table.ColumnHeader><Table.ColumnHeader>Operation</Table.ColumnHeader><Table.ColumnHeader>Kind</Table.ColumnHeader><Table.ColumnHeader>Requested</Table.ColumnHeader><Table.ColumnHeader>Requested by</Table.ColumnHeader></Table.Row>
                </Table.Header>
                <Table.Body>
                  {pagedRelated.items.map((operation) => (
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
            <ActivityPagination
              page={pagedRelated}
              subject="related Operations"
              onChange={(page) => setActivityPage('related', page)}
              onPageSize={(pageSize) => setActivityPageSize('related', pageSize)}
            />
          </>
        )}
      </section>

      {selectedResult && <ServerActionResultDialog result={selectedResult} onClose={() => setSelectedResult(null)} />}
    </div>
  )
}

function ActivityPagination<T>({
  page,
  subject,
  onChange,
  onPageSize,
}: {
  page: ActivityPage<T>
  subject: string
  onChange: (page: number) => void
  onPageSize: (pageSize: number) => void
}) {
  if (page.total <= ACTIVITY_PAGE_SIZES[0]) return null
  return (
    <div className="sw-activity-pagination">
      <Text color="fg.muted" fontSize="sm" aria-live="polite">
        Showing {page.rangeStart}–{page.rangeEnd} of {page.total}
      </Text>
      <div className="sw-activity-pagination__controls">
        <Select
          value={String(page.pageSize)}
          aria-label={`${subject} rows per page`}
          size="sm"
          width="8rem"
          onChange={(value) => onPageSize(Number(value))}
          options={ACTIVITY_PAGE_SIZE_OPTIONS}
        />
        <Pagination total={page.totalPages} value={page.page} onChange={onChange} subject={subject} />
      </div>
    </div>
  )
}
