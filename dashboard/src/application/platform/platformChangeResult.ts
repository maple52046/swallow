import type { ChangePlan } from '@/domain/convergence/changePlan'
import type { Platform, PlatformChangeResult } from '@/domain/platform/types'

/** Contract shape returned by Platform spec writes before an adapter reduces it for a caller. */
export interface PlatformSpecWrite {
  platform: Platform
  plan: ChangePlan | null
}

/**
 * Reduces a spec write to the navigation and approval facts used by generic and module mutations.
 * Keeping this mapping in application code prevents module adapters from importing one another or
 * the generic Platform HTTP adapter merely to interpret the shared Change Plan envelope.
 */
export function toPlatformChangeResult(written: PlatformSpecWrite): PlatformChangeResult {
  const plan = written.plan
  const awaitingApproval = plan?.state === 'pending_approval'
  return {
    platformId: written.platform.id,
    planId: plan?.id ?? null,
    deleted: false,
    awaitingApproval,
    waitReason: awaitingApproval ? plan.stateReason : '',
    blocked: awaitingApproval && plan.blockers.length > 0,
  }
}

/** Spec writes from confirmed dashboard dialogs approve their own Change Plan on submission. */
export const APPROVE_PLATFORM_WRITE = 'approve=true'
