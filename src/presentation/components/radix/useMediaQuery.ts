/**
 * Subscribes to a CSS media query and re-renders on change.
 *
 * Used by layout components to switch between the desktop sidebar and the mobile overlay,
 * since Radix Themes has no responsive app-shell. Returns `false` during the first render
 * on the server or before the listener attaches; callers must treat the initial value as
 * "not yet matched" rather than a definitive answer.
 */
import { useEffect, useState } from 'react'

export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() =>
    typeof window !== 'undefined' ? window.matchMedia(query).matches : false,
  )

  useEffect(() => {
    const mediaQuery = window.matchMedia(query)
    const onChange = () => setMatches(mediaQuery.matches)
    // Sync once in case the query changed between render and effect.
    onChange()
    mediaQuery.addEventListener('change', onChange)
    return () => mediaQuery.removeEventListener('change', onChange)
  }, [query])

  return matches
}
