import { ConditionTypes, findCondition } from '@/domain/convergence/types'
import type { Platform } from '@/domain/platform/types'

/** A Platform's spec state in words: one summary and the lines that explain it, most important first. */
export interface PlatformSpecStateText {
  summary: string
  details: string[]
}

/**
 * Words for where a Platform's spec stands (platforms.md "Spec And Status"): its `generation`
 * against `status.observedGeneration` (the generation the applied spec realized) and its shared
 * `realized` and `drifted` Conditions, for the Platform details card.
 *
 * - A `realized` judgement made for an older generation describes a spec that has changed since,
 *   so the Platform reads as converging to the new generation until the controller judges it.
 * - The Platform reasons are worded for the user: `awaiting_approval` (a Change Plan waits),
 *   `plan_required` (the plan was rejected or expired; write the spec again), `failed` (not started
 *   again for the same generation), `converging`. An unknown reason is shown as sent, never hidden.
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
