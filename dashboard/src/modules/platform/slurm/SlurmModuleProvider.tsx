/* eslint-disable react-refresh/only-export-components */
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import type { SlurmPlatformPort } from './SlurmPlatformPort'

const SlurmModuleContext = createContext<SlurmPlatformPort | null>(null)

/** The warnings of each Platform's last accepted topology change, by Platform id. */
interface SlurmChangeWarnings {
  warnings: Readonly<Record<string, readonly string[]>>
  setWarnings: (platformId: string, warnings: readonly string[]) => void
}

const SlurmChangeWarningsContext = createContext<SlurmChangeWarnings | null>(null)

/**
 * Binds the Slurm module UI to its live-cluster and deployment-policy adapter, and keeps the API's
 * warnings about each Platform's last topology change. The warnings live here, above the routes,
 * because the Platform workspace reloads (and remounts its tabs) after every accepted change, and
 * they must still be on screen afterwards; they are presentation state only and end with the page.
 */
export function SlurmModuleProvider({ service, children }: { service: SlurmPlatformPort; children: ReactNode }) {
  const [warnings, setAll] = useState<Record<string, readonly string[]>>({})
  const setWarnings = useCallback((platformId: string, next: readonly string[]) => {
    setAll((current) => ({ ...current, [platformId]: next }))
  }, [])
  const notices = useMemo(() => ({ warnings, setWarnings }), [warnings, setWarnings])
  return (
    <SlurmModuleContext.Provider value={service}>
      <SlurmChangeWarningsContext.Provider value={notices}>{children}</SlurmChangeWarningsContext.Provider>
    </SlurmModuleContext.Provider>
  )
}

/** Returns Slurm-only operations installed by the dashboard composition root. */
export function useSlurmPlatform(): SlurmPlatformPort {
  const service = useContext(SlurmModuleContext)
  if (!service) throw new Error('useSlurmPlatform must be used within SlurmModuleProvider')
  return service
}

/**
 * The warnings the API answered to a Platform's last accepted topology change, with a setter that
 * replaces them (an empty list dismisses them). Empty until a change in this page session answered any.
 */
export function useSlurmChangeWarnings(platformId: string): [readonly string[], (warnings: readonly string[]) => void] {
  const notices = useContext(SlurmChangeWarningsContext)
  if (!notices) throw new Error('useSlurmChangeWarnings must be used within SlurmModuleProvider')
  const current = notices.warnings[platformId] ?? []
  const set = (next: readonly string[]) => notices.setWarnings(platformId, next)
  return [current, set]
}
