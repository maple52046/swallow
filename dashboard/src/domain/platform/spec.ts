import { ConditionTypes, findCondition } from '@/domain/convergence/types'
import type { Platform, PlatformSpec } from './types'

/**
 * Whether the platform controller is about to carry out a newer spec on its own (platforms.md
 * "Spec And Status"): the spec's generation is newer than the one last applied, and the `realized`
 * Condition for it is `converging` — an approved Change Plan about to start its Workflow, or a
 * Workflow running — or has not been judged for that generation yet. A Condition that waits for a
 * user (`awaiting_approval`, `plan_required`) or a failure is not, so a screen that polls while
 * this holds stops there instead of polling for a decision nobody is making.
 */
export function platformConvergesWithoutInput(platform: Platform): boolean {
  if (!platform.spec || platform.generation === 0) return false
  if (platform.status.observedGeneration >= platform.generation) return false
  const realized = findCondition(platform.status.conditions, ConditionTypes.realized)
  if (!realized || realized.observedGeneration < platform.generation) return true
  return realized.status === 'false' && realized.reason === 'converging'
}

/**
 * The spec a Platform's facts are read from (platforms.md "Spec And Status"): the applied spec,
 * which the last successful Workflow realized, when there is one, else the desired spec (a Platform
 * whose install still waits on its Change Plan, or whose first deployment is running). Null only
 * before the platform controller adopts a Platform deployed before specs existed.
 *
 * The applied spec wins because a fact describes what runs; a newer generation that is not realized
 * yet is reported by the spec state (its `realized` Condition), not by the facts.
 */
export function platformCurrentSpec(platform: Platform): PlatformSpec | null {
  return platform.status.applied ?? platform.spec
}

/**
 * A non-empty string field of a spec's open `settings` map, or of an object inside it; undefined
 * when the field is absent, blank, or not a string. Settings are the platform type's own fields
 * (platforms.md "Create A Platform") and the shared spec does not type them, so a reader narrows
 * each field here instead of asserting a shape the API did not promise.
 */
export function settingString(settings: Readonly<Record<string, unknown>> | undefined, key: string): string | undefined {
  const value = settings?.[key]
  return typeof value === 'string' && value.trim() !== '' ? value : undefined
}

/** A finite number field of a settings map, or undefined; see `settingString`. */
export function settingNumber(settings: Readonly<Record<string, unknown>> | undefined, key: string): number | undefined {
  const value = settings?.[key]
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

/**
 * An object field of a settings map (for example a Slurm spec's `slurm`), or undefined when it is
 * absent, null, an array, or a scalar; see `settingString`.
 */
export function settingObject(
  settings: Readonly<Record<string, unknown>> | undefined,
  key: string,
): Readonly<Record<string, unknown>> | undefined {
  const value = settings?.[key]
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined
  // Narrowed to a non-null, non-array object, which JSON gives string keys; its values stay unknown.
  return value as Readonly<Record<string, unknown>>
}
