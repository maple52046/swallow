import type { ChangePlan, ChangePlanResource, ChangePlanState, PlannedAction } from '@/domain/convergence/changePlan'
import { plural } from '@/shared/utils/plural'

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
 * Display names for the resources the page's plans refer to, keyed by id per resource kind. The
 * contract names a resource only by `{kind, id}`; these come from the Platform and Server reads
 * (`useChangePlanNames`) and are presentation only. A missing entry (not read yet, deleted, a
 * failed read, or a kind without a lookup) means the id is shown instead.
 */
export interface ChangePlanNames {
  platforms: ReadonlyMap<string, string>
  servers: ReadonlyMap<string, string>
}

/** The resource kinds whose names the page looks up; the contract's kinds for Platforms and Servers. */
export type NamedResourceKind = 'platform' | 'server'

/**
 * The resource's display name, or `null` when none is known, in which case callers show the id.
 * A name equal to the id (a Server with no hostname or FQDN) counts as no name, so the id is not
 * shown twice.
 */
export function changePlanResourceName(resource: ChangePlanResource, names: ChangePlanNames): string | null {
  const lookup = resource.kind === 'platform' ? names.platforms : resource.kind === 'server' ? names.servers : null
  const name = lookup?.get(resource.id)
  return name && name !== resource.id ? name : null
}

/**
 * The resource kind as a capitalised word ("Platform", "Server"). The kind is the API's; an
 * unknown kind is shown as sent rather than guessed, and a missing one as "Resource".
 */
export function changePlanResourceKind(resource: ChangePlanResource): string {
  return resource.kind ? resource.kind.charAt(0).toUpperCase() + resource.kind.slice(1) : 'Resource'
}

/**
 * Names a resource for people: "Platform research-slurm", or "Platform p1" while its name is
 * unknown. Card and table accessible names use this label, so they follow the name once it loads.
 */
export function changePlanResourceLabel(resource: ChangePlanResource, names: ChangePlanNames): string {
  return `${changePlanResourceKind(resource)} ${changePlanResourceName(resource, names) ?? resource.id}`
}

/**
 * The distinct ids of `kind` resources that the plans name, as plan resources or action targets,
 * sorted so the lookup key does not change when the API reorders plans. Empty ids are skipped.
 */
export function changePlanResourceIds(plans: readonly ChangePlan[], kind: NamedResourceKind): string[] {
  const ids = new Set<string>()
  for (const plan of plans) {
    if (plan.resource.kind === kind && plan.resource.id) ids.add(plan.resource.id)
    for (const action of plan.actions) {
      if (action.target.kind === kind && action.target.id) ids.add(action.target.id)
    }
  }
  return [...ids].sort()
}

/** The change an action makes, from the current to the desired value when the API gave them. */
export function plannedActionChange(action: PlannedAction): string {
  if (action.from && action.to) return `${action.from} → ${action.to}`
  if (action.to) return `→ ${action.to}`
  if (action.from) return `${action.from} → (removed)`
  return '—'
}

/**
 * The approval confirmation's scope statement: the resource and, when the API marked any action
 * destructive, how many ("including 1 destructive action"). A plan without destructive actions
 * gets no count clause rather than "including 0 destructive actions".
 */
export function changePlanApprovalScope(plan: ChangePlan, names: ChangePlanNames): string {
  const destructive = plan.actions.filter((action) => action.destructive).length
  const including = destructive > 0 ? `, including ${destructive} destructive ${plural(destructive, 'action')}` : ''
  return `Swallow will carry out every action of this plan for ${changePlanResourceLabel(plan.resource, names)}${including}.`
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
