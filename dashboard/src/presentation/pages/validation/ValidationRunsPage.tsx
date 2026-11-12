import { useCallback, useEffect, useState } from 'react'
import { Alert, Box, Button, Flex, HStack, Link as ChakraLink, Text } from '@chakra-ui/react'
import { CircleX, Play, RefreshCw, ShieldAlert } from 'lucide-react'
import { Link as RouterLink, useSearchParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { ValidationRunFilter } from '@/application/ports/ValidationRepository'
import type {
  ValidationRunStatus,
  ValidationRunVerdict,
  ValidationSuite,
  ValidationTriggerKind,
} from '@/domain/validation/types'
import { InventorySurface } from '@/presentation/components/ConsolePrimitives'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, failureText } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { Pagination } from '@/presentation/components/Pagination'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Select } from '@/presentation/components/ui/select'
import { ValidationRunList } from '@/presentation/components/validation/ValidationRunList'
import { StartValidationRunDialog } from '@/presentation/components/validation/StartValidationRunDialog'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { usePlatformNamesById } from '@/presentation/hooks/usePlatformNames'
import { useServerNames } from '@/presentation/hooks/useServerNames'
import { useValidationRuns } from '@/presentation/hooks/useValidationRuns'
import { formatRelative } from '@/shared/utils/time'
import { ValidationHeader } from './ValidationHeader'

const PAGE_SIZE = 20
const STATUSES: readonly ValidationRunStatus[] = ['pending', 'running', 'completed', 'canceled']
const VERDICTS: readonly ValidationRunVerdict[] = ['passed', 'failed', 'error']
const TRIGGERS: readonly ValidationTriggerKind[] = ['manual', 'schedule', 'deployment']

type Lens = 'all' | 'active' | 'failed' | 'error' | 'canceled'

function oneOf<T extends string>(value: string | null, allowed: readonly T[]): T | undefined {
  return allowed.find((item) => item === value)
}

/**
 * Assurance-first Validation Run inventory. The API owns ordering and pagination while the URL owns
 * filters, deep-link resource scopes, and the page. Site scope is sent to the API rather than only
 * decorating the route.
 */
