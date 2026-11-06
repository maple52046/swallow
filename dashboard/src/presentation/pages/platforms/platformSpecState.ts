import { ConditionTypes, findCondition } from '@/domain/convergence/types'
import type { Platform } from '@/domain/platform/types'

/** A Platform's spec state in words: one summary and the lines that explain it, most important first. */
export interface PlatformSpecStateText {
  summary: string
  details: string[]
}

/**
 * A Condition message as a sentence inside the notice: the API writes messages without a final
 * period, so one is added unless the message already ends a sentence; an empty one is `fallback`.
 */
function asSentence(message: string, fallback: string): string {
  const text = message.trim()
  if (!text) return fallback
  return /[.!?]$/.test(text) ? text : `${text}.`
}

/**
 * The Platform page's notice for an install or uninstall that has not started yet: its tone, words,
 * and whether it links to Change Plans.
 */
export interface PlatformChangeNotice {
  status: 'info' | 'warning'
  title: string
  body: string
  /** The change's Change Plan waits for a user, or was ended by one, so the notice offers the way to Change Plans. */
  linksChangePlans: boolean
}

/**
 * Words for a `planned` Platform, whose install has not started (platforms.md "Platform
 * Resource"), from its `realized` Condition for the current generation: waiting for approval (the
 * Change Plan policy or a blocker kept a confirmed install waiting, and the message says which),
 * needing a new plan (rejected or expired), or about to start (approved with its request). Before
 * the controller judged the generation the notice only says that nothing started, since the
 * dashboard cannot tell which it is.
 */
export function plannedInstallNotice(platform: Platform): PlatformChangeNotice {
  const realized = findCondition(platform.status.conditions, ConditionTypes.realized)
  const judged = realized !== undefined && realized.observedGeneration >= platform.generation
  if (judged && realized.reason === 'awaiting_approval') {
    return {
      status: 'warning',
      title: 'Install waits for approval',
      body: `Nothing is installed yet. ${asSentence(realized.message, 'Its Change Plan waits for approval.')} Approve it in Change Plans to build ${platform.name}.`,
      linksChangePlans: true,
    }
  }
  if (judged && realized.reason === 'plan_required') {
    return {
      status: 'warning',
      title: 'Install needs a new Change Plan',
      body: `Nothing is installed yet. ${asSentence(realized.message, 'Its Change Plan was rejected or expired.')}`,
      linksChangePlans: true,
    }
  }
  if (judged && realized.reason === 'converging') {
    return {
      status: 'info',
      title: 'Install is starting',
      body: `Its Change Plan is approved. Swallow starts building ${platform.name} in a moment; this page updates on its own.`,
      linksChangePlans: false,
    }
  }
  return {
    status: 'info',
    title: 'Install has not started',
    body: `Nothing is installed yet. This page updates when Swallow starts building ${platform.name} or when its Change Plan needs approval.`,
    linksChangePlans: false,
  }
}

/**
 * Words for a Platform marked absent whose uninstall has not started (platforms.md "Delete A
 * Platform"), from its `realized` Condition for the current generation, or null when there is
 * nothing to say here: the spec is present, or the uninstall runs, failed, or succeeded, which the
 * lifecycle notice explains.
 *
 * - `awaiting_approval`: the Change Plan policy or a blocker kept the confirmed uninstall waiting;
 *   the message says which, and the notice links to Change Plans.
 * - `plan_required`: the plan was rejected or expired, so the user chooses Uninstall again, which
 *   proposes a new one.
 * - `converging`: approved with its request and about to start, or held until a deployment that
 *   still runs ends.
 * - `failed` before any uninstall Workflow ran: the approved plan could not start it (a target is
 *   locked, missing, or busy); the controller keeps trying.
 *
 * Before the controller judged the generation the notice only says that nothing started, since the
 * dashboard cannot tell which it is.
 */
