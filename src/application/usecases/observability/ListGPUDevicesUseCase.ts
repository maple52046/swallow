import type { GPUDevice } from '@/domain/gpu/types'
import type { ObservabilityRepository, ListGPUDevicesFilters } from '@/application/ports/ObservabilityRepository'

export class ListGPUDevicesUseCase {
  constructor(private readonly repo: ObservabilityRepository) {}

  async execute(filters?: ListGPUDevicesFilters): Promise<GPUDevice[]> {
    return this.repo.listGPUDevices(filters)
  }
}
