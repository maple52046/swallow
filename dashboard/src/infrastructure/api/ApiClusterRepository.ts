import type { ClusterRepository } from '@/application/ports/ClusterRepository'
import type {
  Cluster,
  DeployClusterInput,
  DeployClusterResult,
  MembershipReport,
} from '@/domain/cluster/types'
import { ApiRequestError, apiRequest } from './client'

/**
 * The cluster contract maps directly onto the domain type, so there is no reshaping here.
 * The one translation is 404 on getCluster, which is a stale link rather than an error to
 * surface, matching how the server repository treats a missing server.
 */
export class ApiClusterRepository implements ClusterRepository {
  async listClusters(siteId?: string): Promise<Cluster[]> {
    const suffix = siteId ? `?siteId=${encodeURIComponent(siteId)}` : ''
    return apiRequest<Cluster[]>(`/api/v1/clusters${suffix}`)
  }

  async getCluster(id: string): Promise<Cluster | null> {
    try {
      return await apiRequest<Cluster>(`/api/v1/clusters/${encodeURIComponent(id)}`)
    } catch (error) {
      if (error instanceof ApiRequestError && error.status === 404) {
        return null
      }
      throw error
    }
  }

  async deployCluster(input: DeployClusterInput): Promise<DeployClusterResult> {
    return apiRequest<DeployClusterResult>('/api/v1/clusters/deploy', {
      method: 'POST',
      body: JSON.stringify(input),
    })
  }
  async uninstallCluster(id: string): Promise<DeployClusterResult> {
    return apiRequest<DeployClusterResult>(
      `/api/v1/clusters/${encodeURIComponent(id)}/uninstall`,
      { method: 'POST' },
    )
  }

  async deleteCluster(id: string): Promise<void> {
    await apiRequest<{ success: boolean }>(`/api/v1/clusters/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    })
  }


  async syncCluster(id: string): Promise<MembershipReport> {
    return apiRequest<MembershipReport>(`/api/v1/clusters/${encodeURIComponent(id)}/sync`, {
      method: 'POST',
    })
  }
}
