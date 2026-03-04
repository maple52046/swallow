import { createContext, useContext, useMemo, type ReactNode } from 'react'
import { type AppContainer, createContainer } from './container'

const AppContext = createContext<AppContainer | null>(null)

export function AppProvider({ children }: { children: ReactNode }) {
  const container = useMemo(() => createContainer(), [])
  return <AppContext.Provider value={container}>{children}</AppContext.Provider>
}

export function useApp(): AppContainer {
  const ctx = useContext(AppContext)
  if (!ctx) throw new Error('useApp must be used within AppProvider')
  return ctx
}
