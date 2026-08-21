import { useCallback, useMemo, useState } from 'react'
import { Theme } from '@radix-ui/themes'
import { AppearanceContext } from './appearanceContext'
import { THEME_CONFIG, loadColorScheme, saveColorScheme, type ColorScheme } from './index'

interface AppearanceProviderProps {
  children: React.ReactNode
}

/**
 * App shell theme boundary: renders the single Radix `<Theme>` and owns the live
 * light/dark appearance.
 *
 * Radix reads `appearance` from the nearest `<Theme>`, so the toggle has to live in React
 * state here rather than being a CSS class flip. The initial value comes from
 * localStorage (`loadColorScheme`) and every change is persisted, so the choice is stable
 * across reloads. Consumers switch the scheme through `useAppearance`, never by writing
 * storage directly.
 */
export function AppearanceProvider({ children }: AppearanceProviderProps) {
  const [appearance, setAppearance] = useState<ColorScheme>(loadColorScheme)

  const toggle = useCallback(() => {
    setAppearance((current) => {
      const next: ColorScheme = current === 'dark' ? 'light' : 'dark'
      saveColorScheme(next)
      return next
    })
  }, [])

  const value = useMemo(() => ({ appearance, toggle }), [appearance, toggle])

  return (
    <AppearanceContext.Provider value={value}>
      <Theme
        appearance={appearance}
        accentColor={THEME_CONFIG.accentColor}
        grayColor={THEME_CONFIG.grayColor}
        radius={THEME_CONFIG.radius}
        scaling={THEME_CONFIG.scaling}
      >
        {children}
      </Theme>
    </AppearanceContext.Provider>
  )
}
