import type { User } from '@/domain/user/types'

/**
 * Session boundary consumed by the session provider (decision 042). Implementations own every
 * credential: the presentation never sees an access or refresh token, only the signed-in User.
 */
export interface AuthRepository {
  /**
   * Resumes the browser's Session after a page load and returns its User, or `null` when there is
   * no Session (never signed in, logged out, or expired). Rejects only when the server cannot be
   * reached, which the caller treats as signed out.
   */
  restoreSession(): Promise<User | null>
  /** Signs in with a password and returns the User; rejects with a user-facing message. */
  login(username: string, password: string): Promise<User>
  /** Ends the Session on the server (best effort) and always forgets local credentials. */
  logout(): Promise<void>
  /**
   * Subscribes to the Session ending while signed in — a refresh the server rejected — so the
   * presentation can return to sign-in. Returns the unsubscribe function.
   */
  onSessionEnded(listener: () => void): () => void
}
