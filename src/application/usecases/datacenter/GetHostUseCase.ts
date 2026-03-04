import type { Host } from '@/domain/asset/types'
import type { AssetRepository } from '@/application/ports/AssetRepository'

export class GetHostUseCase {
  constructor(private readonly repo: AssetRepository) {}

  async execute(id: string): Promise<Host | null> {
    return this.repo.getHost(id)
  }
}
