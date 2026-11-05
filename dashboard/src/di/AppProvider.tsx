/* eslint-disable react-refresh/only-export-components */
import { createContext, useContext, useMemo, type ReactNode } from 'react'
import { AMDModuleProvider } from '@/modules/accelerator/amd/AMDModuleProvider'
import { KubernetesModuleProvider } from '@/modules/platform/kubernetes/KubernetesModuleProvider'
import { SlurmModuleProvider } from '@/modules/platform/slurm/SlurmModuleProvider'
import { type AppContainer, createContainer, createPlatformModuleContainer } from './container'

const AppContext = createContext<AppContainer | null>(null)

/**
 * Browser composition root for generic application ports and independently owned Platform module
 * adapters. Module dependencies stay out of AppContainer so shared presentation cannot reach them.
 */
export function AppProvider({ children }: { children: ReactNode }) {
  const container = useMemo(() => createContainer(), [])
  const modules = useMemo(() => createPlatformModuleContainer(), [])
  return (
    <AppContext.Provider value={container}>
      <KubernetesModuleProvider explorer={modules.kubernetes}>
        <SlurmModuleProvider service={modules.slurm}>
          <AMDModuleProvider service={modules.amd}>{children}</AMDModuleProvider>
        </SlurmModuleProvider>
      </KubernetesModuleProvider>
    </AppContext.Provider>
  )
}

/** Returns generic application ports; type-specific Platform ports live in module-local hooks. */
export function useApp(): AppContainer {
  const ctx = useContext(AppContext)
  if (!ctx) throw new Error('useApp must be used within AppProvider')
  return ctx
}
