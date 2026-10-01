/* eslint-disable react-refresh/only-export-components */
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import {
  EXPERIMENTAL_FEATURES,
  type ExperimentalFeature,
  type ExperimentalFeatureSettings,
} from '@/application/ports/ExperimentalFeatureSettings'
import { useApp } from '@/di/AppProvider'

/** Whether each experimental feature is shown, keyed by feature. */
export type ExperimentalFeatureState = Readonly<Record<ExperimentalFeature, boolean>>

interface ExperimentalFeaturesValue {
  /** False in release builds; the settings dialog and its menu entry exist only when true. */
  adjustable: boolean
  enabled: ExperimentalFeatureState
  setEnabled: (feature: ExperimentalFeature, enabled: boolean) => void
  reset: () => void
}

const ExperimentalFeaturesContext = createContext<ExperimentalFeaturesValue | null>(null)

/** Reads every feature once, so a render sees one consistent state rather than per-call storage reads. */
function snapshot(settings: ExperimentalFeatureSettings): ExperimentalFeatureState {
  // The assertion is safe: the loop below assigns every member of EXPERIMENTAL_FEATURES, which
  // lists the whole ExperimentalFeature union.
  const state = {} as Record<ExperimentalFeature, boolean>
  for (const feature of EXPERIMENTAL_FEATURES) state[feature] = settings.isEnabled(feature)
  return state
}

/**
 * Shares the experimental feature switches with the whole console.
 *
 * The settings port (bound per build in the DI container) is read once on mount
 * and again after every change, and the snapshot lives in React state so a switch
 * flipped in the settings dialog re-renders navigation, routes, and placeholders
 * immediately instead of on the next reload. It must sit inside `AppProvider`.
 */
export function ExperimentalFeaturesProvider({ children }: { children: ReactNode }) {
  const { experimentalFeatures } = useApp()
  const [enabled, setState] = useState(() => snapshot(experimentalFeatures))

  const setEnabled = useCallback((feature: ExperimentalFeature, value: boolean) => {
    experimentalFeatures.setEnabled(feature, value)
    setState(snapshot(experimentalFeatures))
  }, [experimentalFeatures])

  const reset = useCallback(() => {
    experimentalFeatures.reset()
    setState(snapshot(experimentalFeatures))
  }, [experimentalFeatures])

  const value = useMemo(
    () => ({ adjustable: experimentalFeatures.adjustable, enabled, setEnabled, reset }),
    [experimentalFeatures.adjustable, enabled, setEnabled, reset],
  )
  return <ExperimentalFeaturesContext.Provider value={value}>{children}</ExperimentalFeaturesContext.Provider>
}

/** Full switch state and setters, for the dev-only settings dialog and its menu entry. */
export function useExperimentalFeatures(): ExperimentalFeaturesValue {
  const ctx = useContext(ExperimentalFeaturesContext)
  if (!ctx) throw new Error('useExperimentalFeatures must be used within ExperimentalFeaturesProvider')
  return ctx
}

/**
 * Whether an in-development feature is shown in this build and browser.
 *
 * Always false in release builds. Components use it to hide standalone surfaces
 * (navigation entries, tabs, actions) and to render the shared "not available in
 * this release" placeholder where a value keeps its place on a shared page.
 */
export function useExperimentalFeature(feature: ExperimentalFeature): boolean {
  return useExperimentalFeatures().enabled[feature]
}
