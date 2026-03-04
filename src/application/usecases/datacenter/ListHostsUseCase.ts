import type { Host } from '@/domain/asset/types'
import type { AssetRepository, ListHostsFilters } from '@/application/ports/AssetRepository'

export class ListHostsUseCase {
  constructor(private readonly repo: AssetRepository) {}

  async execute(filters?: ListHostsFilters): Promise<Host[]> {
    return this.repo.listHosts(filters)
  }
}
