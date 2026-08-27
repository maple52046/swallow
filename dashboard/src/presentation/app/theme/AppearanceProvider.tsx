import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { AppearanceContext } from './appearanceContext'
import {
  loadAppearanceMode,
  resolveAppearance,
  saveAppearanceMode,
  type AppearanceMode,
} from './index'

interface AppearanceProviderProps {
  children: ReactNode
}

/**
 * Applies PatternFly's official dark class to the document root.
 *
 * System mode subscribes to `prefers-color-scheme`; the listener is removed on unmount
 * and explicit Light/Dark selections remain stable even when the OS changes. No custom
 * palette is applied, so both schemes use stock PatternFly semantic tokens.
 */
export function AppearanceProvider({ children }: AppearanceProviderProps) {
  const [mode, setModeState] = useState<AppearanceMode>(loadAppearanceMode)
  const [systemDark, setSystemDark] = useState(() =>
    window.matchMedia('(prefers-color-scheme: dark)').matches,
  )
  const resolved = resolveAppearance(mode, systemDark)

  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = (event: MediaQueryListEvent) => setSystemDark(event.matches)
    media.addEventListener('change', onChange)
    return () => media.removeEventListener('change', onChange)
  }, [])

  useEffect(() => {
    document.documentElement.classList.toggle('pf-v6-theme-dark', resolved === 'dark')
    document.documentElement.style.colorScheme = resolved
  }, [resolved])

  const setMode = useCallback((next: AppearanceMode) => {
    saveAppearanceMode(next)
    setModeState(next)
  }, [])

  const value = useMemo(() => ({ mode, resolved, setMode }), [mode, resolved, setMode])
  return <AppearanceContext.Provider value={value}>{children}</AppearanceContext.Provider>
}
