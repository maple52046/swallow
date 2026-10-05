import type { BootMediaApply } from '@/domain/server/types'

/** Whether a step of the preflight is finished, running now, or still ahead. */
export type BootMediaApplyStepStatus = 'done' | 'current' | 'pending'

/** One row of the preflight's step list. */
export interface BootMediaApplyStep {
  key: string
  label: string
  status: BootMediaApplyStepStatus
}

/** What the progress block shows for one moment of a preflight. */
export interface BootMediaApplyProgressModel {
  /** Whole percent, 0–99: the bar never claims completion; the API's answer does. */
  percent: number
  steps: BootMediaApplyStep[]
  /** Milliseconds since the preflight started. */
  elapsedMs: number
  /** Milliseconds left in the settle wait, only while it runs. */
  settleLeftMs: number | null
  /** The current step's sentence for screen readers and the bar's value text. */
  summary: string
}

// The steps as the operator reads them, with each one's typical duration on tainan-ci (AMI
// MegaRAC), which weights the bar. `ejecting` shares the first step: it only happens on a switch
// and takes seconds, and listing a step that is usually skipped would read as a skipped failure.
// The settle wait is fixed by the API (three minutes) and fills by its real end time.
const STEPS: ReadonlyArray<{ key: string; label: string; phases: readonly string[]; seconds: number }> = [
  { key: 'check', label: 'Check the BMC', phases: ['probing', 'ejecting'], seconds: 20 },
  { key: 'mount', label: 'Mount the Boot ISO as a virtual CD', phases: ['mounting'], seconds: 50 },
  { key: 'settle', label: 'Let the BMC settle the mount', phases: ['settling'], seconds: 180 },
  { key: 'direct', label: 'Direct the next boots at the virtual CD', phases: ['directing'], seconds: 30 },
  { key: 'verify', label: 'Read both back from the BMC', phases: ['verifying'], seconds: 20 },
]

const TOTAL_SECONDS = STEPS.reduce((sum, step) => sum + step.seconds, 0)

/** The longest a step's share of the bar fills before the API moves on, so the bar never stalls at a boundary it has not reached. */
const UNTIMED_STEP_CAP = 0.9

/**
 * The progress of a running Boot Media preflight at `now` (epoch milliseconds).
 *
 * `apply` is the API's record (`null` before the first poll answers: the request was just sent,
 * so it is the first step since `requestedAt`). Steps before the current one read as done — a
 * skipped mount (the BMC already held the ISO) is done too, since nothing is left to do there. A
 * step's share of the bar fills by its typical duration, capped below its end, except the settle
 * wait, which fills by its real end time. An unknown future phase keeps every step pending and
 * fills by elapsed time alone. The result never reaches 100%: completion is the API's answer.
 */
export function bootMediaApplyProgress(apply: BootMediaApply | null, requestedAt: number, now: number): BootMediaApplyProgressModel {
  const started = parseTime(apply?.startedAt) ?? requestedAt
  const phaseStarted = parseTime(apply?.phaseStartedAt) ?? started
  const phaseEnds = parseTime(apply?.phaseEndsAt ?? undefined)
  const elapsedMs = Math.max(0, now - started)
  const phase = apply?.phase ?? 'probing'
  const index = STEPS.findIndex((step) => step.phases.includes(phase))

  if (index < 0) {
    return {
      percent: clampPercent(elapsedMs / 1_000 / TOTAL_SECONDS),
      steps: STEPS.map((step) => ({ key: step.key, label: step.label, status: 'pending' })),
      elapsedMs,
      settleLeftMs: null,
      summary: 'Applying Boot Media through the BMC',
    }
  }

  const step = STEPS[index]
  const inPhaseMs = Math.max(0, now - phaseStarted)
  let within = Math.min(inPhaseMs / 1_000 / step.seconds, UNTIMED_STEP_CAP)
  let settleLeftMs: number | null = null
  if (phase === 'settling' && phaseEnds !== null && phaseEnds > phaseStarted) {
    within = Math.min(1, inPhaseMs / (phaseEnds - phaseStarted))
    settleLeftMs = Math.max(0, phaseEnds - now)
  }
  const doneSeconds = STEPS.slice(0, index).reduce((sum, earlier) => sum + earlier.seconds, 0)
  const label = phase === 'ejecting' ? 'Eject the previous Boot ISO' : step.label
  return {
    percent: clampPercent((doneSeconds + within * step.seconds) / TOTAL_SECONDS),
    steps: STEPS.map((candidate, position) => ({
      key: candidate.key,
      label: position === index ? label : candidate.label,
      status: position < index ? 'done' : position === index ? 'current' : 'pending',
    })),
    elapsedMs,
    settleLeftMs,
    summary: `Step ${index + 1} of ${STEPS.length}: ${label}`,
  }
}

function parseTime(value: string | undefined): number | null {
  if (!value) return null
  const time = Date.parse(value)
  return Number.isNaN(time) ? null : time
}

function clampPercent(fraction: number): number {
  return Math.max(0, Math.min(99, Math.floor(fraction * 100)))
}
