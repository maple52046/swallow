import { useStoredFlag } from './useStoredFlag'

/** Browser storage key of the preference; a UI preference only, never data about swallow. */
const SHOW_EXPERIMENTAL_SUITES_KEY = 'swallow.validation.showExperimentalSuites'

/**
 * Whether the Validation area offers experimental Validation Suites (module suites the API marks
 * `experimental`), and the setter the Suites page's switch uses. The choice is the User's, kept in
 * this browser's localStorage and off by default; it is separate from the build's experimental
 * features, which gate whole screens. It only filters what is offered: runs and schedules of an
 * experimental suite are always shown, and the API runs such a suite whatever this says.
 *
 * Each component reads the stored value when it mounts. The run and schedule dialogs remount on
 * every opening, so they see a change the Suites page made.
 */
export function useShowExperimentalSuites(): [boolean, (show: boolean) => void] {
  return useStoredFlag(SHOW_EXPERIMENTAL_SUITES_KEY)
}
