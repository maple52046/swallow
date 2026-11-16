import type { ValidationRepository } from '@/application/ports/ValidationRepository'
import { currentSuiteRevision, type SuiteRevisionContent } from '@/domain/validation/revisions'
import type { ValidationSuite } from '@/domain/validation/types'

/**
 * What changed in a user suite between the revision a User reviewed (or last ran) and its current
 * revision: what the review before a run shows (validation contract, Revisions). `previous` is null when that revision was
 * never kept (one from before revisions were kept), so only the current one can be shown.
 */
export interface SuiteChange {
  fromRevision: number
  previous: SuiteRevisionContent | null
  current: SuiteRevisionContent
}

/**
 * The change a User must review before starting `suite`: from the revision of their own latest run
 * of it (`requestedBy=me`) to its current revision. Null when nothing needs review: a module suite,
 * a suite they never ran, or one still at the revision they last ran.
 *
 * Reads the runs list, then (only when the revisions differ) the earlier revision. Read failures
 * reject; the caller shows them and offers no way to start the run until a read succeeds, because
 * a skipped review would run a changed script unseen.
 */
export async function suiteChangeSinceLastRun(validation: ValidationRepository, suite: ValidationSuite): Promise<SuiteChange | null> {
  const current = currentSuiteRevision(suite)
  if (!current) return null
  const latest = await validation.listRuns({ suiteId: suite.id, requestedBy: 'me', page: 1, pageSize: 1 })
  const lastRan = latest.items[0]?.suite.revision ?? null
  if (lastRan === null || lastRan === current.revision) return null
  return suiteChangeSince(validation, suite, lastRan)
}

/**
 * The change from `fromRevision` to the current revision of `suite`, read for a review: after the
 * last run (above), or after a start refused because the suite changed while the dialog was open.
 * A revision the API no longer keeps (`null` from the port) leaves `previous` null.
 */
export async function suiteChangeSince(validation: ValidationRepository, suite: ValidationSuite, fromRevision: number): Promise<SuiteChange | null> {
  const current = currentSuiteRevision(suite)
  if (!current || current.revision === fromRevision) return null
  const previous = await validation.getSuiteRevision(suite.id, fromRevision)
  return { fromRevision, previous, current }
}
