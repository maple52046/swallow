import type { Condition } from '@/domain/convergence/types'
import type { ModuleKind, ModuleSpecSchema } from '@/domain/module/types'

/**
 * Validation (decision 081, `docs/development/validation.md`): Validation Suites, the runs that
 * execute them, and the schedules that start runs. The shapes are the validation contract's
 * (`validation.md`), which the adapter returns as they are; every value here is the API's, and the
 * dashboard derives no verdict, status, or condition of its own.
 */

/** What a suite validates: one or more deployed Servers, or one Platform (glossary Validation Suite). */
export type ValidationTargetKind = 'servers' | 'platform'

/**
 * How much a suite may disturb its targets (glossary Validation Impact): `observe` only reads,
 * `workload` runs short work that competes with the targets' own, and `disruptive` may interrupt
 * service, so a run of it must be confirmed and no schedule may start it.
 */
export type ValidationImpact = 'observe' | 'workload' | 'disruptive'

/** Where a suite comes from: a module of the build (read-only) or an admin's script. */
export type ValidationSuiteSource = 'module' | 'user'

/** The account a user suite's script runs as. */
export type ValidationRunAs = 'automation-user' | 'root'

/** One check of a suite (glossary Validation Check). `gating` matters only when a deployment composes the suite. */
export interface ValidationCheck {
  id: string
  title: string
  description: string
  gating: boolean
}

/** The module that declares a module suite. */
export interface ValidationSuiteModule {
  kind: ModuleKind
  id: string
  version: string
}

/**
 * A Validation Suite of the catalog. A module suite has `module`, maybe a `parameters` schema and a
 * `platformType`, and null user fields; a user suite has `script`, `runAs`, `timeoutSeconds`,
 * `revision`, and `scriptSha256`, and its one check is `script` (its exit code is the verdict).
 */
export interface ValidationSuite {
  id: string
  source: ValidationSuiteSource
  module: ValidationSuiteModule | null
  name: string
  description: string
  target: ValidationTargetKind
  platformType: string | null
  impact: ValidationImpact
  checks: ValidationCheck[]
  /** The run parameters the suite accepts, in the modules area's spec schema subset; null takes none. */
  parameters: ModuleSpecSchema | null
  /** How long one report run may take before it is canceled. */
  budgetSeconds: number
  script: string | null
  runAs: ValidationRunAs | null
  timeoutSeconds: number | null
  revision: number | null
  scriptSha256: string | null
  /**
   * A module suite offered as a preview whose checks may still change. It runs like any other; the
   * dashboard offers it only when the admin shows experimental suites. Always false for a user suite.
   */
  experimental: boolean
  retired: boolean
  createdAt: string | null
  updatedAt: string | null
  createdBy: string | null
}

/** A user suite as an admin writes it; omitted fields take the API's defaults. */
export interface UserSuiteInput {
  name: string
  description?: string
  script: string
  runAs?: ValidationRunAs
  timeoutSeconds?: number
  impact?: ValidationImpact
}

/** A run's lifecycle: accepted, executing, ended with a verdict, or canceled without one. */
export type ValidationRunStatus = 'pending' | 'running' | 'completed' | 'canceled'

/** A completed run's verdict: any failed check fails it, otherwise any error makes it an error. */
export type ValidationRunVerdict = 'passed' | 'failed' | 'error'

/** One check's verdict on one target (glossary Validation Verdict). */
export type ValidationVerdict = 'passed' | 'failed' | 'error' | 'skipped'

/** `report` runs on its own; `gate` runs inside a deployment and can stop it. */
export type ValidationRunMode = 'report' | 'gate'

/** What started a run. */
export type ValidationTriggerKind = 'manual' | 'schedule' | 'deployment'

/** The suite a run ran, as it was when the run was accepted. */
export interface ValidationRunSuite {
  id: string
  source: ValidationSuiteSource
  name: string
  version: string | null
  revision: number | null
  scriptSha256: string | null
}

/** A run's target: the Servers (or the Platform's access Servers) and the Platform, if any. */
export interface ValidationRunTarget {
  kind: ValidationTargetKind
  serverIds: string[]
  platformId: string | null
}

