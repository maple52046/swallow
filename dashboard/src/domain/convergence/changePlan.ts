/**
 * Change Plan domain types (root glossary: Change Plan; decision 076; contract `change-plans.md`).
 *
 * A Change Plan previews what realizing a destructive spec change will do. An admin approves or
 * rejects it as a whole; the resource's controller then recomputes and applies it as a Workflow.
 * The dashboard never derives a plan's actions, destructiveness, or state: every value here is the
 * API's (dashboard architecture spec, "Desired and observed state").
 */

/**
 * Where a plan is in its life. `pending_approval` and `approved` are unfinished — a resource has
 * at most one such plan; the others are final.
 */
export type ChangePlanState = 'pending_approval' | 'approved' | 'applied' | 'rejected' | 'superseded' | 'expired'

/** Names the resource a plan, or one of its actions, is about (for example a Platform or a Server). */
export interface ChangePlanResource {
  kind: string
  id: string
}

/** One thing realizing the spec will do to one target, as the API computed it. */
export interface PlannedAction {
  target: ChangePlanResource
  /** The change, for example `uninstall` or `remove-member`. */
  action: string
  /** Current and desired values for people; empty when there is none. */
  from: string
  to: string
  destructive: boolean
  reason: string
}

/**
 * A Change Plan as the API reports it. `blockers` are failed preflight checks (a plan with
 * blockers cannot be approved); `decidedBy`/`decidedAt` name who approved or rejected it;
 * `workflowId` is set once the controller applied it.
 */
export interface ChangePlan {
  id: string
  resource: ChangePlanResource
  /** The spec generation the plan realizes. */
  generation: number
  /** When the observed state the plan relied on was read (ISO 8601). */
  observedAt: string
  actions: PlannedAction[]
  effects: string[]
  blockers: string[]
  state: ChangePlanState
  stateReason: string
  decidedBy: string
  decidedAt: string | null
  workflowId: string
  expiresAt: string
  createdAt: string
  updatedAt: string
}

/** Whether the plan still waits for a decision or for its controller. */
export function isChangePlanUnfinished(plan: ChangePlan): boolean {
  return plan.state === 'pending_approval' || plan.state === 'approved'
}

/**
 * Why the plan cannot be approved now, or `null` when it can. Mirrors the contract's 409 rules so
 * the button can say why it is disabled; the API still decides.
 */
export function changePlanApprovalBlocker(plan: ChangePlan, now: number): string | null {
  if (plan.state !== 'pending_approval') return 'Only a plan waiting for approval can be approved.'
  if (plan.blockers.length > 0) return 'Resolve the blockers first; a plan with blockers cannot be approved.'
  if (Date.parse(plan.expiresAt) <= now) return 'This plan has expired; change the spec again for a new plan.'
  return null
}
