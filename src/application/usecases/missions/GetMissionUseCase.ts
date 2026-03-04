import type { Mission } from '@/domain/mission/types'
import type { MissionRepository } from '@/application/ports/MissionRepository'

export class GetMissionUseCase {
  constructor(private readonly repo: MissionRepository) {}

  async execute(id: string): Promise<Mission | null> {
    return this.repo.get(id)
  }
}