export function pendingUninstallNotice(platform: Platform): PlatformChangeNotice | null {
  const state = platform.lifecycleState
  if (platform.spec?.desired !== 'absent' || state === 'uninstalling' || state === 'uninstall_failed' || state === 'uninstalled') {
    return null
  }
  const realized = findCondition(platform.status.conditions, ConditionTypes.realized)
  const judged = realized !== undefined && realized.observedGeneration >= platform.generation
  if (judged && realized.reason === 'awaiting_approval') {
    return {
      status: 'warning',
      title: 'Uninstall waits for approval',
      body: `Nothing was removed yet. ${asSentence(realized.message, 'Its Change Plan waits for approval.')} Approve it in Change Plans to uninstall ${platform.name}.`,
      linksChangePlans: true,
    }
  }
  if (judged && realized.reason === 'plan_required') {
    return {
      status: 'warning',
      title: 'Uninstall needs a new Change Plan',
      body: 'Nothing was removed. Its Change Plan was rejected or expired. Choose Uninstall again to propose a new one.',
      linksChangePlans: true,
    }
  }
  if (judged && realized.reason === 'converging') {
    return {
      status: 'info',
      title: 'Uninstall is starting',
      body: state === 'deploying'
        ? `Its Change Plan is approved. Swallow uninstalls ${platform.name} once its deployment ends; this page updates on its own.`
        : `Its Change Plan is approved. Swallow starts removing ${platform.name} in a moment; this page updates on its own.`,
      linksChangePlans: false,
    }
  }
  if (judged && realized.reason === 'failed') {
    return {
      status: 'warning',
      title: 'Uninstall cannot start yet',
      body: `${asSentence(realized.message, 'Swallow could not start the uninstall.')} Swallow keeps trying; resolve the cause, or choose Uninstall again with other options.`,
      linksChangePlans: false,
    }
  }
  return {
    status: 'info',
    title: 'Uninstall has not started',
    body: `Nothing was removed yet. This page updates when Swallow starts removing ${platform.name} or when its Change Plan needs approval.`,
    linksChangePlans: false,
  }
}

/**
 * Words for where a Platform's spec stands (platforms.md "Spec And Status"): its `generation`
 * against `status.observedGeneration` (the generation the applied spec realized) and its shared
 * `realized` and `drifted` Conditions, for the Platform details card.
 *
 * - A `realized` judgement made for an older generation describes a spec that has changed since,
 *   so the Platform reads as converging to the new generation until the controller judges it.
 * - The Platform reasons are worded for the user: `awaiting_approval` (a Change Plan waits for
 *   approval; the message says why), `plan_required` (the plan was rejected or expired; write the
 *   spec again), `failed` (not started again for the same generation), `converging` (a Workflow
 *   runs, or an approved plan is about to start one). An unknown reason is shown as sent, never hidden.
 * - Drift is its own line, with the controller's message naming the members it found.
 *
 * The Server convergence presenter (`describeConvergence`) is not reused: its states and words are
 * the provisioner's (held, provider missing), and it has no Change Plan reasons.
 */
export function describePlatformSpecState(platform: Platform): PlatformSpecStateText {
  const { generation, status } = platform
  if (!platform.spec || generation === 0) {
    return {
      summary: 'Not adopted yet',
      details: ['Swallow rebuilds the spec from the latest deployment at its next observation.'],
    }
  }
  const realized = findCondition(status.conditions, ConditionTypes.realized)
  const drifted = findCondition(status.conditions, ConditionTypes.drifted)
  const judged = realized !== undefined && realized.observedGeneration >= generation
  let summary: string
  const details: string[] = []
  if (judged && realized.status === 'false') {
    switch (realized.reason) {
      case 'awaiting_approval':
        summary = `Awaiting approval (generation ${generation})`
        details.push(realized.message || 'Its Change Plan waits for approval in Change Plans.')
        break
      case 'plan_required':
        summary = `Change Plan required (generation ${generation})`
        details.push(realized.message || 'The Change Plan was rejected or expired. Write the spec again to propose a new one.')
        break
      case 'failed':
        summary = `Failed to apply generation ${generation}`
        details.push(realized.message || 'Rerun the Workflow or write a new spec.')
        break
      case 'converging':
        summary = `Converging to generation ${generation}`
        if (realized.message) details.push(realized.message)
        break
      default:
        summary = `Not applied (generation ${generation}): ${realized.reason}`
        if (realized.message) details.push(realized.message)
    }
  } else if (status.observedGeneration >= generation && (!judged || realized.status === 'true')) {
    summary = `Applied (generation ${generation})`
  } else {
    summary = `Converging to generation ${generation}`
    if (status.observedGeneration > 0) details.push(`Generation ${status.observedGeneration} is applied.`)
  }
  if (drifted?.status === 'true') details.push(`Drifted: ${drifted.message || drifted.reason}`)
  return { summary, details }
}
