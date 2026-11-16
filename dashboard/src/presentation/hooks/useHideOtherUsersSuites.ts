import { useStoredFlag } from './useStoredFlag'

/**
 * Browser storage key of the preference. It holds only `true` or `false`: no user id, username, or
 * suite id, so it reveals nothing about the signed-in User or the catalog, and a different User
 * signing in on the same browser simply inherits the switch position.
 */
const HIDE_OTHER_USERS_SUITES_KEY = 'swallow.validation.hideOtherUsersSuites'

/**
 * Whether the Validation area hides user suites written by other Users, and the setter the
 * Suites page's switch uses. Off by default and remembered per browser. It filters only what the
 * dashboard lists and offers: module suites are never hidden, the User's own suites never are, the
 * API still returns and runs every suite, and runs of hidden suites stay listed.
 *
 * Each component reads the stored value when it mounts. The run dialog remounts on every opening,
 * so it sees a change the Suites page made.
 */
export function useHideOtherUsersSuites(): [boolean, (hide: boolean) => void] {
  return useStoredFlag(HIDE_OTHER_USERS_SUITES_KEY)
}
