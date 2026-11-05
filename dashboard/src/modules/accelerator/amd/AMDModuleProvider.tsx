/* eslint-disable react-refresh/only-export-components */
import { createContext, useContext, type ReactNode } from 'react'
import type { AMDPlatformPort } from './AMDPlatformPort'

const AMDModuleContext = createContext<AMDPlatformPort | null>(null)

/** Binds AMD module contributions to their release catalog and Platform mutation adapter. */
export function AMDModuleProvider({ service, children }: { service: AMDPlatformPort; children: ReactNode }) {
  return <AMDModuleContext.Provider value={service}>{children}</AMDModuleContext.Provider>
}

/** Returns the AMD module dependency installed by the dashboard composition root. */
export function useAMDPlatform(): AMDPlatformPort {
  const service = useContext(AMDModuleContext)
  if (!service) throw new Error('useAMDPlatform must be used within AMDModuleProvider')
  return service
}
