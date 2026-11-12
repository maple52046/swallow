import { useCallback, useEffect, useRef, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Paginated } from '@/application/ports/ServerRepository'
import type { ValidationRunFilter } from '@/application/ports/ValidationRepository'
import { isValidationRunActive, type ValidationRunSummary } from '@/domain/validation/types'
import { failureOf, failureText, type DisplayedFailure } from '@/presentation/components/failure'

const POLL_INTERVAL_MS = 5000

/** One server-paginated Validation Run working set. */
export type ValidationRunsState =
  | { status: 'loading'; filterKey: string }
  | ({ status: 'error'; filterKey: string } & DisplayedFailure)
  | {
      status: 'ready'
      filterKey: string
      page: Paginated<ValidationRunSummary>
      refreshedAt: string
      /** A background failure keeps the last-good rows visible. */
      refreshError?: string
    }

function validationFilterKey(filter: ValidationRunFilter): string {
  return [
    filter.siteId ?? '',
    filter.suiteId ?? '',
    filter.serverId ?? '',
    filter.platformId ?? '',
    filter.scheduleId ?? '',
    filter.status ?? '',
    filter.verdict ?? '',
    filter.trigger ?? '',
    filter.active ? 'active' : '',
    filter.page ?? '',
    filter.pageSize ?? '',
  ].join('|')
}

/**
 * Loads one page of Validation Runs and keeps active runs current.
 *
 * The API remains authoritative for filtering, pagination, and newest-first ordering. A changed
 * filter returns loading immediately so rows from another Site or URL-owned view never flash under
 * the new controls. Pending and running rows poll every five seconds; a transient refresh failure
 * preserves the last successful page, surfaces a request-aware warning, and retries.
 */
export function useValidationRuns(filter: ValidationRunFilter): {
  state: ValidationRunsState
  reload: () => void
} {
  const { validation } = useApp()
  const filterKey = validationFilterKey(filter)
  const [snapshot, setSnapshot] = useState<ValidationRunsState>({ status: 'loading', filterKey })
  const [nonce, setNonce] = useState(0)
  const loadedFilter = useRef<string | undefined>(undefined)
  const pollingFilter = useRef<string | undefined>(undefined)
  const pollingEnabled = useRef(false)
  const reload = useCallback(() => setNonce((value) => value + 1), [])

  const {
    siteId, suiteId, serverId, platformId, scheduleId, status, verdict, trigger, active, page, pageSize,
  } = filter

  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined
    let shouldPoll = pollingFilter.current === filterKey && pollingEnabled.current
    if (loadedFilter.current !== filterKey) shouldPoll = false

    const scheduleNext = () => {
      if (!cancelled && shouldPoll) timer = setTimeout(load, POLL_INTERVAL_MS)
    }
    const load = () => {
      validation.listRuns({
        siteId, suiteId, serverId, platformId, scheduleId, status, verdict, trigger, active, page, pageSize,
      }).then((result) => {
        if (cancelled) return
        shouldPoll = result.items.some((run) => isValidationRunActive(run.status))
        pollingFilter.current = filterKey
        pollingEnabled.current = shouldPoll
        loadedFilter.current = filterKey
        setSnapshot({
          status: 'ready',
          filterKey,
          page: result,
          refreshedAt: new Date().toISOString(),
        })
        scheduleNext()
      }).catch((caught: unknown) => {
        if (cancelled) return
        const failure = failureOf(caught, 'Could not load Validation Runs.')
        if (loadedFilter.current === filterKey) {
          setSnapshot((current) => current.status === 'ready' && current.filterKey === filterKey
            ? { ...current, refreshError: failureText(failure) }
            : current)
          scheduleNext()
          return
        }
        setSnapshot({ status: 'error', filterKey, ...failure })
      })
    }

    load()
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [
    validation, filterKey, siteId, suiteId, serverId, platformId, scheduleId, status, verdict,
    trigger, active, page, pageSize, nonce,
  ])

  const state = snapshot.filterKey === filterKey
    ? snapshot
    : { status: 'loading' as const, filterKey }
  return { state, reload }
}
