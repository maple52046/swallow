import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class DeleteModelUseCase {
  constructor(private readonly repo: PlatformRepository) {}

  async execute(id: string): Promise<void> {
    const models = await this.repo.listModels()
    const model = models.find((m) => m.id === id)
    await this.repo.deleteModel(id)
    await this.repo.appendAuditEvent({
      type: 'model.deleted',
      actor: 'user',
      resourceType: 'model',
      resourceId: id,
      resourceName: model?.name ?? id,
      description: `Model "${model?.name ?? id}" deleted`,
      timestamp: new Date().toISOString(),
    })
  }
}
