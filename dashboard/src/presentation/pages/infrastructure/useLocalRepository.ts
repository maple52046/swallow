import { useCallback, useEffect, useRef, useState } from 'react'
import { useApp } from '@/di/AppProvider'
import { isConverging, type LocalRepository } from '@/domain/localRepository/types'
import { failureOf, failureText, type DisplayedFailure } from '@/presentation/components/failure'

/** How often the page re-reads the Local Repository while something in it is still being fetched. */
export const LOCAL_REPOSITORY_POLL_MS = 5000

/**
 * How long the page keeps re-reading after a sync request while the worker has not observed the
 * repository again. The request only asks for a convergence and the provider may find nothing to
 * fetch, so no state needs to change; the next `observedAt` is what ends the wait.
 */
const SYNC_FOLLOW_MS = 120_000

/**
 * One Site's Local Repository read, keyed by the Site so a scope change never shows the previous
 * Site's repository. `missing` is a Site without a `repository` Integration, a normal answer the
 * page explains rather than a failure. `refreshError` is a failed background re-read: the last good
 * read stays on screen with that line of text (request ID included).
 */
export type LocalRepositoryState =
  | { status: 'loading'; siteId: string }
  | ({ status: 'error'; siteId: string } & DisplayedFailure)
  | { status: 'missing'; siteId: string }
  | { status: 'ready'; siteId: string; repository: LocalRepository; refreshError?: string }

/** The observation a sync request waits for: the Site, the `observedAt` it was made against, and a deadline. */
interface SyncFollow {
  siteId: string
  observedAt: string | null
  until: number
}

/**
 * Loads a Site's Local Repository from swallow's store and keeps it current while it converges.
 *
 * Polling follows the conventions of the Workflow list (`useOperations`): a timer chain re-reads
 * every {@link LOCAL_REPOSITORY_POLL_MS} while the repository is converging (`isConverging`: a
 * source, file, or image is `pending` or `syncing`, or a saved spec is not converged yet), and stops once it
 * settled; a failed re-read keeps the last good repository and retries on the same schedule. After {@link followSync} it also re-reads until the worker records a new
 * observation (or {@link SYNC_FOLLOW_MS} passes). Cleanup cancels the timer and drops the answers of
 * an unmounted view or a previous Site.
 *
 * - `reload` reads again now without resetting the view.
 * - `replace` shows a repository a write already returned, then reads again so polling resumes for
 *   sources that now read `pending`.
 * - `followSync` is called after a sync request was accepted.
 */
export function useLocalRepository(siteId: string): {
  state: LocalRepositoryState
  reload: () => void
  replace: (repository: LocalRepository) => void
  followSync: () => void
} {
  const { localRepository } = useApp()
  const [snapshot, setSnapshot] = useState<LocalRepositoryState>({ status: 'loading', siteId })
  const [nonce, setNonce] = useState(0)
  // The Site whose repository is on screen, so a failed re-read keeps it instead of erroring.
  const loadedSiteRef = useRef<string | undefined>(undefined)
  // Whether the last good read was still converging, so a failed re-read keeps retrying only then.
  const pollingRef = useRef(false)
  // Read by the timer chain outside render; a ref so starting a follow does not restart the chain.
  const followRef = useRef<SyncFollow | null>(null)
  const reload = useCallback(() => setNonce((value) => value + 1), [])

  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | undefined

    // Whether to read again: the repository is still converging, or a sync request still waits for
    // the worker's next observation. A follow ends once `observedAt` moves or its deadline passes.
    const shouldPoll = (repository: LocalRepository) => {
      const follow = followRef.current
      const following = follow !== null && follow.siteId === siteId && follow.observedAt === repository.observedAt && Date.now() < follow.until
      if (!following) followRef.current = null
      return isConverging(repository) || following
    }

    const load = () => {
      localRepository
        .getLocalRepository(siteId)
        .then((repository) => {
          if (cancelled) return
          if (!repository) {
            loadedSiteRef.current = undefined
            pollingRef.current = false
            setSnapshot({ status: 'missing', siteId })
            return
          }
          loadedSiteRef.current = siteId
          pollingRef.current = shouldPoll(repository)
          setSnapshot({ status: 'ready', siteId, repository })
          if (pollingRef.current) timer = setTimeout(load, LOCAL_REPOSITORY_POLL_MS)
        })
        .catch((error: unknown) => {
          if (cancelled) return
          const failure = failureOf(error, 'The Local Repository could not be loaded.')
          if (loadedSiteRef.current === siteId) {
            setSnapshot((current) => current.status === 'ready' && current.siteId === siteId
              ? { ...current, refreshError: failureText(failure) }
              : current)
            if (pollingRef.current) timer = setTimeout(load, LOCAL_REPOSITORY_POLL_MS)
            return
          }
          setSnapshot({ status: 'error', siteId, ...failure })
        })
    }

    load()
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [localRepository, siteId, nonce])

  const replace = useCallback((repository: LocalRepository) => {
    loadedSiteRef.current = repository.siteId
    setSnapshot({ status: 'ready', siteId: repository.siteId, repository })
    setNonce((value) => value + 1)
  }, [])

  const observedAt = snapshot.status === 'ready' && snapshot.siteId === siteId ? snapshot.repository.observedAt : null
  const followSync = useCallback(() => {
    followRef.current = { siteId, observedAt, until: Date.now() + SYNC_FOLLOW_MS }
    setNonce((value) => value + 1)
  }, [observedAt, siteId])

  const state: LocalRepositoryState = snapshot.siteId === siteId ? snapshot : { status: 'loading', siteId }
  return { state, reload, replace, followSync }
}
