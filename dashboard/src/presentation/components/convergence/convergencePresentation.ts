import { ConditionTypes, findCondition, type Condition } from '@/domain/convergence/types'

/**
 * The one state an operator needs from a migrated resource's conditions, most urgent first. Each
 * maps to a `StatusBadge` status for its colour family; the label always carries the meaning.
 */
export type ConvergenceState =
  | 'requires_attention'
  | 'held'
  | 'missing'
  | 'unsupported'
  | 'drifted'
  | 'retrying'
  | 'converging'
  | 'in_sync'
  | 'pending'

/** What `ConvergenceStatus` shows: a short label, the badge colour family, and optional detail text. */
export interface ConvergenceSummary {
  state: ConvergenceState
  label: string
  /** The `StatusBadge` status whose colour family this state uses. */
  badgeStatus: string
  /** The controller's message or swallow's explanation; empty when the label says it all. */
  detail: string
}

const HOLD_DETAIL: Record<string, string> = {
  server_locked: 'The Server is locked in its provisioner; changes wait until it is unlocked.',
  rescue_mode: 'The Server is in Rescue Mode; changes wait until it leaves.',
  workflow_lease: 'A running Workflow holds this Server; changes wait until it finishes.',
}

/**
 * Reduces a resource's conditions to one operator-facing state (servers-list.md, "Spec and
 * status"). Precedence follows what the operator must act on first: a stopped convergence, then a
 * hold, then a provisioner that cannot realize the spec, then drift, then transient progress.
 * Conditions judged for an older generation than `generation` describe a spec that has since
 * changed, so the resource reads as converging until the controller judges the new spec.
 * Unknown reasons fall through to the generic states, so a newer backend never breaks the view.
 */
export function describeConvergence(conditions: readonly Condition[], generation: number): ConvergenceSummary {
  if (conditions.length === 0) {
    return { state: 'pending', label: 'Pending', badgeStatus: 'pending', detail: 'Swallow has not checked the provisioner yet.' }
  }
  const attention = findCondition(conditions, ConditionTypes.requiresAttention)
  if (attention?.status === 'true') {
    return { state: 'requires_attention', label: 'Needs attention', badgeStatus: 'requires_attention', detail: attention.message || 'The provisioner refused the change. Change the setting to try again.' }
  }
  const held = findCondition(conditions, ConditionTypes.held)
  const realized = findCondition(conditions, ConditionTypes.realized)
  const current = realized !== undefined && realized.observedGeneration >= generation
  if (held?.status === 'true' && !(current && realized.status === 'true')) {
    return { state: 'held', label: 'Held', badgeStatus: 'suppressed', detail: HOLD_DETAIL[held.reason] ?? held.message }
  }
  if (realized?.status === 'false' && realized.reason === 'provider_missing') {
    return { state: 'missing', label: 'Not in provisioner', badgeStatus: 'offline', detail: realized.message }
  }
  if (realized?.status === 'false' && realized.reason === 'capability_missing') {
    return { state: 'unsupported', label: 'Not supported', badgeStatus: 'unknown', detail: realized.message || 'The provisioner cannot apply this setting; swallow keeps it.' }
  }
  const drifted = findCondition(conditions, ConditionTypes.drifted)
  if (drifted?.status === 'true') {
    return { state: 'drifted', label: 'Drift detected', badgeStatus: 'warning', detail: `${drifted.message} Swallow corrects it at its next periodic check.`.trim() }
  }
  if (realized?.status === 'false' && realized.reason === 'provider_unreachable') {
    return { state: 'retrying', label: 'Retrying', badgeStatus: 'degraded', detail: realized.message }
  }
  if (current && realized.status === 'true') {
    return { state: 'in_sync', label: 'In sync', badgeStatus: 'ok', detail: '' }
  }
  return { state: 'converging', label: 'Applying', badgeStatus: 'running', detail: 'The provisioner is being updated to match.' }
}
