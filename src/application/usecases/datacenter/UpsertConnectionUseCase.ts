import type { Connection } from '@/domain/asset/types'
import type { AccessRepository, UpsertConnectionInput } from '@/application/ports/AccessRepository'

export class UpsertConnectionUseCase {
  constructor(private readonly repo: AccessRepository) {}

  async execute(input: UpsertConnectionInput): Promise<Connection> {
    return this.repo.upsertConnection(input)
  }
}
