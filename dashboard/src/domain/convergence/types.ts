/**
 * Convergence of a migrated resource (docs/decisions/071): swallow owns the resource's `spec`, a
 * controller in the backend makes the provider match it, and the controller reports its judgement as
 * conditions in the resource's `status`. The dashboard only reads conditions; it never writes them.
 *
 * See docs/development/glossaries/terms/condition.md, controller.md, and hold.md.
 */

/** A condition's truth value; `unknown` until the controller can judge it. */
export type ConditionStatus = 'true' | 'false' | 'unknown'

/**
 * One named judgement in a resource's status. `type` is `realized`, `drifted`,
 * `requires_attention`, `held`, or a resource-specific type, and `reason` a short code
 * (servers-list.md, "Spec and status"). Both are open strings: a newer backend may send values this
 * build does not know, which must be shown as-is rather than refused. `message` is display text
 * only. `observedGeneration` is the spec generation the judgement was made for, so a condition
 * older than the resource's `generation` describes a spec that has since changed.
 */
export interface Condition {
  type: string
  status: ConditionStatus
  reason: string
  message: string
  lastTransitionTime: string
  observedGeneration: number
}

/** The condition types every migrated resource reports. */
export const ConditionTypes = {
  realized: 'realized',
  drifted: 'drifted',
  requiresAttention: 'requires_attention',
  held: 'held',
} as const

/** The condition of the given type, or undefined before the controller first judged it. */
export function findCondition(conditions: readonly Condition[], type: string): Condition | undefined {
  return conditions.find((condition) => condition.type === type)
}
