import type { AccountRepository } from '@/application/ports/AccountRepository'
import type { AccessAuditEntry, AccessSettings, Account, DirectoryStatus } from '@/domain/access/accounts'
import type { UserRole } from '@/domain/user/types'
import { apiRequest } from './client'
import type { JsonRequestBody, JsonResponse } from './contractTypes'
import type { operations } from './generated/openapi'

/**
 * Maps the Active account contracts (`/api/v1/users` in users.md, `/api/v1/access/*` in access.md,
 * and `PUT /api/v1/auth/me/password` in auth-me.md) onto the AccountRepository port. The contract's
 * JSON already matches the domain shapes, which the generated request and response types prove at
 * each call.
 *
 * Security: passwords appear only in request bodies. They are never returned, cached, logged, or
 * written to browser storage, and the requests ask the browser not to keep anything in its HTTP
 * cache.
 */
export class ApiAccountRepository implements AccountRepository {
  async listAccounts(): Promise<Account[]> {
    const response = await apiRequest<JsonResponse<operations['listUsers'], 200>>('/api/v1/users')
    return response.items
  }

  createAccount(input: { username: string; password: string; role: UserRole }): Promise<Account> {
    const body: JsonRequestBody<operations['createUser']> = input
    return apiRequest<JsonResponse<operations['createUser'], 201>>('/api/v1/users', {
      method: 'POST',
      body: JSON.stringify(body),
      cache: 'no-store',
    })
  }

  updateAccount(id: string, input: { role?: UserRole; password?: string }): Promise<Account> {
    const body: JsonRequestBody<operations['updateUser']> = input
    return apiRequest<JsonResponse<operations['updateUser'], 200>>(`/api/v1/users/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
      cache: 'no-store',
    })
  }

  async deleteAccount(id: string): Promise<void> {
    await apiRequest<void>(`/api/v1/users/${encodeURIComponent(id)}`, { method: 'DELETE' })
  }

  async changeOwnPassword(input: { currentPassword: string; newPassword: string }): Promise<void> {
    const body: JsonRequestBody<operations['changeOwnPassword']> = input
    await apiRequest<void>('/api/v1/auth/me/password', {
      method: 'PUT',
      body: JSON.stringify(body),
      cache: 'no-store',
    })
  }

  getAccessSettings(): Promise<AccessSettings> {
    return apiRequest<JsonResponse<operations['getAccessSettings'], 200>>('/api/v1/access/settings')
  }

  setAccessSettings(input: { adminSudoPasswordless: boolean }): Promise<AccessSettings> {
    const body: JsonRequestBody<operations['setAccessSettings']> = input
    return apiRequest<JsonResponse<operations['setAccessSettings'], 200>>('/api/v1/access/settings', {
      method: 'PUT',
      body: JSON.stringify(body),
    })
  }

  async listAccessAudit(limit?: number): Promise<AccessAuditEntry[]> {
    const query = limit === undefined ? '' : `?limit=${limit}`
    const response = await apiRequest<JsonResponse<operations['listAccessAudit'], 200>>(`/api/v1/access/audit${query}`)
    return response.items
  }

  getDirectory(): Promise<DirectoryStatus> {
    return apiRequest<JsonResponse<operations['getDirectory'], 200>>('/api/v1/access/directory')
  }

  async syncDirectory(): Promise<void> {
    await apiRequest<void>('/api/v1/access/directory/sync', { method: 'POST' })
  }
}
