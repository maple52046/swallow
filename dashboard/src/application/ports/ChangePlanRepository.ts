import type { ChangePlan, ChangePlanState } from '@/domain/convergence/changePlan'

/** Narrows a Change Plan listing; omitted fields match every plan. */
export interface ChangePlanFilter {
  resourceKind?: string
  resourceId?: string
  states?: readonly ChangePlanState[]
}

/**
 * Application port for Change Plans (decision 076, contract `change-plans.md`).
 *
 * Every call needs the `admin` role. Implementations map the provider contract only and reject
 * with the shared error envelope: `not_found` for an unknown plan, and `conflict` for approving a
 * plan that is not pending, has blockers, or has expired (which the API then expires), or for
 * rejecting one that is already final. Approval is of the whole plan; the controller applies it
 * afterwards, so an approved plan is not yet `applied`.
 */
export interface ChangePlanRepository {
  /** Plans matching the filter, newest first. */
  listChangePlans(filter?: ChangePlanFilter): Promise<ChangePlan[]>
  /** One plan by id. */
  getChangePlan(id: string): Promise<ChangePlan>
  /** Approves a pending plan as a whole and returns it as `approved`. */
  approveChangePlan(id: string): Promise<ChangePlan>
  /** Rejects an unfinished plan and returns it as `rejected`. */
  rejectChangePlan(id: string): Promise<ChangePlan>
}
