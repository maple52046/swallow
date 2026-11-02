import type { ApiKeyRepository } from '@/application/ports/ApiKeyRepository'
import type { ApiKey, CreatedApiKey } from '@/domain/access/types'
import { apiRequest } from './client'
import type { JsonRequestBody, JsonResponse } from './contractTypes'
import type { operations } from './generated/openapi'

/**
 * Maps the Active API Keys contract (`/api/v1/api-keys`, `api-keys.md`) onto the ApiKeyRepository
 * port; the contract's JSON already matches the domain shapes, which the generated request and
 * response types prove where each is sent and returned. (The `Api` prefix names the HTTP
 * driver, as for every adapter in this folder.)
 *
 * Security: the only secret crossing this adapter is the one-time key secret in the create
 * response. It is handed straight to the caller and never cached, logged, or written to browser
 * storage; the request asks the browser not to keep the response in its HTTP cache.
 */
export class ApiApiKeyRepository implements ApiKeyRepository {
  listApiKeys(): Promise<ApiKey[]> {
    return apiRequest<JsonResponse<operations['listApiKeys'], 200>>('/api/v1/api-keys')
  }

  createApiKey(input: { name: string; expiresAt?: string }): Promise<CreatedApiKey> {
    const body: JsonRequestBody<operations['createApiKey']> = input
    return apiRequest<JsonResponse<operations['createApiKey'], 201>>('/api/v1/api-keys', {
      method: 'POST',
      body: JSON.stringify(body),
      cache: 'no-store',
    })
  }

  async deleteApiKey(id: string): Promise<void> {
    await apiRequest<void>(`/api/v1/api-keys/${encodeURIComponent(id)}`, { method: 'DELETE' })
  }
}
