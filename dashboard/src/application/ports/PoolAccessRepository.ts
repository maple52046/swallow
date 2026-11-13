import type { PoolAccessPolicy, PoolMember, SudoSetting } from '@/domain/access/accounts'
import type { PoolRole } from '@/domain/user/types'

/**
 * Application port for a Pool's memberships and Sudo Policy (contract pool-access.md, decision 082).
 *
 * Reads require the `admin` User Role or any Pool Role in the Pool; writes require `admin` or the
 * Pool's `owner` role. A caller who may not read the Pool gets `not_found` (the Pool is not
 * revealed), a member who may read but not write gets `forbidden`. Adding a username that is not a
 * User is `not_found`, adding an existing member is `conflict`, and a bad role or setting is
 * `validation_error`. Changes reach the Servers' directory client within a minute; nothing here
 * waits for that.
 */
export interface PoolAccessRepository {
  /** The Pool's memberships with their resolved Sudo Policy, ordered by username. */
  listMembers(poolId: string): Promise<PoolMember[]>
  /** Adds an existing User; an omitted sudo setting is `inherit`. */
  addMember(
    poolId: string,
    input: { username: string; role: PoolRole; sudo?: SudoSetting; sudoPasswordless?: SudoSetting },
  ): Promise<PoolMember>
  /** Changes a membership's role or overrides; an omitted field is left as it is. */
  updateMember(
    poolId: string,
    userId: string,
    input: { role?: PoolRole; sudo?: SudoSetting; sudoPasswordless?: SudoSetting },
  ): Promise<PoolMember>
  /** Removes a membership; the User loses access to the Pool's Servers once the Directory is applied. */
  removeMember(poolId: string, userId: string): Promise<void>
  /** The Pool's Sudo Policy defaults. */
  getPolicy(poolId: string): Promise<PoolAccessPolicy>
  /** Replaces the Pool's Sudo Policy defaults. */
  setPolicy(poolId: string, input: { memberSudo: boolean; sudoPasswordless: boolean }): Promise<PoolAccessPolicy>
}
