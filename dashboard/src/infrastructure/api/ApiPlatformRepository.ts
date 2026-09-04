import type { PlatformRepository } from '@/application/ports/PlatformRepository'
import type {
  Platform,
  DeployPlatformInput,
  DeployPlatformResult,
  MembershipReport,
  UninstallPlatformOptions,
} from '@/domain/platform/types'
import { ApiRequestError, apiRequest } from './client'

/**
 * The platform contract maps directly onto the domain type, so there is no reshaping here.
 * The one translation is 404 on getPlatform, which is a stale link rather than an error to
 * surface, matching how the server repository treats a missing server.
 */
export class ApiPlatformRepository implements PlatformRepository {
  async listPlatforms(siteId?: string): Promise<Platform[]> {
    const suffix = siteId ? `?siteId=${encodeURIComponent(siteId)}` : ''
    return apiRequest<Platform[]>(`/api/v1/platforms${suffix}`)
  }

  async getPlatform(id: string): Promise<Platform | null> {
    try {
      return await apiRequest<Platform>(`/api/v1/platforms/${encodeURIComponent(id)}`)
    } catch (error) {
      if (error instanceof ApiRequestError && error.status === 404) {
        return null
      }
      throw error
    }
  }

  async deployPlatform(input: DeployPlatformInput): Promise<DeployPlatformResult> {
    return apiRequest<DeployPlatformResult>('/api/v1/platforms/deploy', {
      method: 'POST',
      body: JSON.stringify(input),
    })
  }
  async uninstallPlatform(
    id: string,
    options?: UninstallPlatformOptions,
  ): Promise<DeployPlatformResult> {
    // An absent body keeps the k0s-only uninstall; a body opts into releasing members.
    return apiRequest<DeployPlatformResult>(
      `/api/v1/platforms/${encodeURIComponent(id)}/uninstall`,
      options ? { method: 'POST', body: JSON.stringify(options) } : { method: 'POST' },
    )
  }

  async deletePlatform(id: string): Promise<void> {
    await apiRequest<{ success: boolean }>(`/api/v1/platforms/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    })
  }


  async syncPlatform(id: string): Promise<MembershipReport> {
    return apiRequest<MembershipReport>(`/api/v1/platforms/${encodeURIComponent(id)}/sync`, {
      method: 'POST',
    })
  }
}
