import type { Agent } from '@/domain/platform/types'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class ListAgentsUseCase {
  constructor(private readonly repo: PlatformRepository) {}

  async execute(): Promise<Agent[]> {
    return this.repo.listAgents()
  }
}
