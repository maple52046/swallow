import type { GPUProfile } from '@/domain/gpu/types'
import type { ObservabilityRepository } from '@/application/ports/ObservabilityRepository'

export class ListGPUProfilesUseCase {
  constructor(private readonly repo: ObservabilityRepository) {}

  async execute(limit?: number): Promise<GPUProfile[]> {
    return this.repo.listGPUProfiles(limit)
  }
}