/** What started a run: a user, a schedule, or a deployment (the gate's or the schedule's trigger). */
export interface ValidationRunTrigger {
  kind: ValidationTriggerKind
  scheduleId: string | null
  workflowId: string | null
}

/** How many results have each verdict. */
export interface ValidationRunCounts {
  passed: number
  failed: number
  error: number
  skipped: number
}

/** One value a check measured, with what it expected. */
export interface ValidationMeasurement {
  name: string
  value: number
  unit: string
  expected: string
}

/** The Server or Platform one result judges. */
export interface ValidationResultTarget {
  kind: 'server' | 'platform'
  id: string
}

/** One check's verdict on one target, with what the host printed (bounded and redacted). */
export interface ValidationResult {
  check: string
  target: ValidationResultTarget
  verdict: ValidationVerdict
  summary: string
  measurements: ValidationMeasurement[]
  evidence: string
  observedAt: string
  /** The Task that judged it; null for an `error` swallow recorded because no Task did. */
  taskId: string | null
}

/** A run as the list returns it: everything but the results. */
export interface ValidationRunSummary {
  id: string
  siteId: string
  suite: ValidationRunSuite
  checks: ValidationCheck[]
  target: ValidationRunTarget
  parameters: Record<string, unknown>
  mode: ValidationRunMode
  trigger: ValidationRunTrigger
  /** The Workflow that executes the run (its own, or its deployment's in gate mode). */
  workflowId: string
  status: ValidationRunStatus
  statusReason: string | null
  /** Null until the run completes, and for a canceled run. */
  verdict: ValidationRunVerdict | null
  counts: ValidationRunCounts
  requestedBy: string
  requestedAt: string
  startedAt: string | null
  finishedAt: string | null
}

/** A run with every result. */
export interface ValidationRun extends ValidationRunSummary {
  results: ValidationResult[]
}

/** A request to run a suite: Servers for a `servers` suite, a Platform for a `platform` one. */
export interface ValidationRunInput {
  suiteId: string
  serverIds?: string[]
  platformId?: string
  parameters?: Record<string, unknown>
  /** Acknowledges a `disruptive` suite's impact; the API refuses such a run without it. */
  confirm?: boolean
}

/** When a schedule starts runs: on a cron expression, or after each deployment of its target. */
export type ValidationScheduleTriggerKind = 'cron' | 'after-deployment'

/** A schedule's trigger; `cron` and `timezone` belong to a `cron` trigger only. */
export interface ValidationScheduleTrigger {
  kind: ValidationScheduleTriggerKind
  cron: string | null
  timezone: string | null
}

/** A schedule's desired state, written only through the API. */
export interface ValidationScheduleSpec {
  name: string
  suiteId: string
  serverIds: string[]
  platformId: string | null
  parameters: Record<string, unknown>
  trigger: ValidationScheduleTrigger
  enabled: boolean
}

/** What the worker's `validation-schedule` controller observed and did. */
export interface ValidationScheduleStatus {
  observedGeneration: number
  nextRunAt: string | null
  lastRunId: string | null
  lastRunAt: string | null
  /** Why the latest occurrence started no run (a busy, locked, or not deployed target). */
  lastSkipReason: string | null
  conditions: Condition[]
}

/** A Validation Schedule (glossary): desired spec and observed status. */
export interface ValidationSchedule {
  id: string
  siteId: string
  generation: number
  spec: ValidationScheduleSpec
  status: ValidationScheduleStatus
  createdAt: string
  updatedAt: string
}

/** A schedule as an admin writes it; `generation` makes a replace apply only to the spec it read. */
export interface ValidationScheduleInput {
  name: string
  suiteId: string
  serverIds?: string[]
  platformId?: string
  parameters?: Record<string, unknown>
  trigger: { kind: ValidationScheduleTriggerKind; cron?: string; timezone?: string }
  enabled: boolean
  generation?: number
}

/** Whether a run has not ended yet, so its view keeps following it. */
export function isValidationRunActive(status: ValidationRunStatus): boolean {
  return status === 'pending' || status === 'running'
}
