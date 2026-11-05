import type { PlatformChangeResult } from '@/domain/platform/types'

/**
 * Copy for a change the user confirmed whose Change Plan still waits for a separate approval
 * (contract `change-plans.md`, "Approval On Submit"): the Change Plan policy requires one, or the
 * plan has blockers. Shared by every screen that submits a confirmed spec write — the deploy
 * wizard, Platform uninstall and its bulk form, and module sections such as the GPU Operator — so
 * they all say why and where to go in the same words.
 */

/**
 * The toast description: why the plan waits, in the API's words (`waitReason`, the plan's
 * `stateReason`), then what to do in Change Plans — approve it to carry out `outcome` (for example
 * "build the platform"), or, for a blocked plan that cannot be approved, read its blockers. An
 * empty reason, which the contract does not produce for a waiting plan, falls back to a generic
 * sentence instead of an empty one.
 */
export function changePlanWaitDescription(result: Pick<PlatformChangeResult, 'waitReason' | 'blocked'>, outcome: string): string {
  const reason = result.waitReason.trim()
  const why = reason ? `${reason.charAt(0).toUpperCase()}${reason.slice(1)}.` : 'Its Change Plan waits for approval.'
  return result.blocked
    ? `${why} See its blockers in Change Plans.`
    : `${why} Approve it in Change Plans to ${outcome}.`
}
