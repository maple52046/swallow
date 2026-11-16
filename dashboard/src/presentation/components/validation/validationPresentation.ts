import type { SuiteStartBlocker } from '@/domain/validation/access'
import type {
  ValidationCheck,
  ValidationImpact,
  ValidationStarterLevel,
  ValidationResult,
  ValidationResultTarget,
  ValidationRun,
  ValidationRunStatus,
  ValidationRunSummary,
  ValidationScheduleTrigger,
  ValidationSuite,
  ValidationVerdict,
} from '@/domain/validation/types'

/**
 * Wording of the Validation area (decision 081). Every value comes from the validation contract;
 * these helpers only name it for people, and pick the `StatusBadge` colour family a value reads as.
 * Colour is never the only cue: each badge also carries the label.
 */

/** People's names of a check's verdict on one target. */
export const VERDICT_LABEL: Readonly<Record<ValidationVerdict, string>> = {
  passed: 'Passed', failed: 'Failed', error: 'Error', skipped: 'Skipped',
}

/** The StatusBadge status each verdict reads as: an error is a warning, a skip is neutral. */
export const VERDICT_BADGE: Readonly<Record<ValidationVerdict, string>> = {
  passed: 'succeeded', failed: 'failed', error: 'warning', skipped: 'skipped',
}

/** People's names of a run's lifecycle states. */
export const RUN_STATUS_LABEL: Readonly<Record<ValidationRunStatus, string>> = {
  pending: 'Pending', running: 'Running', completed: 'Completed', canceled: 'Canceled',
}

/** What each impact means for the targets, written for the User choosing whether to run it. */
export const IMPACT_COPY: Readonly<Record<ValidationImpact, { label: string; description: string }>> = {
  observe: { label: 'Observe', description: 'Only reads the targets, or probes them too lightly for running workloads to notice.' },
  workload: {
    label: 'Workload',
    description: 'Holds or saturates what workloads use (GPUs, bandwidth, scheduler nodes) for a while; not run on Servers a Platform claims.',
  },
  disruptive: { label: 'Disruptive', description: 'May interrupt service on the targets; each run must be confirmed and no schedule can start it.' },
}

/** Which suites a list or picker offers; see {@link offeredSuites}. */
export interface SuiteOfferOptions {
  /** Whether experimental module suites are offered (`useShowExperimentalSuites`). */
  showExperimental: boolean
  /**
   * The signed-in User's id when other Users' suites are hidden (`useHideOtherUsersSuites`), or
   * undefined to offer every author's. A user suite without an author (`createdById` null) is
   * another User's.
   */
  hideOthersThan?: string
  /** The suite the picker opened with, always kept so the form never loses its value. */
  keepId?: string
  /** Narrows the catalog further, for example to the suites a schedule may start. */
  eligible?: (suite: ValidationSuite) => boolean
}

/**
 * The suites a picker or the catalog offers, and how many each preference leaves out: retired suites
 * never; experimental suites only when the User shows them; user suites of other authors not when
 * the User hides them (module suites are never hidden by that). `keepId` survives both preferences.
 * The counts let the screen say that something is hidden rather than look empty.
 */
export function offeredSuites(
  suites: readonly ValidationSuite[],
  { showExperimental, hideOthersThan, keepId, eligible = () => true }: SuiteOfferOptions,
): { offered: ValidationSuite[]; hiddenExperimental: number; hiddenOtherUsers: number } {
  const candidates = suites.filter((suite) => !suite.retired && eligible(suite))
  const isOthers = (suite: ValidationSuite) =>
    hideOthersThan !== undefined && suite.source === 'user' && suite.createdById !== hideOthersThan && suite.id !== keepId
  const shownByAuthor = candidates.filter((suite) => !isOthers(suite))
  const offered = shownByAuthor.filter((suite) => showExperimental || !suite.experimental || suite.id === keepId)
  return {
    offered,
    hiddenExperimental: shownByAuthor.length - offered.length,
    hiddenOtherUsers: candidates.length - shownByAuthor.length,
  }
}

/** What each starter level means, for the editor's choice and the suite's details. */
export const STARTER_LEVEL_COPY: Readonly<Record<ValidationStarterLevel, { label: string; description: string }>> = {
  owner: {
    label: 'Pool owners',
    description: 'Only an owner of every target’s Pool (or an admin) can start it.',
  },
  member: {
    label: 'Pool members too',
    description: 'Any member of every target’s Pool can start it, on the Servers they may log in to.',
  },
}

