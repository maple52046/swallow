import type { Mission } from '@/domain/mission/types'
import type { MissionRepository } from '@/application/ports/MissionRepository'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'
import type { UpdateMissionPlanDto } from '@/application/dtos'

export class UpdateMissionPlanUseCase {
  constructor(
    private readonly missionRepo: MissionRepository,
    private readonly platformRepo: PlatformRepository,
  ) {}

  async execute(dto: UpdateMissionPlanDto): Promise<Mission> {
    const mission = await this.missionRepo.update(dto.missionId, { plan: dto.plan })
    await this.platformRepo.appendAuditEvent({
      type: 'mission.updated',
      actor: 'user',
      resourceType: 'mission',
      resourceId: mission.id,
      resourceName: mission.name,
      description: `Mission "${mission.name}" plan updated`,
      timestamp: new Date().toISOString(),
    })
    return mission
  }
}
