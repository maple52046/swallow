import type { Run } from '@/domain/run/types'
import type { MissionRepository } from '@/application/ports/MissionRepository'
import type { RunRepository } from '@/application/ports/RunRepository'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class RunMissionNowUseCase {
  constructor(
    private readonly missionRepo: MissionRepository,
    private readonly runRepo: RunRepository,
    private readonly platformRepo: PlatformRepository,
  ) {}

  async execute(missionId: string, trigger: 'manual' | 'rerun' = 'manual'): Promise<Run> {
    const mission = await this.missionRepo.get(missionId)
    if (!mission) throw new Error(`Mission ${missionId} not found`)

    const run = await this.runRepo.create({
      missionId: mission.id,
      missionName: mission.name,
      trigger,
      modelId: mission.modelId,
      target: mission.target,
      goal: mission.goal,
      stepNames: mission.plan.steps.map((s) => s.name),
    })

    await this.missionRepo.update(missionId, { lastRunAt: run.queuedAt })

    await this.platformRepo.appendAuditEvent({
      type: 'run.started',
      actor: 'user',
      resourceType: 'run',
      resourceId: run.id,
      resourceName: `Run for ${mission.name}`,
      description: `Run started for mission "${mission.name}"`,
      timestamp: new Date().toISOString(),
    })

    return run
  }
}
