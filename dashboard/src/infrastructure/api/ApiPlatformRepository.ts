import type { PlatformRepository } from '@/application/ports/PlatformRepository'
import type {
  Platform,
  DeployPlatformInput,
  DeployPlatformResult,
  MembershipReport,
  SlurmCluster,
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

  async getSlurmCluster(id: string): Promise<SlurmCluster | null> {
    try {
      const cluster = await apiRequest<SlurmCluster>(
        `/api/v1/platforms/${encodeURIComponent(id)}/slurm`,
      )
      // Normalize nullable collections so the view can iterate without guards.
      return {
        controllers: cluster.controllers ?? [],
        partitions: cluster.partitions ?? [],
        nodes: (cluster.nodes ?? []).map((node) => ({
          ...node,
          partitions: node.partitions ?? [],
        })),
      }
    } catch (error) {
      // Any handled API response (non-Slurm platform, no slurmrestd integration yet,
      // slurmrestd unreachable or rejecting) means there is no live view; degrade to null so
      // the Slurm view falls back to deployment intent plus membership. Only an unexpected,
      // non-API failure propagates.
      if (error instanceof ApiRequestError) {
        return null
      }
      throw error
    }
  }
}
