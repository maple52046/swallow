import type { Connection } from '@/domain/asset/types'
import type { AccessRepository } from '@/application/ports/AccessRepository'

export class ListConnectionsUseCase {
  constructor(private readonly repo: AccessRepository) {}

  async execute(): Promise<Connection[]> {
    return this.repo.listConnections()
  }
}
