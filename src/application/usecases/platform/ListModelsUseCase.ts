import type { Model } from '@/domain/platform/types'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class ListModelsUseCase {
  constructor(private readonly repo: PlatformRepository) {}

  async execute(): Promise<Model[]> {
    return this.repo.listModels()
  }
}
