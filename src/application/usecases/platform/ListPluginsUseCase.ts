import type { Plugin } from '@/domain/platform/types'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class ListPluginsUseCase {
  constructor(private readonly repo: PlatformRepository) {}

  async execute(): Promise<Plugin[]> {
    return this.repo.listPlugins()
  }
}
