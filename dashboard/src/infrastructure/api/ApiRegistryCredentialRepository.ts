import type { RegistryCredentialRepository } from '@/application/ports/RegistryCredentialRepository'
import type {
  CreateRegistryCredentialInput,
  RegistryCredential,
  ReplaceRegistryCredentialInput,
} from '@/domain/software/docker'
import { apiRequest } from './client'

const BASE = '/api/v1/software/docker-ce/registry-credentials'

/**
 * Maps the Active Registry Credentials contract (`registry-credentials.md`) onto the port.
 *
 * Response JSON already matches the domain shape. The password leaves the browser only in the
 * create/replace request body over the same-origin API; it is never cached, logged, or stored by the
 * dashboard, and no response contains it.
 */
export class ApiRegistryCredentialRepository implements RegistryCredentialRepository {
  async list(): Promise<RegistryCredential[]> {
    return (await apiRequest<{ items: RegistryCredential[] }>(BASE)).items
  }

  async create(input: CreateRegistryCredentialInput): Promise<RegistryCredential> {
    return apiRequest<RegistryCredential>(BASE, { method: 'POST', body: JSON.stringify(input) })
  }

  async replace(id: string, input: ReplaceRegistryCredentialInput): Promise<RegistryCredential> {
    return apiRequest<RegistryCredential>(`${BASE}/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(input) })
  }

  async remove(id: string): Promise<void> {
    await apiRequest(`${BASE}/${encodeURIComponent(id)}`, { method: 'DELETE' })
  }
}
