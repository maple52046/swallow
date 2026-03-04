import type { Mission } from '@/domain/mission/types'
import type { MissionRepository } from '@/application/ports/MissionRepository'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class ArchiveMissionUseCase {
  constructor(
    private readonly missionRepo: MissionRepository,
    private readonly platformRepo: PlatformRepository,
  ) {}

  async execute(id: string): Promise<Mission> {
    const mission = await this.missionRepo.update(id, { status: 'archived' })
    await this.platformRepo.appendAuditEvent({
      type: 'mission.archived',
      actor: 'user',
      resourceType: 'mission',
      resourceId: id,
      resourceName: mission.name,
      description: `Mission "${mission.name}" archived`,
      timestamp: new Date().toISOString(),
    })
    return mission
  }
}
