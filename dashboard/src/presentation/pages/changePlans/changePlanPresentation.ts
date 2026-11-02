import type { ChangePlan, ChangePlanResource, ChangePlanState, PlannedAction } from '@/domain/convergence/changePlan'

/** User-facing words for each plan state; the badge shows these, never the colour alone. */
export const CHANGE_PLAN_STATE_LABELS: Record<ChangePlanState, string> = {
  pending_approval: 'Waiting for approval',
  approved: 'Approved',
  applied: 'Applied',
  rejected: 'Rejected',
  superseded: 'Superseded',
  expired: 'Expired',
}

/**
 * Names a resource for people: "Platform p1". The kind is the API's resource kind; an unknown kind
 * is shown as sent rather than guessed.
 */
export function changePlanResourceLabel(resource: ChangePlanResource): string {
  const kind = resource.kind ? resource.kind.charAt(0).toUpperCase() + resource.kind.slice(1) : 'Resource'
  return `${kind} ${resource.id}`
}

/** The change an action makes, from the current to the desired value when the API gave them. */
export function plannedActionChange(action: PlannedAction): string {
  if (action.from && action.to) return `${action.from} → ${action.to}`
  if (action.to) return `→ ${action.to}`
  if (action.from) return `${action.from} → (removed)`
  return '—'
}

/** How many of a plan's actions are destructive, for the approval consequence statement. */
export function destructiveActionCount(plan: ChangePlan): number {
  return plan.actions.filter((action) => action.destructive).length
}

/** Plans grouped the way a user works through them: decide, wait for the controller, history. */
export interface ChangePlanGroups {
  pending: ChangePlan[]
  approved: ChangePlan[]
  finished: ChangePlan[]
}

/** Splits plans into the page's three sections, keeping the API's newest-first order. */
export function groupChangePlans(plans: readonly ChangePlan[]): ChangePlanGroups {
  return {
    pending: plans.filter((plan) => plan.state === 'pending_approval'),
    approved: plans.filter((plan) => plan.state === 'approved'),
    finished: plans.filter((plan) => plan.state !== 'pending_approval' && plan.state !== 'approved'),
  }
}
