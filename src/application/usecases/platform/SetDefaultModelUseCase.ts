import type { Model } from '@/domain/platform/types'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class SetDefaultModelUseCase {
  constructor(private readonly repo: PlatformRepository) {}

  async execute(id: string): Promise<Model[]> {
    const models = await this.repo.listModels()
    await Promise.all(
      models.map((m) => this.repo.updateModel(m.id, { isDefault: m.id === id })),
    )
    await this.repo.appendAuditEvent({
      type: 'model.default_set',
      actor: 'user',
      resourceType: 'model',
      resourceId: id,
      resourceName: models.find((m) => m.id === id)?.name ?? id,
      description: `Default model set to "${models.find((m) => m.id === id)?.name}"`,
      timestamp: new Date().toISOString(),
    })
    return this.repo.listModels()
  }
}
