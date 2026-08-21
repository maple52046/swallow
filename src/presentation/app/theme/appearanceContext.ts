/**
 * Appearance (light/dark) context, kept separate from `AppearanceProvider` so the
 * provider file exports only a component (Fast Refresh requires that).
 */
import { createContext, useContext } from 'react'
import type { ColorScheme } from './index'

/** What `useAppearance` exposes: the current scheme and a toggle that persists it. */
export interface AppearanceContextValue {
  appearance: ColorScheme
  /** Flips light/dark, updates the live Radix `<Theme>`, and persists the choice. */
  toggle: () => void
}

export const AppearanceContext = createContext<AppearanceContextValue | null>(null)

/**
 * Reads the appearance context.
 *
 * Throws when used outside `AppearanceProvider`, because a silent default would let the
 * theme toggle render but do nothing.
 */
export function useAppearance(): AppearanceContextValue {
  const value = useContext(AppearanceContext)
  if (value === null) {
    throw new Error('useAppearance must be used within AppearanceProvider')
  }
  return value
}
