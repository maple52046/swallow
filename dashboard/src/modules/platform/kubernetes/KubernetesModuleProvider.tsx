/* eslint-disable react-refresh/only-export-components */
import { createContext, useContext, type ReactNode } from 'react'
import type { KubernetesExplorerPort } from './KubernetesExplorerPort'

const KubernetesModuleContext = createContext<KubernetesExplorerPort | null>(null)

/** Binds the Kubernetes module UI to its cluster explorer without exposing it to the core port. */
export function KubernetesModuleProvider({ explorer, children }: { explorer: KubernetesExplorerPort; children: ReactNode }) {
  return <KubernetesModuleContext.Provider value={explorer}>{children}</KubernetesModuleContext.Provider>
}

/** Returns the Kubernetes module's live explorer dependency installed by the composition root. */
export function useKubernetesExplorer(): KubernetesExplorerPort {
  const explorer = useContext(KubernetesModuleContext)
  if (!explorer) throw new Error('useKubernetesExplorer must be used within KubernetesModuleProvider')
  return explorer
}
