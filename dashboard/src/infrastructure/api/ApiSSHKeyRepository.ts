import type { SSHKeyRepository } from '@/application/ports/SSHKeyRepository'
import type { GeneratedAccessKey, SSHKey } from '@/domain/access/types'
import { apiRequest } from './client'

/**
 * Maps the Active SSH Keys contract (`/api/v1/ssh-keys`, `ssh-keys.md`) onto the SSHKeyRepository
 * port. The contract's JSON already matches the domain shapes, so responses are returned as-is.
 *
 * Security: the only secret that ever crosses this adapter is a private key — outbound when the
 * operator replaces the Deployment Key, inbound once from `generate`. Neither is cached, logged,
 * or written to browser storage here; the generate response is handed straight to the caller.
 */
export class ApiSSHKeyRepository implements SSHKeyRepository {
  listSSHKeys(): Promise<SSHKey[]> {
    return apiRequest<SSHKey[]>('/api/v1/ssh-keys')
  }

  getSSHKey(id: string): Promise<SSHKey> {
    return apiRequest<SSHKey>(`/api/v1/ssh-keys/${encodeURIComponent(id)}`)
  }

  importAccessKey(input: { name: string; publicKey: string }): Promise<SSHKey> {
    return apiRequest<SSHKey>('/api/v1/ssh-keys', { method: 'POST', body: JSON.stringify(input) })
  }

  generateAccessKey(name: string): Promise<GeneratedAccessKey> {
    return apiRequest<GeneratedAccessKey>('/api/v1/ssh-keys/generate', {
      method: 'POST',
      body: JSON.stringify({ name }),
      // The response carries a private key; ask the browser not to keep it in its HTTP cache.
      cache: 'no-store',
    })
  }

  async deleteAccessKey(id: string): Promise<void> {
    await apiRequest<void>(`/api/v1/ssh-keys/${encodeURIComponent(id)}`, { method: 'DELETE' })
  }

  replaceDeploymentKey(input: { privateKey: string; name?: string }): Promise<SSHKey> {
    return apiRequest<SSHKey>('/api/v1/ssh-keys/deployment', { method: 'PUT', body: JSON.stringify(input) })
  }

  regenerateDeploymentKey(): Promise<SSHKey> {
    return apiRequest<SSHKey>('/api/v1/ssh-keys/deployment/regenerate', { method: 'POST' })
  }

  async requestSync(): Promise<void> {
    await apiRequest<void>('/api/v1/ssh-keys/sync', { method: 'POST' })
  }
}
