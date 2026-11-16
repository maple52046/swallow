import type { Paginated } from '@/application/ports/ServerRepository'
import type {
  UserSuiteInput,
  ValidationRun,
  ValidationRunInput,
  ValidationRunStatus,
  ValidationRunSummary,
  ValidationRunVerdict,
  ValidationSchedule,
  ValidationScheduleInput,
  ValidationSuite,
  ValidationSuiteRevision,
  ValidationTriggerKind,
} from '@/domain/validation/types'

/** Which runs a list returns; every field narrows it, and an unset field does not. */
export interface ValidationRunFilter {
  siteId?: string
  suiteId?: string
  /** Runs whose target or member Servers include this Server. */
  serverId?: string
  platformId?: string
  scheduleId?: string
  status?: ValidationRunStatus
  verdict?: ValidationRunVerdict
  trigger?: ValidationTriggerKind
  /** Only runs that have not ended. */
  active?: boolean
  /**
   * `me` keeps only the runs the signed-in User started; with `suiteId`, the first item is their
   * latest run of that suite, whose `suite.revision` is the revision they last ran.
   */
  requestedBy?: 'me'
  page?: number
  pageSize?: number
}

/**
 * Validation Suites, Runs, and Schedules (contract `validation.md`, decision 081). Every User reads
 * suites and the runs of targets they may read, and writes their own user suites; schedules are
 * admin routes. Implementations reject with a `RequestFailure` the presentation reads with
 * `failureOf`, never with a raw transport error.
 *
 * - Module suites are read-only (`409`). A user suite is replaced by its author only and retired by
 *   its author or an admin (`403` otherwise); a retired suite stays readable by id for its past runs.
 * - `replaceSuite` with `input.revision` applies only at that revision: a stale one, or a replace
 *   that raced another, rejects with `ValidationConflictError` and writes nothing.
 * - `startRun` answers with the accepted run (`pending`). Any `409` (a stale `suiteRevision`, or a
 *   target that is busy, locked, undeployed, or has no account for the starter) rejects with
 *   `ValidationConflictError`, so the caller re-reads the suite to tell which.
 * - `cancelRun` writes the cancel of a report run's Workflow and answers with the run as it was;
 *   the run turns `canceled` once the Workflow ends. A gate run is canceled with its deployment.
 * - Schedule writes carry `generation`; a stale one is refused (`409`) so an edit never overwrites
 *   a spec the admin did not see.
 */
export interface ValidationRepository {
  listSuites(includeRetired?: boolean): Promise<ValidationSuite[]>
  /** Null when no suite has this id. */
  getSuite(id: string): Promise<ValidationSuite | null>
  /**
   * One kept revision of a user suite. Null for a revision that was never kept (one from before
   * revisions were kept), an unknown one, and any revision of a module suite.
   */
  getSuiteRevision(suiteId: string, revision: number): Promise<ValidationSuiteRevision | null>
  /** The caller becomes the author (`createdById`). */
  createSuite(input: UserSuiteInput): Promise<ValidationSuite>
  replaceSuite(id: string, input: UserSuiteInput): Promise<ValidationSuite>
  retireSuite(id: string): Promise<void>

  /** Newest first. */
  listRuns(filter?: ValidationRunFilter): Promise<Paginated<ValidationRunSummary>>
  /** Null when no run has this id (including one past its retention). */
  getRun(id: string): Promise<ValidationRun | null>
  startRun(input: ValidationRunInput): Promise<ValidationRun>
  cancelRun(id: string): Promise<ValidationRun>

  /** By name. */
  listSchedules(siteId?: string): Promise<ValidationSchedule[]>
  createSchedule(input: ValidationScheduleInput): Promise<ValidationSchedule>
  replaceSchedule(id: string, input: ValidationScheduleInput): Promise<ValidationSchedule>
  deleteSchedule(id: string): Promise<void>
}
