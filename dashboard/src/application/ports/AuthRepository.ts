import type { User } from '@/domain/user/types'

/** Authentication boundary consumed by the session provider. */
export interface AuthRepository {
  /** Whether a browser token exists and should be verified. */
  hasSession(): boolean
  /** Verifies the stored token and returns the current domain user. */
  restoreSession(): Promise<User>
  /** Exchanges credentials for a token, then returns the authenticated user. */
  login(username: string, password: string): Promise<User>
  /** Clears browser authentication state. */
  logout(): void
}