/** What `sudo` means on the targets, for the editor's switch and the suite's details. */
export const SUDO_COPY: Readonly<Record<'on' | 'off', string>> = {
  on: 'Runs through the starter’s own sudo -n, so only on targets where the Sudo Policy gives them passwordless sudo.',
  off: 'Runs with the starter’s own rights. The script may still call sudo -n where the Sudo Policy allows it.',
}

/**
 * Who wrote a user suite, for "Created by …" on its card and page. A suite whose author could not be
 * identified (`createdById` null) is shown as a deleted author, still naming the username it was
 * created by; nobody may edit such a suite.
 */
export function suiteAuthorName(suite: Pick<ValidationSuite, 'createdBy' | 'createdById'>): string {
  if (suite.createdById === null) return suite.createdBy ? `${suite.createdBy} (author deleted)` : 'a deleted User'
  return suite.createdBy ?? 'an unknown User'
}

/**
 * Why the run button is disabled for a suite, said in words (never colour alone). Mirrors
 * `SuiteStartBlocker`; the API decides again when the run is started.
 */
export const START_BLOCKER_TEXT: Readonly<Record<SuiteStartBlocker, string>> = {
  retired: 'A retired suite cannot be run.',
  'no-pool': 'You are in no Pool, so there is no target you can run it on.',
  'owners-only': 'Only a Pool owner can start this suite, and you own no Pool.',
}

/** The account a run uses on its targets, in one phrase: root for a module suite run. */
export function runAccountLabel(run: Pick<ValidationRunSummary, 'runAs'>): string {
  if (!run.runAs) return 'root'
  return `${run.runAs.username} (uid ${run.runAs.uidNumber})${run.runAs.sudo ? ', through sudo' : ''}`
}

/**
 * The badge of a run as a whole: a completed run shows its verdict, any other run its status. A
 * completed run without a verdict cannot happen in the contract; it falls back to its status.
 */
export function runOutcome(run: Pick<ValidationRunSummary, 'status' | 'verdict'>): { status: string; label: string } {
  if (run.status === 'completed' && run.verdict) return { status: VERDICT_BADGE[run.verdict], label: VERDICT_LABEL[run.verdict] }
  return { status: run.status, label: RUN_STATUS_LABEL[run.status] }
}

/** How a run was started, in one phrase. */
export function runTriggerLabel(run: Pick<ValidationRunSummary, 'trigger' | 'requestedBy' | 'mode'>): string {
  switch (run.trigger.kind) {
    case 'schedule':
      return 'Schedule'
    case 'deployment':
      return run.mode === 'gate' ? 'Deployment gate' : 'After deployment'
    default:
      return run.requestedBy ? `Started by ${run.requestedBy}` : 'Started by a user'
  }
}

/** A suite's origin: the module that declares it, or a User's script at a revision. */
export function suiteSourceLabel(suite: Pick<ValidationSuite, 'source' | 'module' | 'revision'>): string {
  if (suite.source === 'module') return suite.module ? `${suite.module.id} (${suite.module.kind} module) ${suite.module.version}` : 'Module'
  return suite.revision ? `Script, revision ${suite.revision}` : 'Script'
}

/** What a run ran: a module suite by id and version, a script suite by its revision. */
export function runSuiteLabel(suite: ValidationRun['suite']): string {
  if (suite.source === 'user') return suite.revision ? `Script suite, revision ${suite.revision}` : 'Script suite'
  return suite.version ? `${suite.id} ${suite.version}` : suite.id
}

/** A schedule's trigger in one phrase. */
export function scheduleTriggerLabel(trigger: ValidationScheduleTrigger): string {
  if (trigger.kind === 'after-deployment') return 'After each deployment'
  return `Cron ${trigger.cron ?? ''} (${trigger.timezone || 'UTC'})`
}

/** The text of a counts row, naming only the verdicts that occurred. */
export function countsLabel(counts: ValidationRunSummary['counts']): string {
  const parts = (['passed', 'failed', 'error', 'skipped'] as const)
    .filter((verdict) => counts[verdict] > 0)
    .map((verdict) => `${counts[verdict]} ${VERDICT_LABEL[verdict].toLowerCase()}`)
  return parts.length > 0 ? parts.join(', ') : 'No results yet'
}

/** One column of the results matrix: a target some check judged, or is expected to judge. */
export interface ResultColumn {
  key: string
  target: ValidationResultTarget
}

