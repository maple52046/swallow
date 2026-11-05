import type { GPUOperatorRelease, GPUOperatorSettings, PlatformChangeResult } from '@/domain/platform/types'

/** AMD-owned GPU Operator catalog and Platform mutation surface. */
export interface AMDPlatformPort {
  listGPUOperatorReleases(): Promise<GPUOperatorRelease[]>
  configureGPUOperator(platformId: string, settings: GPUOperatorSettings): Promise<PlatformChangeResult>
}
