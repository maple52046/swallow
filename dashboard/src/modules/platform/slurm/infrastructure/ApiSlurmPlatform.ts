import type { operations } from '@/infrastructure/api/generated/openapi'
import type { JsonRequestBody, JsonResponse } from '@/infrastructure/api/contractTypes'
import { apiRequest } from '@/infrastructure/api/client'
import type { MinimumResources, SlurmCluster } from '@/domain/platform/types'
import type { SlurmPlatformPort } from '../SlurmPlatformPort'

/**
 * Contract-typed Slurm adapter. Live-read failures deliberately propagate so the module can
 * distinguish a missing Integration from provider_unavailable and preserve its last-good state.
 */
export class ApiSlurmPlatform implements SlurmPlatformPort {
  async getCluster(platformId: string): Promise<SlurmCluster> {
    const cluster = await apiRequest<JsonResponse<operations['getSlurmClusterState'], 200>>(
      `/api/v1/platforms/${encodeURIComponent(platformId)}/slurm`,
    )
    return {
      controllers: cluster.controllers ?? [],
      partitions: cluster.partitions ?? [],
      nodes: (cluster.nodes ?? []).map((node) => ({ ...node, partitions: node.partitions ?? [] })),
    }
  }

  async getDeploymentRequirement() {
    return apiRequest<JsonResponse<operations['getSlurmDeploymentRequirement'], 200>>(
      '/api/v1/platforms/deployment-requirements/slurm',
    )
  }

  async replaceDeploymentRequirement(minimum: MinimumResources | null) {
    const body: JsonRequestBody<operations['replaceSlurmDeploymentRequirement']> = { minimumResources: minimum }
    return apiRequest<JsonResponse<operations['replaceSlurmDeploymentRequirement'], 200>>(
      '/api/v1/platforms/deployment-requirements/slurm',
      { method: 'PUT', body: JSON.stringify(body) },
    )
  }
}