export function ValidationRunsPage() {
  const { validation } = useApp()
  const { showToast } = useToast()
  const { siteId, scopedHref } = useSiteScope()
  const [params, setParams] = useSearchParams()
  const status = oneOf(params.get('status'), STATUSES)
  const verdict = oneOf(params.get('verdict'), VERDICTS)
  const trigger = oneOf(params.get('trigger'), TRIGGERS)
  const active = params.get('active') === 'true'
  const page = Math.max(1, Number(params.get('page')) || 1)
  const filter: ValidationRunFilter = {
    siteId,
    active,
    status,
    verdict,
    trigger,
    serverId: params.get('serverId') || undefined,
    platformId: params.get('platformId') || undefined,
    suiteId: params.get('suiteId') || undefined,
    scheduleId: params.get('scheduleId') || undefined,
    page,
    pageSize: PAGE_SIZE,
  }
  const { state, reload } = useValidationRuns(filter)
  const [dialog, setDialog] = useState<{ key: number; suites: ValidationSuite[] } | null>(null)
  const [opening, setOpening] = useState(false)
  const serverName = useServerNames(filter.serverId ? [filter.serverId] : []).get(filter.serverId ?? '')
  const platformName = usePlatformNamesById(filter.platformId ? [filter.platformId] : []).get(filter.platformId ?? '')

  const updateParams = useCallback((update: (next: URLSearchParams) => void, replace = false) => {
    const next = new URLSearchParams(params)
    update(next)
    setParams(next, { replace })
  }, [params, setParams])
  const setFilter = (name: string, value?: string) => updateParams((next) => {
    if (value) next.set(name, value)
    else next.delete(name)
    next.delete('page')
  })
  const setLens = (lens: Lens) => updateParams((next) => {
    next.delete('active')
    next.delete('status')
    next.delete('verdict')
    next.delete('page')
    if (lens === 'active') next.set('active', 'true')
    if (lens === 'failed' || lens === 'error') next.set('verdict', lens)
    if (lens === 'canceled') next.set('status', 'canceled')
  })
  const clearFilters = () => updateParams((next) => {
    for (const key of ['active', 'status', 'verdict', 'trigger', 'serverId', 'platformId', 'suiteId', 'scheduleId', 'page']) {
      next.delete(key)
    }
  })

  const openDialog = async () => {
    setOpening(true)
    try {
      setDialog({ key: Date.now(), suites: await validation.listSuites() })
    } catch (caught: unknown) {
      showToast({ tone: 'error', title: 'The suites could not be loaded', description: failureText(failureOf(caught, '')) })
    } finally {
      setOpening(false)
    }
  }

  const totalPages = state.status === 'ready' ? Math.max(1, Math.ceil(state.page.total / PAGE_SIZE)) : 1
  useEffect(() => {
    if (state.status === 'ready' && page > totalPages) {
      updateParams((next) => {
        if (totalPages > 1) next.set('page', String(totalPages))
        else next.delete('page')
      }, true)
    }
  }, [page, state.status, totalPages, updateParams])

  const lens: Lens | null = active
    ? 'active'
    : verdict === 'failed' || verdict === 'error'
      ? verdict
      : status === 'canceled' ? 'canceled' : status || verdict ? null : 'all'
  const hasFilters = active || Boolean(status) || Boolean(verdict) || Boolean(trigger) ||
    Boolean(filter.serverId || filter.platformId || filter.suiteId || filter.scheduleId)
  const live = state.status === 'ready' && state.page.items.some((run) => run.status === 'pending' || run.status === 'running')
  const rangeStart = state.status === 'ready' && state.page.items.length > 0 ? (page - 1) * PAGE_SIZE + 1 : 0
  const rangeEnd = state.status === 'ready' ? rangeStart + Math.max(0, state.page.items.length - 1) : 0
  const summary = state.status === 'ready' ? (
    <div className="sw-workflow-result-summary">
      <span>{state.page.total === 0 ? 'Showing 0 runs' : `Showing ${rangeStart}–${rangeEnd} of ${state.page.total} runs`}</span>
      <span className="sw-workflow-refresh-state" data-live={live || undefined}>
        {live && <span className="sw-workflow-live-dot" aria-hidden />}
        {live ? 'Live updates every 5s' : `Updated ${formatRelative(state.refreshedAt)}`}
      </span>
    </div>
  ) : state.status === 'error' ? 'Validation activity unavailable' : 'Loading Validation activity'

  const scope = filter.serverId
    ? `Server ${serverName ?? filter.serverId}`
    : filter.platformId
      ? `Platform ${platformName ?? filter.platformId}`
      : filter.suiteId
        ? `Suite ${filter.suiteId}`
        : filter.scheduleId ? `Schedule ${filter.scheduleId}` : null

  return (
    <div className="console-page sw-workflows-page">
      <ValidationHeader
        subtitle="Review recent assurance results, active checks, and outcomes that need investigation."
        actions={
          <HStack gap="2">
            <Button variant="outline" onClick={reload} disabled={state.status === 'loading'}>
              <RefreshCw size={16} aria-hidden /> Refresh
            </Button>
            <Button colorPalette="brand" onClick={() => void openDialog()} loading={opening}>
              <Play size={16} aria-hidden /> Run a suite
            </Button>
          </HStack>
        }
      />
      <InventorySurface
        headingId="validation-runs-title"
        eyebrow="Validation assurance"
        title="Validation activity"
        summary={summary}
        toolbar={
          <>
            <Flex as="div" className="sw-workflow-filter-group" role="group" aria-label="Validation outcome view">
              {([
                ['all', 'All'],
                ['active', 'Active'],
                ['failed', 'Failed'],
                ['error', 'Errors'],
                ['canceled', 'Canceled'],
              ] as const).map(([value, label]) => (
                <Button
                  key={value}
                  className="sw-workflow-filter-chip"
                  variant="plain"
                  size="sm"
                  aria-pressed={lens === value}
                  data-active={lens === value || undefined}
                  onClick={() => setLens(value)}
                >
                  {value === 'failed' && <ShieldAlert size={14} aria-hidden />}
                  {value === 'error' && <CircleX size={14} aria-hidden />}
                  {label}
                </Button>
              ))}
            </Flex>
            <Select
              value={trigger ?? ''}
              onChange={(value) => setFilter('trigger', value)}
              options={[
                { value: '', label: 'Any trigger' },
                { value: 'manual', label: 'Manual' },
                { value: 'schedule', label: 'Schedule' },
                { value: 'deployment', label: 'Deployment gate' },
              ]}
              aria-label="Filter by trigger"
              size="sm"
              width="auto"
            />
            {scope && (
              <Text fontSize="sm">
                Showing {scope}.{' '}
                <ChakraLink asChild color="brand.fg">
                  <RouterLink to={scopedHref('/validation/runs')}>Show every run</RouterLink>
                </ChakraLink>
              </Text>
            )}
          </>
        }
      >
        {state.status === 'ready' && state.refreshError && (
          <Box p="4" pb="0">
            <Alert.Root status="warning">
              <Alert.Indicator />
              <Alert.Content>
                <Alert.Title>Live updates interrupted</Alert.Title>
                <Alert.Description>{state.refreshError} Showing the last successful result and retrying.</Alert.Description>
              </Alert.Content>
            </Alert.Root>
          </Box>
        )}
        {state.status === 'loading' && <Box p="4"><LoadingState rows={7} /></Box>}
        {state.status === 'error' && <Box p="4"><ErrorState message={state.message} requestId={state.requestId} onRetry={reload} /></Box>}
        {state.status === 'ready' && state.page.total === 0 && !hasFilters && (
          <Box p="4">
            <EmptyState
              title="No Validation Runs yet"
              message="Run a suite on deployed Servers or a Platform. Scheduled and deployment-gate runs will appear here too."
            />
          </Box>
        )}
        {state.status === 'ready' && state.page.total === 0 && hasFilters && (
          <Box p="4">
            <EmptyState
              title="No runs match these filters"
              message="The Site has Validation history, but not for this view."
              action={{ label: 'Clear filters', onClick: clearFilters }}
            />
          </Box>
        )}
        {state.status === 'ready' && state.page.items.length > 0 && (
          <>
            <ValidationRunList runs={state.page.items} label="Validation Runs" showSite={!siteId} />
            <Box p="4">
              <Pagination
                total={totalPages}
                value={page}
                onChange={(nextPage) => updateParams((next) => {
                  if (nextPage > 1) next.set('page', String(nextPage))
                  else next.delete('page')
                })}
                subject="runs"
              />
            </Box>
          </>
        )}
      </InventorySurface>
      <StartValidationRunDialog
        key={dialog?.key ?? 0}
        open={dialog !== null}
        onClose={() => setDialog(null)}
        suites={dialog?.suites ?? []}
        preset={{ serverIds: filter.serverId ? [filter.serverId] : undefined, platformId: filter.platformId }}
      />
    </div>
  )
}
