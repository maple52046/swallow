import { useCallback, useState } from 'react'

/** Browser storage key of the preference; a UI preference only, never data about swallow. */
const SHOW_EXPERIMENTAL_SUITES_KEY = 'swallow.validation.showExperimentalSuites'

function readShowExperimentalSuites(): boolean {
  try {
    return localStorage.getItem(SHOW_EXPERIMENTAL_SUITES_KEY) === 'true'
  } catch {
    // Unavailable storage (private mode, a blocked origin) keeps the default: hidden.
    return false
  }
}

/**
 * Whether the Validation area offers experimental Validation Suites (module suites the API marks
 * `experimental`), and the setter the Suites page's switch uses. The choice is the admin's, kept in
 * this browser's localStorage and off by default; it is separate from the build's experimental
 * features, which gate whole screens. It only filters what is offered: runs and schedules of an
 * experimental suite are always shown, and the API runs such a suite whatever this says.
 *
 * Each component reads the stored value when it mounts. The run and schedule dialogs remount on
 * every opening, so they see a change the Suites page made; two components mounted at once do not
 * sync with each other.
 */
export function useShowExperimentalSuites(): [boolean, (show: boolean) => void] {
  const [show, setShow] = useState(readShowExperimentalSuites)
  const update = useCallback((next: boolean) => {
    setShow(next)
    try {
      localStorage.setItem(SHOW_EXPERIMENTAL_SUITES_KEY, String(next))
    } catch {
      // A full or blocked storage only means the choice lasts until the page reloads.
    }
  }, [])
  return [show, update]
}
