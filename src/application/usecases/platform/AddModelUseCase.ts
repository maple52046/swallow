import type { Model, CreateModelInput } from '@/domain/platform/types'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class AddModelUseCase {
  constructor(private readonly repo: PlatformRepository) {}

  async execute(model: CreateModelInput): Promise<Model> {
    const created = await this.repo.addModel(model)
    await this.repo.appendAuditEvent({
      type: 'model.added',
      actor: 'user',
      resourceType: 'model',
      resourceId: created.id,
      resourceName: created.name,
      description: `Model "${created.name}" added (${created.type})`,
      timestamp: new Date().toISOString(),
    })
    return created
  }
}
