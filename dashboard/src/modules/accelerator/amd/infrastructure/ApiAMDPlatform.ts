import type { operations } from '@/infrastructure/api/generated/openapi'
import type { JsonRequestBody, JsonResponse } from '@/infrastructure/api/contractTypes'
import { apiRequest } from '@/infrastructure/api/client'
import { APPROVE_PLATFORM_WRITE, toPlatformChangeResult, type PlatformSpecWrite } from '@/application/platform/platformChangeResult'
import type { GPUOperatorSettings } from '@/domain/platform/types'
import type { AMDPlatformPort } from '../AMDPlatformPort'

/** Contract-typed adapter for AMD GPU Operator catalog and spec convergence. */
export class ApiAMDPlatform implements AMDPlatformPort {
  async listGPUOperatorReleases() {
    const response = await apiRequest<JsonResponse<operations['listGPUOperatorReleases'], 200>>(
      '/api/v1/platforms/gpu-operator/releases',
    )
    return response.items ?? []
  }

  async configureGPUOperator(platformId: string, settings: GPUOperatorSettings) {
    const platform = await apiRequest<JsonResponse<operations['getPlatform'], 200>>(
      `/api/v1/platforms/${encodeURIComponent(platformId)}`,
    )
    if (!platform.spec) {
      throw new Error('This platform has no spec yet; Swallow adopts it shortly after the API starts.')
    }
    const { members, settings: current, machinePreparation } = platform.spec
    const spec = {
      members,
      settings: { ...current, gpuOperator: settings },
      ...(machinePreparation ? { machinePreparation } : {}),
    }
    // The read model keeps machine preparation compatibility fields wider than the generated write contract; the backend returned this exact spec, and the mutation changes only gpuOperator.
    const body = { generation: platform.generation, spec } as JsonRequestBody<operations['replacePlatformSpec']>
    const written = await apiRequest<JsonResponse<operations['replacePlatformSpec'], 200> | JsonResponse<operations['replacePlatformSpec'], 202>>(
      `/api/v1/platforms/${encodeURIComponent(platformId)}/spec?${APPROVE_PLATFORM_WRITE}`,
      { method: 'PUT', body: JSON.stringify(body) },
    )
    return toPlatformChangeResult(written as PlatformSpecWrite)
  }
}
