import type { PoolAccessRepository } from '@/application/ports/PoolAccessRepository'
import type { PoolAccessPolicy, PoolMember, SudoSetting } from '@/domain/access/accounts'
import type { PoolRole } from '@/domain/user/types'
import { apiRequest } from './client'
import type { JsonRequestBody, JsonResponse } from './contractTypes'
import type { operations } from './generated/openapi'

/** The members collection of one Pool; ids are opaque and always encoded. */
function membersPath(poolId: string): string {
  return `/api/v1/infrastructure/pools/${encodeURIComponent(poolId)}/members`
}

/**
 * Maps the Active Pool access contract (`/api/v1/infrastructure/pools/{id}/members` and
 * `/access-policy`, pool-access.md) onto the PoolAccessRepository port. The contract's JSON already
 * matches the domain shapes, which the generated request and response types prove at each call.
 */
export class ApiPoolAccessRepository implements PoolAccessRepository {
  async listMembers(poolId: string): Promise<PoolMember[]> {
    const response = await apiRequest<JsonResponse<operations['listPoolMembers'], 200>>(membersPath(poolId))
    return response.items
  }

  addMember(
    poolId: string,
    input: { username: string; role: PoolRole; sudo?: SudoSetting; sudoPasswordless?: SudoSetting },
  ): Promise<PoolMember> {
    const body: JsonRequestBody<operations['addPoolMember']> = input
    return apiRequest<JsonResponse<operations['addPoolMember'], 201>>(membersPath(poolId), {
      method: 'POST',
      body: JSON.stringify(body),
    })
  }

  updateMember(
    poolId: string,
    userId: string,
    input: { role?: PoolRole; sudo?: SudoSetting; sudoPasswordless?: SudoSetting },
  ): Promise<PoolMember> {
    const body: JsonRequestBody<operations['updatePoolMember']> = input
    return apiRequest<JsonResponse<operations['updatePoolMember'], 200>>(`${membersPath(poolId)}/${encodeURIComponent(userId)}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    })
  }

  async removeMember(poolId: string, userId: string): Promise<void> {
    await apiRequest<void>(`${membersPath(poolId)}/${encodeURIComponent(userId)}`, { method: 'DELETE' })
  }

  getPolicy(poolId: string): Promise<PoolAccessPolicy> {
    return apiRequest<JsonResponse<operations['getPoolAccessPolicy'], 200>>(
      `/api/v1/infrastructure/pools/${encodeURIComponent(poolId)}/access-policy`,
    )
  }

  setPolicy(poolId: string, input: { memberSudo: boolean; sudoPasswordless: boolean }): Promise<PoolAccessPolicy> {
    const body: JsonRequestBody<operations['setPoolAccessPolicy']> = input
    return apiRequest<JsonResponse<operations['setPoolAccessPolicy'], 200>>(
      `/api/v1/infrastructure/pools/${encodeURIComponent(poolId)}/access-policy`,
      { method: 'PUT', body: JSON.stringify(body) },
    )
  }
}
