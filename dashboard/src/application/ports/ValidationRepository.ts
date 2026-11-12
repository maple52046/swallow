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
  page?: number
  pageSize?: number
}

/**
 * Validation Suites, Runs, and Schedules (contract `validation.md`, decision 081). Every route
 * requires `admin`, so a non-admin gets the API's error; implementations reject with a
 * `RequestFailure` the presentation reads with `failureOf`, never with a raw transport error.
 *
 * - Module suites are read-only: replacing or retiring one is refused (`409`); a user suite is
 *   retired rather than deleted, and a retired suite stays readable by id for its past runs.
 * - `startRun` answers with the accepted run (`pending`); its targets are checked against the
 *   suite and must not be busy, locked, or undeployed, or the API refuses it (`409`).
 * - `cancelRun` writes the cancel of a report run's Workflow and answers with the run as it was;
 *   the run turns `canceled` once the Workflow ends. A gate run is canceled with its deployment.
 * - Schedule writes carry `generation`; a stale one is refused (`409`) so an edit never overwrites
 *   a spec the admin did not see.
 */
export interface ValidationRepository {
  listSuites(includeRetired?: boolean): Promise<ValidationSuite[]>
  /** Null when no suite has this id. */
  getSuite(id: string): Promise<ValidationSuite | null>
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
