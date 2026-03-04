import type { Run } from '@/domain/run/types'
import type { MissionRepository } from '@/application/ports/MissionRepository'
import type { RunRepository } from '@/application/ports/RunRepository'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class RerunUseCase {
  constructor(
    private readonly missionRepo: MissionRepository,
    private readonly runRepo: RunRepository,
    private readonly platformRepo: PlatformRepository,
  ) {}

  async execute(runId: string): Promise<Run> {
    const sourceRun = await this.runRepo.get(runId)
    if (!sourceRun) throw new Error(`Run ${runId} not found`)

    const mission = await this.missionRepo.get(sourceRun.missionId)
    if (!mission) throw new Error(`Mission ${sourceRun.missionId} not found`)

    const newRun = await this.runRepo.create({
      missionId: mission.id,
      missionName: mission.name,
      trigger: 'rerun',
      model: sourceRun.model,
      target: sourceRun.target,
      goal: sourceRun.goal,
      stepNames: mission.plan.steps.map((s) => s.name),
    })

    await this.platformRepo.appendAuditEvent({
      type: 'run.started',
      actor: 'user',
      resourceType: 'run',
      resourceId: newRun.id,
      resourceName: `Rerun for ${mission.name}`,
      description: `Rerun started for mission "${mission.name}"`,
      timestamp: new Date().toISOString(),
    })

    return newRun
  }
}
