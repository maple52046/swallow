import type { Mission } from '@/domain/mission/types'
import type { MissionRepository, ListMissionsFilters } from '@/application/ports/MissionRepository'

export class ListMissionsUseCase {
  constructor(private readonly repo: MissionRepository) {}

  async execute(filters?: ListMissionsFilters): Promise<Mission[]> {
    return this.repo.list(filters)
  }
}
