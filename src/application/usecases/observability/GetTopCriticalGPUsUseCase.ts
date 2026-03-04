import type { GPUDevice, GPUMetrics } from '@/domain/gpu/types'
import type { ObservabilityRepository } from '@/application/ports/ObservabilityRepository'

export class GetTopCriticalGPUsUseCase {
  constructor(private readonly repo: ObservabilityRepository) {}

  async execute(limit = 5): Promise<Array<GPUDevice & { metrics: GPUMetrics }>> {
    return this.repo.getTopCriticalGPUs(limit)
  }
}
