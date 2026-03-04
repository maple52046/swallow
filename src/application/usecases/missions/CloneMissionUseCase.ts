import type { Mission } from '@/domain/mission/types'
import type { MissionRepository } from '@/application/ports/MissionRepository'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class CloneMissionUseCase {
  constructor(
    private readonly missionRepo: MissionRepository,
    private readonly platformRepo: PlatformRepository,
  ) {}

  async execute(id: string): Promise<Mission> {
    const source = await this.missionRepo.get(id)
    if (!source) throw new Error(`Mission ${id} not found`)

    const cloned = await this.missionRepo.create({
      ...source,
      name: `${source.name} (Copy)`,
      status: 'draft',
      lastRunAt: undefined,
    })

    await this.platformRepo.appendAuditEvent({
      type: 'mission.cloned',
      actor: 'user',
      resourceType: 'mission',
      resourceId: cloned.id,
      resourceName: cloned.name,
      description: `Mission "${source.name}" cloned as "${cloned.name}"`,
      timestamp: new Date().toISOString(),
    })

    return cloned
  }
}
