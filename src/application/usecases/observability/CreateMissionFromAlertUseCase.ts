import type { Mission } from '@/domain/mission/types'
import type { ObservabilityRepository } from '@/application/ports/ObservabilityRepository'
import type { MissionRepository } from '@/application/ports/MissionRepository'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'
import type { CreateMissionFromAlertDto } from '@/application/dtos'

export class CreateMissionFromAlertUseCase {
  constructor(
    private readonly obsRepo: ObservabilityRepository,
    private readonly missionRepo: MissionRepository,
    private readonly platformRepo: PlatformRepository,
  ) {}

  async execute(dto: CreateMissionFromAlertDto): Promise<Mission> {
    const alert = await this.obsRepo.getAlert(dto.alertId)
    if (!alert) throw new Error(`Alert ${dto.alertId} not found`)

    const models = await this.platformRepo.listModels()
    const defaultModel = models.find((m) => m.isDefault) ?? models[0]

    const mission = await this.missionRepo.create({
      name: `Alert Response: ${alert.title}`,
      goal: dto.goal,
      status: 'draft',
      trigger: 'manual',
      model: defaultModel?.name ?? 'gpt-4o',
      target: dto.target,
      plan: {
        steps: dto.plugins.map((plugin, i) => ({
          id: `step-${i + 1}`,
          order: i + 1,
          name: `Execute ${plugin}`,
          description: `Run ${plugin} on target`,
          plugin,
          action: 'run',
          parameters: {},
        })),
        estimatedDurationSeconds: dto.plugins.length * 60,
      },
      permissions: {
        allowedPlugins: dto.plugins,
        allowedTargets: [dto.target],
        guardrails: ['require-confirmation'],
      },
      tags: ['alert-response', alert.category],
    })

    await this.platformRepo.appendAuditEvent({
      type: 'mission.created',
      actor: 'user',
      resourceType: 'mission',
      resourceId: mission.id,
      resourceName: mission.name,
      description: `Mission created from alert "${alert.title}"`,
      timestamp: new Date().toISOString(),
    })

    return mission
  }
}
