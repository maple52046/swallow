import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { Paginated } from '@/application/ports/ServerRepository'
import type { ValidationRunFilter } from '@/application/ports/ValidationRepository'
import { isValidationRunActive, type ValidationRunSummary } from '@/domain/validation/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { useSettlingRefresh } from '@/presentation/hooks/useSettlingRefresh'

/** One page of Validation Runs as a view holds it. */
export type ValidationRunsState =
  | { status: 'loading' }
  | ({ status: 'error' } & DisplayedFailure)
  | { status: 'ready'; page: Paginated<ValidationRunSummary> }

const LOADING: ValidationRunsState = { status: 'loading' }

/**
 * Reads one page of Validation Runs (newest first) for `filter` through the ValidationRepository
 * port, for the Runs list and the recent runs on Server and Platform pages.
 *
 * - A new filter (compared by value), or `reload` after an error, reads again and shows `loading`
 *   until it answers: the stored answer belongs to the request it was read for, so an answer for
 *   an older filter is never shown as the current one.
 * - While a run on the page has not ended, the page is re-read every few seconds in place
 *   (`useSettlingRefresh`), so a run moves to its verdict without a manual reload; a failed re-read
 *   keeps what is shown. Polling stops when every run has ended or the view unmounts.
 * - Every read takes a ticket and only the newest may publish, so a slow read never overwrites a
 *   newer one. Every route requires `admin`; a non-admin sees the API's error.
 */
export function useValidationRuns(filter: ValidationRunFilter): { state: ValidationRunsState; reload: () => void } {
  const { validation } = useApp()
  // Callers rebuild the filter object on every render; its value is what counts, so the filter
  // the reads use changes only when its JSON does.
  const filterKey = JSON.stringify(filter)
  const stableFilter = useMemo(() => JSON.parse(filterKey) as ValidationRunFilter, [filterKey])
  const [nonce, setNonce] = useState(0)
  const requestKey = `${filterKey}#${nonce}`
  const [stored, setStored] = useState<{ key: string; state: ValidationRunsState }>({ key: '', state: LOADING })
  const ticket = useRef(0)

  /** Re-reads the page in place; rejects with the read's failure for the caller to handle. */
  const refresh = useCallback(async () => {
    const mine = ++ticket.current
    const page = await validation.listRuns(stableFilter)
    if (mine === ticket.current) setStored({ key: requestKey, state: { status: 'ready', page } })
  }, [validation, stableFilter, requestKey])

  // The first read of a request (a filter, or a reload). Its failure shows only while it is still
  // the newest read; cleanup invalidates it, so an unmounted view never publishes.
  useEffect(() => {
    const mine = ++ticket.current
    validation.listRuns(stableFilter).then(
      (page) => {
        if (mine === ticket.current) setStored({ key: requestKey, state: { status: 'ready', page } })
      },
      (caught: unknown) => {
        if (mine === ticket.current) setStored({ key: requestKey, state: { status: 'error', ...failureOf(caught, 'Could not load Validation Runs.') } })
      },
    )
    return () => {
      ticket.current += 1
    }
  }, [validation, stableFilter, requestKey])

  const state = stored.key === requestKey ? stored.state : LOADING
  const active = state.status === 'ready' && state.page.items.some((run) => isValidationRunActive(run.status))
  useSettlingRefresh(active, refresh)

  const reload = useCallback(() => setNonce((value) => value + 1), [])
  return { state, reload }
}
