/** A browser appearance preference. System follows the live OS color-scheme setting. */
export type AppearanceMode = 'system' | 'light' | 'dark'

/** The concrete PatternFly color scheme currently applied to the document. */
export type ResolvedAppearance = 'light' | 'dark'

const APPEARANCE_KEY = 'swallow.appearance'
const MODES: readonly AppearanceMode[] = ['system', 'light', 'dark']

/** Reads the per-browser appearance choice and rejects stale values from earlier themes. */
export function loadAppearanceMode(): AppearanceMode {
  try {
    const stored = JSON.parse(localStorage.getItem(APPEARANCE_KEY) ?? '"system"') as string
    return MODES.includes(stored as AppearanceMode) ? stored as AppearanceMode : 'system'
  } catch { return 'system' }
}

/** Persists only the preference; the provider owns resolving and applying it. */
export function saveAppearanceMode(mode: AppearanceMode): void {
  try { localStorage.setItem(APPEARANCE_KEY, JSON.stringify(mode)) } catch {
    // Browser policy may block persistence; current session appearance remains usable.
  }
}

/** Resolves System against the supplied media-query result without touching the DOM. */
export function resolveAppearance(mode: AppearanceMode, systemDark: boolean): ResolvedAppearance {
  return mode === 'system' ? (systemDark ? 'dark' : 'light') : mode
}
