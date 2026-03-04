import type { Mission } from '@/domain/mission/types'
import type { MissionRepository } from '@/application/ports/MissionRepository'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'
import type { CreateMissionDto } from '@/application/dtos'

export class CreateMissionUseCase {
  constructor(
    private readonly missionRepo: MissionRepository,
    private readonly platformRepo: PlatformRepository,
  ) {}

  async execute(dto: CreateMissionDto): Promise<Mission> {
    const plugins = await this.platformRepo.listPlugins()
    const enabledPlugins = plugins.filter((p) => p.enabled).map((p) => p.name)
    const requestedPlugins = dto.permissions.allowedPlugins
    const disabledPlugins = requestedPlugins.filter((p) => !enabledPlugins.includes(p))
    if (disabledPlugins.length > 0) {
      throw new Error(`Plugins not enabled: ${disabledPlugins.join(', ')}`)
    }

    const mission = await this.missionRepo.create({
      ...dto,
      status: 'active',
      tags: dto.tags ?? [],
    })

    await this.platformRepo.appendAuditEvent({
      type: 'mission.created',
      actor: 'user',
      resourceType: 'mission',
      resourceId: mission.id,
      resourceName: mission.name,
      description: `Mission "${mission.name}" created`,
      timestamp: new Date().toISOString(),
    })

    return mission
  }
}
