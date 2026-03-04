import type { GPUMetrics } from '@/domain/gpu/types'
import type { ObservabilityRepository } from '@/application/ports/ObservabilityRepository'

export class GetGPUMetricsUseCase {
  constructor(private readonly repo: ObservabilityRepository) {}

  async execute(gpuIds?: string[]): Promise<GPUMetrics[]> {
    return this.repo.getGPUMetrics(gpuIds)
  }
}
