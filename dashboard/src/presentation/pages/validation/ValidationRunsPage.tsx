import { useState } from 'react'
import { Button, HStack, Link as ChakraLink, Stack, Text } from '@chakra-ui/react'
import { Play } from 'lucide-react'
import { Link as RouterLink, useSearchParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { ValidationRunFilter } from '@/application/ports/ValidationRepository'
import type { ValidationRunStatus, ValidationRunVerdict, ValidationSuite } from '@/domain/validation/types'
import { DataToolbar, SectionSurface } from '@/presentation/components/ConsolePrimitives'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { failureOf, failureText } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { Pagination } from '@/presentation/components/Pagination'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Select } from '@/presentation/components/ui/select'
import { RUN_STATUS_LABEL, VERDICT_LABEL } from '@/presentation/components/validation/validationPresentation'
import { ValidationRunList } from '@/presentation/components/validation/ValidationRunList'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { usePlatformNamesById } from '@/presentation/hooks/usePlatformNames'
import { useServerNames } from '@/presentation/hooks/useServerNames'
import { useValidationRuns } from '@/presentation/hooks/useValidationRuns'
import { StartValidationRunDialog } from '@/presentation/components/validation/StartValidationRunDialog'
import { ValidationHeader } from './ValidationHeader'

const PAGE_SIZE = 20
const STATUSES: readonly ValidationRunStatus[] = ['pending', 'running', 'completed', 'canceled']
const VERDICTS: readonly ValidationRunVerdict[] = ['passed', 'failed', 'error']

/** The URL's filter values that name a value of `allowed`; anything else is ignored, not sent. */
function oneOf<T extends string>(value: string | null, allowed: readonly T[]): T | undefined {
  return allowed.find((item) => item === value)
}

/**
 * The Runs tab of Validation (route `/validation/runs`, decision 081): every Validation Run, newest
 * first, whether a user, a schedule, or a deployment's gate started it.
 *
 * The URL owns the list: `status` and `verdict` filters, `page`, and the `serverId`, `platformId`,
 * `suiteId`, and `scheduleId` a link from a Server, Platform, suite, or schedule narrows it to
 * (named above the list, with a way to clear it). Unknown filter values are ignored. The list
 * re-reads itself while a run on the page is unfinished (`useValidationRuns`). Run a suite opens
 * the run dialog over the catalog, which is read when the dialog opens. Every route requires
 * `admin`; a non-admin sees the API's error.
 */
export function ValidationRunsPage() {
  const { validation } = useApp()
  const { showToast } = useToast()
  const { scopedHref } = useSiteScope()
  const [params, setParams] = useSearchParams()
  const filter: ValidationRunFilter = {
    status: oneOf(params.get('status'), STATUSES),
    verdict: oneOf(params.get('verdict'), VERDICTS),
    serverId: params.get('serverId') || undefined,
    platformId: params.get('platformId') || undefined,
    suiteId: params.get('suiteId') || undefined,
    scheduleId: params.get('scheduleId') || undefined,
    page: Math.max(1, Number(params.get('page')) || 1),
    pageSize: PAGE_SIZE,
  }
  const { state, reload } = useValidationRuns(filter)
  const [dialog, setDialog] = useState<{ key: number; suites: ValidationSuite[] } | null>(null)
  const [opening, setOpening] = useState(false)
  const serverName = useServerNames(filter.serverId ? [filter.serverId] : []).get(filter.serverId ?? '')
  const platformName = usePlatformNamesById(filter.platformId ? [filter.platformId] : []).get(filter.platformId ?? '')

  /** Writes one filter into the URL and returns to the first page. */
  const setFilter = (name: string, value: string) => {
    const next = new URLSearchParams(params)
    if (value) next.set(name, value)
    else next.delete(name)
    next.delete('page')
    setParams(next)
  }
  const setPage = (page: number) => {
    const next = new URLSearchParams(params)
    next.set('page', String(page))
    setParams(next)
  }

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

  const scope = filter.serverId
    ? `Server ${serverName ?? filter.serverId}`
    : filter.platformId
      ? `Platform ${platformName ?? filter.platformId}`
      : filter.suiteId
        ? `suite ${filter.suiteId}`
        : filter.scheduleId
          ? `schedule ${filter.scheduleId}`
          : null

  return (
    <div className="console-page">
      <ValidationHeader
        subtitle="Every Validation Run: what it checked, on which targets, and each check’s verdict."
        actions={
          <Button colorPalette="brand" onClick={() => void openDialog()} loading={opening}>
            <Play size={16} aria-hidden /> Run a suite
          </Button>
        }
      />
      <SectionSurface title="Runs">
        <Stack gap="4">
          <DataToolbar>
            <HStack gap="3" wrap="wrap">
              <Select
                aria-label="Status"
                size="sm"
                width="10rem"
                value={filter.status ?? ''}
                onChange={(value) => setFilter('status', value)}
                options={[{ value: '', label: 'Any status' }, ...STATUSES.map((status) => ({ value: status, label: RUN_STATUS_LABEL[status] }))]}
              />
              <Select
                aria-label="Verdict"
                size="sm"
                width="10rem"
                value={filter.verdict ?? ''}
                onChange={(value) => setFilter('verdict', value)}
                options={[{ value: '', label: 'Any verdict' }, ...VERDICTS.map((verdict) => ({ value: verdict, label: VERDICT_LABEL[verdict] }))]}
              />
              {scope && (
                <Text fontSize="sm">
                  Runs of {scope}.{' '}
                  <ChakraLink asChild color="brand.fg">
                    <RouterLink to={scopedHref('/validation/runs')}>Show every run</RouterLink>
                  </ChakraLink>
                </Text>
              )}
            </HStack>
          </DataToolbar>
          {state.status === 'loading' && <LoadingState rows={5} />}
          {state.status === 'error' && <ErrorState message={state.message} requestId={state.requestId} onRetry={reload} />}
          {state.status === 'ready' && state.page.items.length === 0 && (
            <EmptyState
              title="No Validation Runs"
              message="Run a suite on deployed Servers or a Platform, or let a schedule or a deployment start one; its verdicts appear here."
            />
          )}
          {state.status === 'ready' && state.page.items.length > 0 && (
            <>
              <ValidationRunList runs={state.page.items} label="Validation Runs" />
              <Pagination total={Math.ceil(state.page.total / PAGE_SIZE)} value={filter.page ?? 1} onChange={setPage} subject="runs" />
            </>
          )}
        </Stack>
      </SectionSurface>
      {/* A new key per opening resets the form; the dialog stays mounted so it can animate closed. */}
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
