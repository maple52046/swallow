import type { operations } from '@/infrastructure/api/generated/openapi'
import type { JsonRequestBody, JsonResponse } from '@/infrastructure/api/contractTypes'
import { apiRequest } from '@/infrastructure/api/client'
import { toPlatformChangeResult, type PlatformSpecWrite } from '@/application/platform/platformChangeResult'
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

  // Reads the Platform right before the write so the spec and generation sent are the stored ones;
  // a write that races another one is the API's `conflict`, which the caller shows and re-reads.
  async setHardwareCounters(platformId: string, wanted: boolean) {
    const platform = await apiRequest<JsonResponse<operations['getPlatform'], 200>>(
      `/api/v1/platforms/${encodeURIComponent(platformId)}`,
    )
    if (!platform.spec) {
      throw new Error('This platform has no spec yet; Swallow adopts it shortly after the API starts.')
    }
    const { members, settings: current, machinePreparation } = platform.spec
    const omnistat = current.omnistat
    if (omnistat === null || typeof omnistat !== 'object' || Array.isArray(omnistat) || !('enabled' in omnistat) || omnistat.enabled !== true) {
      throw new Error('This Platform\u2019s spec has no Omnistat, so it has no hardware counters to switch.')
    }
    const spec = {
      members,
      settings: { ...current, omnistat: { ...omnistat, hardwareCounters: wanted } },
      ...(machinePreparation ? { machinePreparation } : {}),
    }
    // The read model keeps machine preparation compatibility fields wider than the generated write contract; the backend returned this exact spec, and the mutation changes only omnistat.hardwareCounters.
    const body = { generation: platform.generation, spec } as JsonRequestBody<operations['replacePlatformSpec']>
    const written = await apiRequest<JsonResponse<operations['replacePlatformSpec'], 200> | JsonResponse<operations['replacePlatformSpec'], 202>>(
      `/api/v1/platforms/${encodeURIComponent(platformId)}/spec`,
      { method: 'PUT', body: JSON.stringify(body) },
    )
    return toPlatformChangeResult(written as PlatformSpecWrite)
  }
}
