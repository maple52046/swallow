import type {
  LocalRepository,
  LocalRepositoryContentPage,
  LocalRepositoryFormat,
  RepositorySourceSpec,
} from '@/domain/localRepository/types'

/** One page request of the content browser; `source` and `search` narrow it when set. */
export interface LocalRepositoryContentQuery {
  format: LocalRepositoryFormat
  /** A Repository Source name: only that source's content. */
  source?: string
  /** Only items whose name contains it. */
  search?: string
  /** 1-based. */
  page: number
  pageSize: number
}

/** The user's sources to store, with the spec generation they were based on. */
export interface LocalRepositorySpecWrite {
  generation: number
  sources: readonly RepositorySourceSpec[]
}

/**
 * Application port for a Site's Local Repository (contract local-repository.md, decision 078).
 *
 * Every call needs the `admin` role; the server authorizes. Implementations map the provider
 * contract only and reject with the shared error envelope unless stated otherwise:
 * - a Site without a `repository` Integration has no Local Repository, which `getLocalRepository`
 *   resolves as `null` (a domain miss, not a failure); the other calls reject with `not_found`;
 * - the credential and the provider's identifiers never cross this boundary.
 */
export interface LocalRepositoryPort {
  /**
   * The Site's Local Repository as swallow's store records it, with the state the worker last
   * observed; the provider is not called. `null` when the Site has no `repository` Integration.
   */
  getLocalRepository(siteId: string): Promise<LocalRepository | null>
  /**
   * One page of content read live from the provider. Rejects with `validation_error` when the query
   * is out of range or the provider refused it (the message is its reason, for example a missing
   * content origin) and `provider_unavailable` when the provider cannot be reached or refuses the
   * Integration's credential.
   */
  browseContent(siteId: string, query: LocalRepositoryContentQuery): Promise<LocalRepositoryContentPage>
  /**
   * Replaces the user's sources as a whole and resolves the stored Local Repository; the worker
   * converges the provider afterwards, so new sources read `pending` until it observes them. Rejects
   * with `LocalRepositorySpecConflictError` when `generation` is not the stored one (nothing was
   * written), and with `validation_error` for an invalid source or one that contradicts a module's
   * declaration.
   */
  replaceSpec(siteId: string, spec: LocalRepositorySpecWrite): Promise<LocalRepository>
  /**
   * Asks the worker to converge the Local Repositories now rather than at its next resync. It
   * resolves once accepted; the result appears in a later `getLocalRepository`, and the provider
   * decides which sources need a new sync.
   */
  requestSync(siteId: string): Promise<void>
}
