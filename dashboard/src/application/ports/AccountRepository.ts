import type { AccessAuditEntry, AccessSettings, Account, DirectoryStatus } from '@/domain/access/accounts'
import type { UserRole } from '@/domain/user/types'

/**
 * Application port for account administration and the signed-in user's own password (contracts
 * users.md, access.md and auth-me.md, decision 082).
 *
 * Every method except `changeOwnPassword` requires the `admin` User Role; a non-admin is refused
 * with `forbidden`. Implementations map the provider contract only and reject with the shared error
 * envelope: `validation_error` for a bad username, password, or role; `conflict` for a taken
 * username, the last admin, deleting oneself, or a sync without a Directory. A password crosses this
 * port only on its way to the API: implementations never cache, log, or store it.
 */
export interface AccountRepository {
  /** Every User, ordered by username. */
  listAccounts(): Promise<Account[]>
  /** Creates a User with an initial password the admin hands to the person out of band. */
  createAccount(input: { username: string; password: string; role: UserRole }): Promise<Account>
  /** Changes a User's role, resets its password, or both; an omitted field is left as it is. */
  updateAccount(id: string, input: { role?: UserRole; password?: string }): Promise<Account>
  /** Deletes a User with its memberships, Access Keys, API Keys, and Sessions; its uid is never reused. */
  deleteAccount(id: string): Promise<void>
  /**
   * Changes the signed-in user's own password. Requires a password Session (an API key is refused
   * with `forbidden`); a wrong current password is `validation_error`.
   */
  changeOwnPassword(input: { currentPassword: string; newPassword: string }): Promise<void>
  /** The installation's access settings. */
  getAccessSettings(): Promise<AccessSettings>
  /** Sets whether admins' sudo asks for a password; `posixIdStart` cannot change. */
  setAccessSettings(input: { adminSudoPasswordless: boolean }): Promise<AccessSettings>
  /** The most recent access changes, newest first; `limit` is 1 to 500 (100 when omitted). */
  listAccessAudit(limit?: number): Promise<AccessAuditEntry[]>
  /** The Directory's configuration and the Directory controller's last report. */
  getDirectory(): Promise<DirectoryStatus>
  /** Asks the Directory controller to apply the content now; resolves once the request is recorded. */
  syncDirectory(): Promise<void>
}
