import { useTheme } from 'next-themes'
import type { AppearanceMode, ResolvedAppearance } from './index'

/**
 * Appearance state exposed to the masthead's color-mode control.
 *
 * `mode` is the persisted tri-state preference (`system` | `light` | `dark`);
 * `resolved` is the concrete scheme currently painted. This is a thin adapter over
 * `next-themes` so the masthead keeps a stable, domain-shaped contract without
 * touching the theming library directly.
 */
export interface AppearanceContextValue {
  mode: AppearanceMode
  resolved: ResolvedAppearance
  setMode: (mode: AppearanceMode) => void
}

/**
 * Returns the appearance preference and setter, backed by `next-themes`.
 *
 * The preference persists under `swallow.appearance` (see `ColorModeProvider`).
 * `mode` falls back to `system` until the client has hydrated the stored value,
 * and `resolved` defaults to `light` before the OS scheme is known.
 */
export function useAppearance(): AppearanceContextValue {
  const { theme, resolvedTheme, setTheme } = useTheme()
  const mode = (theme ?? 'system') as AppearanceMode
  const resolved: ResolvedAppearance = resolvedTheme === 'dark' ? 'dark' : 'light'
  return { mode, resolved, setMode: setTheme }
}
