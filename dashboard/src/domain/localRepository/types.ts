/**
 * A Site's Local Repository: the one place its Servers fetch every artifact a deployment installs
 * (decision 078; glossary terms local-repository.md and repository-source.md; contract
 * local-repository.md). swallow owns what it must hold — what the compiled modules declare plus the
 * user's Repository Sources (the spec) — and the worker converges the Site's `repository`
 * Integration to it and records what it observes. The dashboard reads that record, browses the
 * content live, replaces the user's sources, and asks for an immediate convergence; it never
 * derives readiness itself.
 *
 * The types mirror the contract's wire shapes, so the adapter returns the parsed bodies as they are
 * and the generated OpenAPI types prove the match where it does.
 */

import { findCondition, type Condition } from '@/domain/convergence/types'

/**
 * The kind of content, the core's only words for it: OS packages (`deb`; `rpm` is designed in but
 * not realized), container images (`oci`), Python packages (`python`), and single files (`file`).
 */
export type LocalRepositoryFormat = 'deb' | 'rpm' | 'oci' | 'python' | 'file'

/**
 * The observed state of a Repository Source, a declared file, or a declared image, a closed set:
 * - `pending`: not fetched yet, or not observed since its definition last changed (an image also
 *   while its registry's pull-through source is not `ready`);
 * - `syncing`: the provider is fetching it;
 * - `ready`: its content is served;
 * - `failed`: the last fetch failed, its `message` says why, and a later convergence tries again.
 */
export type LocalRepositorySyncState = 'pending' | 'syncing' | 'ready' | 'failed'

/**
 * One Repository Source of the user's spec. `suites` and `components` are set only for `deb` (at
 * least one each) and `architectures` only for `deb` and `rpm`; the read shape has empty arrays for
 * the rest.
 */
export interface RepositorySourceSpec {
  name: string
  format: LocalRepositoryFormat
  url: string
  suites: string[]
  components: string[]
  architectures: string[]
}

/**
 * The user's part of the Local Repository. `generation` is `0` before the first write and is the
 * value a write must send back, so a write based on an older read is refused instead of silently
 * replacing someone else's change.
 */
export interface LocalRepositorySpec {
  generation: number
  sources: RepositorySourceSpec[]
}

/**
 * One effective Repository Source: a module's declaration, a user source, or a pull-through source
 * swallow derives (one `oci` source per registry host a declared image names, and `pypi` when a
 * module declares a Python requirement), merged by `name`.
 *
 * `declaredBy` lists who declared it, in order: `module:<id>` or `user` (see {@link parseDeclarer}).
 * `lastSyncedAt` is the last successful sync, `null` before one. `contentCount` is how many items it
 * serves, and `0` means unknown rather than empty.
 */
export interface RepositorySource extends RepositorySourceSpec {
  declaredBy: string[]
  state: LocalRepositorySyncState
  message: string | null
  lastSyncedAt: string | null
  contentCount: number
}

/**
 * A file a module declares: downloaded once from `url` and verified against `sha256`, with no
 * upstream kept. `sizeBytes` is `0` until the file was fetched.
 */
export interface LocalRepositoryFile {
  path: string
  url: string
  sha256: string
  declaredBy: string[]
  state: LocalRepositorySyncState
  message: string | null
  sizeBytes: number
}

/**
 * A container image a module declares, fetched in advance through its registry's pull-through source
 * once that source is `ready`. It is `ready` only once the provider stores every layer (of an index,
 * the linux/amd64 image), so no Server pull reaches the upstream; an interrupted or failed fetch is
 * started again by the worker. `sizeBytes` is the total of its config and layers, `0` until stored.
 */
export interface LocalRepositoryImage {
  reference: string
  declaredBy: string[]
  state: LocalRepositorySyncState
  message: string | null
  sizeBytes: number
}

/** A Python requirement a module declares, fetched on first request through the `pypi` source. */
export interface LocalRepositoryPythonRequirement {
  requirement: string
  declaredBy: string[]
}

/**
 * A Site's Local Repository as swallow's store records it (`GET .../local-repository`; the provider
 * is not called). `ready` is the API's judgement — the Integration is enabled, the worker's last
 * pass was not stopped by the Integration or its provider, every source, file, and declared image
 * is `ready`, and the host addresses are known — and is exactly when deployments that fetch from it
 * are accepted.
 * `contentOrigin` is the Integration's setting (`null` when unset).
 *
 * `observedAt` is when the worker last completed a pass (`null` before the first) and
 * `observedGeneration` the spec generation that pass converged. `conditions` are the worker's
 * judgements, `[]` before it first looked at the Integration:
 * - `ready` (with, when `false`, a message listing everything in the way),
 * - `realized` (whether the provider serves every effective source, file, and image),
 * - `requires_attention` (a user must fix the Integration: its credential or content origin).
 *
 * A pass that stops records only conditions, so the sources, files, and images then keep an older
 * observation. The Integration's credential and the provider's identifiers never appear.
 */
export interface LocalRepository {
  siteId: string
  integrationId: string
  ready: boolean
  contentOrigin: string | null
  spec: LocalRepositorySpec
  sources: RepositorySource[]
  files: LocalRepositoryFile[]
  images: LocalRepositoryImage[]
  python: LocalRepositoryPythonRequirement[]
  observedAt: string | null
  observedGeneration: number
  conditions: Condition[]
}

