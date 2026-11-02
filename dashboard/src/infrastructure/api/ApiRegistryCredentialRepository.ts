import type { RegistryCredentialRepository } from '@/application/ports/RegistryCredentialRepository'
import type {
  CreateRegistryCredentialInput,
  RegistryCredential,
  ReplaceRegistryCredentialInput,
} from '@/domain/software/docker'
import { apiRequest } from './client'
import type { JsonRequestBody, JsonResponse } from './contractTypes'
import type { operations } from './generated/openapi'

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
    return (await apiRequest<JsonResponse<operations['listRegistryCredentials'], 200>>(BASE)).items
  }

  async create(input: CreateRegistryCredentialInput): Promise<RegistryCredential> {
    const body: JsonRequestBody<operations['createRegistryCredential']> = input
    return apiRequest<JsonResponse<operations['createRegistryCredential'], 201>>(BASE, { method: 'POST', body: JSON.stringify(body) })
  }

  async replace(id: string, input: ReplaceRegistryCredentialInput): Promise<RegistryCredential> {
    const body: JsonRequestBody<operations['replaceRegistryCredential']> = input
    return apiRequest<JsonResponse<operations['replaceRegistryCredential'], 200>>(`${BASE}/${encodeURIComponent(id)}`, {
      method: 'PUT', body: JSON.stringify(body),
    })
  }

  async remove(id: string): Promise<void> {
    await apiRequest(`${BASE}/${encodeURIComponent(id)}`, { method: 'DELETE' })
  }
}
