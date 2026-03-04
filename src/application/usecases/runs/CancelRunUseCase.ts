import type { Run } from '@/domain/run/types'
import type { RunRepository } from '@/application/ports/RunRepository'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class CancelRunUseCase {
  constructor(
    private readonly runRepo: RunRepository,
    private readonly platformRepo: PlatformRepository,
  ) {}

  async execute(id: string): Promise<Run> {
    const run = await this.runRepo.cancel(id)
    await this.platformRepo.appendAuditEvent({
      type: 'run.canceled',
      actor: 'user',
      resourceType: 'run',
      resourceId: id,
      resourceName: `Run for ${run.missionName}`,
      description: `Run canceled for mission "${run.missionName}"`,
      timestamp: new Date().toISOString(),
    })
    return run
  }
}