/** The condition types the Local Repository controller writes (local-repository.md, Conditions). */
export const LocalRepositoryConditionTypes = {
  ready: 'ready',
  realized: 'realized',
  requiresAttention: 'requires_attention',
} as const

/**
 * One item of the content the provider serves. The fields its format does not have are `null`; the
 * contract does not say which formats have which, so a consumer shows what is present.
 */
export interface LocalRepositoryContentItem {
  name: string
  version: string | null
  architecture: string | null
  /** The Repository Source that serves it. */
  source: string | null
  path: string | null
  sizeBytes: number | null
}

/** One page of content; `total` counts the matching items across all pages. */
export interface LocalRepositoryContentPage {
  items: LocalRepositoryContentItem[]
  total: number
  page: number
  pageSize: number
}

/**
 * A spec write named a `generation` other than the stored one: someone changed the user's sources
 * since they were read, and nothing was written. The adapter raises it for the contract's `409
 * conflict` so a screen can re-read and tell the user, rather than inspect transport codes or
 * overwrite the other change.
 *
 * `requestId` matches the application `RequestFailure` port structurally (the domain cannot import
 * it), so the screen can still show it.
 */
export class LocalRepositorySpecConflictError extends Error {
  readonly requestId?: string

  constructor(message: string, requestId?: string) {
    super(message)
    this.name = 'LocalRepositorySpecConflictError'
    this.requestId = requestId
  }
}

/**
 * The formats the content browser offers. `rpm` is left out: the contract accepts it, but no adapter
 * serves it yet (glossary local-repository.md), so it would only ever show an empty list.
 */
export const BROWSABLE_FORMATS: readonly LocalRepositoryFormat[] = ['deb', 'oci', 'python', 'file']

/** Whether a source, file, or image has not settled yet: it waits for its fetch or is being fetched. */
export function isSyncInProgress(state: LocalRepositorySyncState): boolean {
  return state === 'pending' || state === 'syncing'
}

/** Whether any source, file, or declared image of the Local Repository is still `pending` or `syncing`. */
export function hasSyncInProgress(repository: Pick<LocalRepository, 'sources' | 'files' | 'images'>): boolean {
  return repository.sources.some((source) => isSyncInProgress(source.state))
    || repository.files.some((file) => isSyncInProgress(file.state))
    || repository.images.some((image) => isSyncInProgress(image.state))
}

/**
 * Whether the worker is expected to change what the Local Repository reads without anyone acting,
 * which is when a view should keep re-reading it: a source, file, or image is still being fetched
 * (the worker re-converges it with a growing delay), or a saved spec generation has not been
 * converged yet. A spec the worker cannot converge — its pass stopped for a user to fix the
 * Integration (`requires_attention`), or the Integration is disabled — waits for a person, not for
 * time.
 */
export function isConverging(repository: Pick<LocalRepository, 'sources' | 'files' | 'images' | 'spec' | 'observedGeneration' | 'conditions'>): boolean {
  if (hasSyncInProgress(repository)) return true
  if (repository.observedGeneration >= repository.spec.generation) return false
  const attention = findCondition(repository.conditions, LocalRepositoryConditionTypes.requiresAttention)
  const ready = findCondition(repository.conditions, LocalRepositoryConditionTypes.ready)
  return attention?.status !== 'true' && ready?.reason !== 'integration_disabled'
}

/** Who declared an item, read from one `declaredBy` entry. */
export type Declarer =
  | { kind: 'module'; moduleId: string }
  | { kind: 'user' }
  /** A value this build does not know, kept so it can be shown as-is. */
  | { kind: 'other'; value: string }

/** Reads one `declaredBy` entry: `module:<id>` is a module, `user` is the user's spec. */
export function parseDeclarer(value: string): Declarer {
  if (value === 'user') return { kind: 'user' }
  if (value.startsWith('module:') && value.length > 'module:'.length) return { kind: 'module', moduleId: value.slice('module:'.length) }
  return { kind: 'other', value }
}

/**
 * Whether the user's spec declares the source. Only those can be edited from the dashboard; a source
 * only modules (or the pull-through derivation) declare changes with the module.
 */
export function isUserDeclared(source: Pick<RepositorySource, 'declaredBy'>): boolean {
  return source.declaredBy.includes('user')
}

// 1 to 63 lower-case letters, digits, `.`, `_`, or `-`, starting and ending with a letter or digit.
const SOURCE_NAME_PATTERN = /^[a-z0-9](?:[a-z0-9._-]{0,61}[a-z0-9])?$/

/**
 * Whether `name` is an acceptable Repository Source name (it becomes part of the address Servers
 * read the source at). Mirrored so a form can flag a typo before a write; the API remains the
 * validator of record, and it also refuses a name that contradicts a module's declaration.
 */
export function isValidSourceName(name: string): boolean {
  return SOURCE_NAME_PATTERN.test(name)
}

/**
 * Splits a list typed as words separated by commas or whitespace (suites, components,
 * architectures) into its words, dropping empty entries. Order is kept so the spec reads back as
 * typed.
 */
export function parseWordList(value: string): string[] {
  return value.split(/[\s,]+/).filter((word) => word !== '')
}

/** Whether a word is listed more than once; the API refuses that in suites, components, and architectures. */
export function hasDuplicateWords(words: readonly string[]): boolean {
  return new Set(words).size !== words.length
}
