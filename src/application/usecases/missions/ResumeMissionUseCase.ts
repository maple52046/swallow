import type { Mission } from '@/domain/mission/types'
import type { MissionRepository } from '@/application/ports/MissionRepository'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class ResumeMissionUseCase {
  constructor(
    private readonly missionRepo: MissionRepository,
    private readonly platformRepo: PlatformRepository,
  ) {}

  async execute(id: string): Promise<Mission> {
    const mission = await this.missionRepo.update(id, { status: 'active' })
    await this.platformRepo.appendAuditEvent({
      type: 'mission.resumed',
      actor: 'user',
      resourceType: 'mission',
      resourceId: id,
      resourceName: mission.name,
      description: `Mission "${mission.name}" resumed`,
      timestamp: new Date().toISOString(),
    })
    return mission
  }
}
