import type { SSHKeyRepository } from '@/application/ports/SSHKeyRepository'
import type { GeneratedAccessKey, SSHKey } from '@/domain/access/types'
import { apiRequest } from './client'
import type { JsonRequestBody, JsonResponse } from './contractTypes'
import type { operations } from './generated/openapi'

/**
 * Maps the Active SSH Keys contract (`/api/v1/ssh-keys`, `ssh-keys.md`) onto the SSHKeyRepository
 * port. The contract's JSON already matches the domain shapes, so responses are returned as-is;
 * the generated request and response types prove it where each is sent and returned.
 *
 * Security: the only secret that ever crosses this adapter is a private key — outbound when the
 * user replaces the Deployment Key, inbound once from `generate`. Neither is cached, logged,
 * or written to browser storage here; the generate response is handed straight to the caller.
 */
export class ApiSSHKeyRepository implements SSHKeyRepository {
  listSSHKeys(): Promise<SSHKey[]> {
    return apiRequest<JsonResponse<operations['listSshKeys'], 200>>('/api/v1/ssh-keys')
  }

  getSSHKey(id: string): Promise<SSHKey> {
    return apiRequest<JsonResponse<operations['getSshKey'], 200>>(`/api/v1/ssh-keys/${encodeURIComponent(id)}`)
  }

  importAccessKey(input: { name: string; publicKey: string }): Promise<SSHKey> {
    const body: JsonRequestBody<operations['importSshKey']> = input
    return apiRequest<JsonResponse<operations['importSshKey'], 201>>('/api/v1/ssh-keys', { method: 'POST', body: JSON.stringify(body) })
  }

  generateAccessKey(name: string): Promise<GeneratedAccessKey> {
    const body: JsonRequestBody<operations['generateSshKey']> = { name }
    return apiRequest<JsonResponse<operations['generateSshKey'], 201>>('/api/v1/ssh-keys/generate', {
      method: 'POST',
      body: JSON.stringify(body),
      // The response carries a private key; ask the browser not to keep it in its HTTP cache.
      cache: 'no-store',
    })
  }

  async deleteAccessKey(id: string): Promise<void> {
    await apiRequest<void>(`/api/v1/ssh-keys/${encodeURIComponent(id)}`, { method: 'DELETE' })
  }

  replaceDeploymentKey(input: { privateKey: string; name?: string }): Promise<SSHKey> {
    const body: JsonRequestBody<operations['replaceDeploymentKey']> = input
    return apiRequest<JsonResponse<operations['replaceDeploymentKey'], 200>>('/api/v1/ssh-keys/deployment', {
      method: 'PUT', body: JSON.stringify(body),
    })
  }

  regenerateDeploymentKey(): Promise<SSHKey> {
    return apiRequest<JsonResponse<operations['regenerateDeploymentKey'], 200>>('/api/v1/ssh-keys/deployment/regenerate', { method: 'POST' })
  }

  async requestSync(): Promise<void> {
    await apiRequest<void>('/api/v1/ssh-keys/sync', { method: 'POST' })
  }
}
