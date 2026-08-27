import { createContext, useContext } from 'react'
import type { AppearanceMode, ResolvedAppearance } from './index'

/**
 * Appearance state exposed to shell controls. `mode` is the persisted user choice;
 * `resolved` is the concrete scheme currently applied to PatternFly.
 */
export interface AppearanceContextValue {
  mode: AppearanceMode
  resolved: ResolvedAppearance
  setMode: (mode: AppearanceMode) => void
}

export const AppearanceContext = createContext<AppearanceContextValue | null>(null)

/** Returns the app appearance boundary and fails loudly when the provider is missing. */
export function useAppearance(): AppearanceContextValue {
  const value = useContext(AppearanceContext)
  if (value === null) {
    throw new Error('useAppearance must be used within AppearanceProvider')
  }
  return value
}
