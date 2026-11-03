import {
  EXPERIMENTAL_FEATURES,
  type ExperimentalFeature,
  type ExperimentalFeatureSettings,
} from '@/application/ports/ExperimentalFeatureSettings'
import { lsGet, lsSet } from './localStorage'

const STORAGE_KEY = 'swallow.dev.experimentalFeatures'

/** Stored shape: only features the developer switched off, as `{ feature: false }`. */
type StoredChoices = Partial<Record<ExperimentalFeature, boolean>>

/**
 * Reads the stored opt-outs, treating anything that is not a plain object as no choices.
 *
 * Only keys of current features are kept, and the next write drops the rest: a key whose switch
 * left the list (its feature was released or renamed) must neither keep hiding a surface nor
 * carry over to another switch.
 */
function readChoices(): StoredChoices {
  const stored = lsGet<unknown>(STORAGE_KEY, {})
  if (typeof stored !== 'object' || stored === null || Array.isArray(stored)) return {}
  // Safe: `stored` is a non-null, non-array object, and each value is only compared to `false`.
  const record = stored as Record<string, unknown>
  const choices: StoredChoices = {}
  for (const feature of EXPERIMENTAL_FEATURES) {
    if (record[feature] === false) choices[feature] = false
  }
  return choices
}

/**
 * Development-build settings persisted in this browser's localStorage.
 *
 * Every feature defaults to on so the dev server shows the whole product; only an
 * explicit `false` hides one. Storing opt-outs instead of the full state means a
 * newly added experimental feature appears without clearing storage. The key lives
 * under `swallow.dev.` because release builds never read it.
 */
export class LocalExperimentalFeatureSettings implements ExperimentalFeatureSettings {
  readonly adjustable = true

  isEnabled(feature: ExperimentalFeature): boolean {
    return readChoices()[feature] !== false
  }

  setEnabled(feature: ExperimentalFeature, enabled: boolean): void {
    const choices = readChoices()
    if (enabled) delete choices[feature]
    else choices[feature] = false
    lsSet(STORAGE_KEY, choices)
  }

  reset(): void {
    lsSet(STORAGE_KEY, {})
  }
}

/**
 * Release-build settings: every experimental feature is off and cannot be enabled.
 *
 * It deliberately ignores the development storage key, so a value left behind in a
 * browser (or set by hand) cannot expose an unfinished surface in production.
 */
export class DisabledExperimentalFeatureSettings implements ExperimentalFeatureSettings {
  readonly adjustable = false

  isEnabled(): boolean {
    return false
  }

  setEnabled(): void {}

  reset(): void {}
}
