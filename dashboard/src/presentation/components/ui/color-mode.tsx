import { ThemeProvider, useTheme } from 'next-themes'
import type { ComponentProps } from 'react'

/**
 * Color-mode plumbing for the console, built on `next-themes`.
 *
 * Chakra v3 delegates light/dark to `next-themes`, which toggles a `class` on
 * `<html>` that the theme's semantic tokens resolve against. The provider is
 * mounted once by `components/ui/provider`. The preference persists in
 * `localStorage` under `swallow.appearance`; three modes are supported
 * (`system` | `light` | `dark`) with `system` following the OS setting live.
 *
 * Note: this replaces the previous hand-rolled appearance store. The stored value
 * format changed from a JSON string to the plain `next-themes` value; the storage
 * key is preserved so the control keeps its place in the masthead.
 */
export type ColorModeProviderProps = ComponentProps<typeof ThemeProvider>

/** The concrete scheme applied to the document once `system` is resolved. */
export type ColorMode = 'light' | 'dark'

/** Wraps `next-themes` with the console's fixed color-mode policy. */
export function ColorModeProvider(props: ColorModeProviderProps) {
  return (
    <ThemeProvider
      attribute="class"
      disableTransitionOnChange
      enableSystem
      storageKey="swallow.appearance"
      defaultTheme="dark"
      {...props}
    />
  )
}

interface UseColorModeReturn {
  /** The resolved scheme currently painted (`system` collapses to light/dark). */
  colorMode: ColorMode
  setColorMode: (mode: ColorMode) => void
  toggleColorMode: () => void
}

/**
 * Returns the resolved color mode and setters.
 *
 * `colorMode` is the *resolved* scheme (never `system`), so callers can pick
 * scheme-specific values without re-deriving the OS preference. For the raw
 * tri-state preference (including `system`), use `useAppearance`.
 */
export function useColorMode(): UseColorModeReturn {
  const { resolvedTheme, setTheme } = useTheme()
  const colorMode: ColorMode = resolvedTheme === 'dark' ? 'dark' : 'light'
  return {
    colorMode,
    setColorMode: setTheme,
    toggleColorMode: () => setTheme(colorMode === 'dark' ? 'light' : 'dark'),
  }
}

/** Picks a value by the resolved color mode; the SSR-free analogue of Chakra v2's helper. */
export function useColorModeValue<T>(light: T, dark: T): T {
  const { colorMode } = useColorMode()
  return colorMode === 'dark' ? dark : light
}
