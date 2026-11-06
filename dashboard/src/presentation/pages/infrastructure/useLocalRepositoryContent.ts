import { useCallback, useEffect, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import type { LocalRepositoryContentQuery } from '@/application/ports/LocalRepositoryPort'
import type { LocalRepositoryContentPage } from '@/domain/localRepository/types'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'

/**
 * One content page read, keyed by its query so changing the format, source, search, or page shows
 * loading at once instead of the previous query's rows under the new controls.
 */
export type LocalRepositoryContentState =
  | { status: 'loading'; key: string }
  | ({ status: 'error'; key: string } & DisplayedFailure)
  | { status: 'ready'; key: string; page: LocalRepositoryContentPage }

/**
 * Reads one page of a Site's Local Repository content, live from the provider through the API.
 *
 * The read runs whenever the query changes; there is no polling, because content changes only as
 * the sources sync and the page's own state already shows that. The cleanup drops the answer of a
 * superseded query, so a slow earlier page never replaces a newer one. A provider that cannot be
 * reached or refuses the query is an `error` with the API's message (for example a missing content
 * origin). `reload` reads the same query again, showing loading meanwhile.
 */
export function useLocalRepositoryContent(siteId: string, query: LocalRepositoryContentQuery): {
  state: LocalRepositoryContentState
  reload: () => void
} {
  const { localRepository } = useApp()
  const { format, source, search, page, pageSize } = query
  const key = [siteId, format, source ?? '', search ?? '', page, pageSize].join('|')
  const [snapshot, setSnapshot] = useState<LocalRepositoryContentState>({ status: 'loading', key })
  const [nonce, setNonce] = useState(0)

  useEffect(() => {
    let cancelled = false
    localRepository
      .browseContent(siteId, { format, source, search, page, pageSize })
      .then((result) => {
        if (!cancelled) setSnapshot({ status: 'ready', key, page: result })
      })
      .catch((error: unknown) => {
        if (!cancelled) setSnapshot({ status: 'error', key, ...failureOf(error, 'The content could not be loaded.') })
      })
    return () => {
      cancelled = true
    }
  }, [localRepository, siteId, format, source, search, page, pageSize, key, nonce])

  const reload = useCallback(() => {
    setSnapshot({ status: 'loading', key })
    setNonce((value) => value + 1)
  }, [key])

  const state: LocalRepositoryContentState = snapshot.key === key ? snapshot : { status: 'loading', key }
  return { state, reload }
}