/** One row of the results matrix: a check and its result on each column (null: none recorded). */
export interface ResultRow {
  check: ValidationCheck
  cells: Array<ValidationResult | null>
}

/**
 * The results of a run as a check × target matrix, the way the Run detail shows them. Columns are
 * the run's target Servers first, in the run's order, then every other target a result names, in
 * the order the results name them (a Platform suite judges its member Servers and the Platform
 * itself). Rows are the run's checks in the suite's order. A missing cell means no result was
 * recorded for that check on that target yet; after the run completes, swallow records an `error`
 * in its place, so the dashboard never guesses a verdict for it.
 */
export function resultMatrix(run: Pick<ValidationRun, 'checks' | 'results' | 'target'>): { columns: ResultColumn[]; rows: ResultRow[] } {
  const columns: ResultColumn[] = []
  const seen = new Set<string>()
  const add = (target: ValidationResultTarget) => {
    const key = `${target.kind}:${target.id}`
    if (seen.has(key)) return
    seen.add(key)
    columns.push({ key, target })
  }
  if (run.target.kind === 'servers') run.target.serverIds.forEach((id) => add({ kind: 'server', id }))
  run.results.forEach((result) => add(result.target))
  const byCell = new Map(run.results.map((result) => [`${result.check}|${result.target.kind}:${result.target.id}`, result]))
  const rows = run.checks.map((check) => ({
    check,
    cells: columns.map((column) => byCell.get(`${check.id}|${column.key}`) ?? null),
  }))
  return { columns, rows }
}


/** Stable capability groups for the Suite catalog, derived only from domain facts. */
export type ValidationSuiteGroup = 'platform' | 'servers' | 'custom'

export function validationSuiteGroup(suite: ValidationSuite): ValidationSuiteGroup {
  if (suite.source === 'user') return 'custom'
  return suite.target === 'platform' ? 'platform' : 'servers'
}

/** Failed checks precede execution errors in the issue-first run view. */
export function orderedIssues(results: readonly ValidationResult[]): ValidationResult[] {
  return results
    .filter((result) => result.verdict === 'failed' || result.verdict === 'error')
    .sort((left, right) => {
      const rank = { failed: 0, error: 1 } as const
      return rank[left.verdict as 'failed' | 'error'] - rank[right.verdict as 'failed' | 'error']
    })
}

/** Human duration from API timestamps, without inventing progress. */
export function runDuration(startedAt: string | null, finishedAt: string | null): string {
  if (!startedAt) return 'Not started'
  const end = finishedAt ? Date.parse(finishedAt) : Date.now()
  const seconds = Math.floor(Math.max(0, end - Date.parse(startedAt)) / 1000)
  if (seconds < 60) return seconds + 's'
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return minutes + 'm ' + (seconds % 60) + 's'
  return Math.floor(minutes / 60) + 'h ' + (minutes % 60) + 'm'
}

export type SchedulePresetKind = 'after-deployment' | 'daily' | 'weekly' | 'custom'

export interface SchedulePreset {
  kind: SchedulePresetKind
  minute: string
  hour: string
  weekday: string
  cron: string
}

/** Recognizes friendly schedule presets without changing an unrecognized expression. */
export function schedulePreset(trigger: ValidationScheduleTrigger): SchedulePreset {
  if (trigger.kind === 'after-deployment') {
    return { kind: 'after-deployment', minute: '0', hour: '2', weekday: '1', cron: '' }
  }
  const cron = trigger.cron ?? ''
  const fields = cron.trim().split(/\s+/)
  if (fields.length === 5) {
    const [minute, hour, day, month, weekday] = fields
    if (day === '*' && month === '*' && weekday === '*') {
      return { kind: 'daily', minute, hour, weekday: '1', cron }
    }
    if (day === '*' && month === '*' && /^[0-7]$/.test(weekday)) {
      return { kind: 'weekly', minute, hour, weekday, cron }
    }
  }
  return { kind: 'custom', minute: '0', hour: '2', weekday: '1', cron }
}

/** Produces the canonical five-field expression of a friendly preset. */
export function cronForSchedulePreset(preset: SchedulePreset): string {
  if (preset.kind === 'daily') return preset.minute + ' ' + preset.hour + ' * * *'
  if (preset.kind === 'weekly') return preset.minute + ' ' + preset.hour + ' * * ' + preset.weekday
  return preset.cron
}
