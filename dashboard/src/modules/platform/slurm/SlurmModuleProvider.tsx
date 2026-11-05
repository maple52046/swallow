/* eslint-disable react-refresh/only-export-components */
import { createContext, useContext, type ReactNode } from 'react'
import type { SlurmPlatformPort } from './SlurmPlatformPort'

const SlurmModuleContext = createContext<SlurmPlatformPort | null>(null)

/** Binds the Slurm module UI to its live-cluster and deployment-policy adapter. */
export function SlurmModuleProvider({ service, children }: { service: SlurmPlatformPort; children: ReactNode }) {
  return <SlurmModuleContext.Provider value={service}>{children}</SlurmModuleContext.Provider>
}

/** Returns Slurm-only operations installed by the dashboard composition root. */
export function useSlurmPlatform(): SlurmPlatformPort {
  const service = useContext(SlurmModuleContext)
  if (!service) throw new Error('useSlurmPlatform must be used within SlurmModuleProvider')
  return service
}
