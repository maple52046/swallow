import type { Condition } from '@/domain/convergence/types'
import {
  parseDeclarer,
  type LocalRepositoryFormat,
  type LocalRepositorySyncState,
  type RepositorySourceSpec,
} from '@/domain/localRepository/types'
import { formatBytes } from '@/shared/utils/bytes'

/** How one observed sync state reads: the shared StatusBadge family and its visible label. */
export interface SyncStatePresentation {
  badgeStatus: string
  label: string
}

const SYNC_STATES: Record<LocalRepositorySyncState, SyncStatePresentation> = {
  pending: { badgeStatus: 'pending', label: 'Pending' },
  // `running` gives the in-progress colour; the label keeps the contract's word.
  syncing: { badgeStatus: 'running', label: 'Syncing' },
  ready: { badgeStatus: 'ready', label: 'Ready' },
  failed: { badgeStatus: 'failed', label: 'Failed' },
}

/**
 * The badge of a Repository Source's, file's, or declared image's observed state. A value a newer
 * API adds reads as a neutral badge with its raw text rather than as a failure.
 */
export function syncStatePresentation(state: string): SyncStatePresentation {
  // The cast only types the lookup; a value outside the set misses it and takes the fallback.
  return SYNC_STATES[state as LocalRepositorySyncState] ?? { badgeStatus: 'unknown', label: state }
}

/**
 * The badge of the controller's `realized` condition (local-repository.md, Conditions): whether the
 * provider serves every effective source, file, and image, and if not, the reason's family. Reasons
 * this build does not know read by status alone; `undefined` is a repository the worker has not
 * judged.
 */
export function realizedPresentation(condition: Condition | undefined): SyncStatePresentation {
  if (!condition) return { badgeStatus: 'pending', label: 'Not checked yet' }
  if (condition.status === 'true') return { badgeStatus: 'ready', label: 'In sync' }
  if (condition.status === 'unknown') return { badgeStatus: 'unknown', label: 'Unknown' }
  switch (condition.reason) {
    case 'syncing':
      return { badgeStatus: 'running', label: 'Syncing' }
    case 'source_failed':
    case 'file_failed':
    case 'image_failed':
      return { badgeStatus: 'failed', label: 'Sync failed' }
    case 'provider_unreachable':
      return { badgeStatus: 'degraded', label: 'Provider unreachable, retrying' }
    case 'credential_missing':
    case 'credential_rejected':
    case 'content_origin_missing':
    case 'integration_invalid':
      return { badgeStatus: 'requires_attention', label: 'Needs attention' }
    default:
      return { badgeStatus: 'warning', label: 'Not in sync' }
  }
}

/**
 * The Integration settings tab where a user fixes what a `requires_attention` reason names: the
 * credential for a missing or rejected one, Connection for the content origin, General otherwise.
 */
export function attentionTab(reason: string): 'credential' | 'connection' | 'general' {
  if (reason === 'credential_missing' || reason === 'credential_rejected') return 'credential'
  if (reason === 'content_origin_missing') return 'connection'
  return 'general'
}

/** The content browser's names for the formats; the domain value is what the URL and the API carry. */
export const FORMAT_LABELS: Record<LocalRepositoryFormat, string> = {
  deb: 'OS packages (deb)',
  rpm: 'OS packages (rpm)',
  oci: 'Container images',
  python: 'Python packages',
  file: 'Files',
}

/** The same formats as a plural noun inside a sentence ("Showing 1–25 of 120 container images"). */
export const FORMAT_NOUNS: Record<LocalRepositoryFormat, string> = {
  deb: 'OS packages',
  rpm: 'OS packages',
  oci: 'container images',
  python: 'Python packages',
  file: 'files',
}

/**
 * One `declaredBy` entry as a reader sees it: a module by its id (`module:k0s` reads `k0s`), the
 * user's spec as "User", and anything else as sent.
 */
function declarerLabel(value: string): string {
  const declarer = parseDeclarer(value)
  if (declarer.kind === 'module') return declarer.moduleId
  if (declarer.kind === 'user') return 'User'
  return declarer.value
}

/** Every declarer of an item, in declaration order, as one line. */
export function declarersText(declaredBy: readonly string[]): string {
  return declaredBy.length > 0 ? declaredBy.map(declarerLabel).join(', ') : '—'
}

/** The first 12 hex digits of a sha256, which tells files apart in a table; the full value is copied. */
export function shortDigest(sha256: string): string {
  return sha256.length > 12 ? `${sha256.slice(0, 12)}…` : sha256
}

/**
 * A size the provider reports, or an em dash when it reports none: a content item's `null`, and a
 * declared file's or image's `0` before it was stored.
 */
export function formatReportedSize(sizeBytes: number | null): string {
  return sizeBytes !== null && sizeBytes > 0 ? formatBytes(sizeBytes) : '—'
}

/** A source's item count; the contract's `0` means unknown, not empty. */
export function formatContentCount(contentCount: number): string {
  return contentCount > 0 ? new Intl.NumberFormat().format(contentCount) : '—'
}

/** Suites, components, or architectures as one line, or an em dash for a format without them. */
export function wordListText(words: readonly string[]): string {
  return words.length > 0 ? words.join(', ') : '—'
}

/**
 * What a source takes from its upstream — suites, components, architectures — as one line, or `''`
 * for a format without them (every format but `deb` and `rpm`).
 */
export function sourceScopeText(source: Pick<RepositorySourceSpec, 'suites' | 'components' | 'architectures'>): string {
  return [
    source.suites.length > 0 ? `Suites ${source.suites.join(', ')}` : '',
    source.components.length > 0 ? `components ${source.components.join(', ')}` : '',
    source.architectures.length > 0 ? `architectures ${source.architectures.join(', ')}` : '',
  ].filter(Boolean).join(' · ')
}
