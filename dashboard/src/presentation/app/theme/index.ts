/**
 * Theme configuration and colour-scheme persistence for the Radix Themes UI.
 *
 * The dashboard renders inside a single Radix `<Theme>` (see `AppearanceProvider`).
 * These constants are the app-wide look, and the colour-scheme helpers own the one piece
 * of UI state the browser is the right home for: whether the user prefers light or dark.
 */
import { lsGet, lsSet } from '@/infrastructure/persistence/localStorage'

/**
 * Static `<Theme>` props shared by the whole app.
 *
 * Kept here rather than inline in the provider so the accent, radius, and scaling are one
 * documented source of truth, mirroring the single Mantine theme this replaced.
 */
export const THEME_CONFIG = {
  accentColor: 'blue',
  grayColor: 'slate',
  radius: 'medium',
  scaling: '100%',
} as const

/** The two colour schemes the dashboard supports; maps onto Radix `appearance`. */
export type ColorScheme = 'light' | 'dark'

/** The localStorage key for the persisted colour scheme. Unchanged across the Radix migration. */
const COLOR_SCHEME_KEY = 'color-scheme'

/**
 * Reads the persisted colour scheme, defaulting to dark.
 *
 * Owned by the browser because it is a per-device UI preference, not platform state.
 */
export function loadColorScheme(): ColorScheme {
  return lsGet<ColorScheme>(COLOR_SCHEME_KEY, 'dark')
}

/** Persists the colour scheme so the choice survives a reload. */
export function saveColorScheme(scheme: ColorScheme): void {
  lsSet(COLOR_SCHEME_KEY, scheme)
}
