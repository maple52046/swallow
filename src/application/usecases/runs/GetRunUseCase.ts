import type { Run } from '@/domain/run/types'
import type { RunRepository } from '@/application/ports/RunRepository'

export class GetRunUseCase {
  constructor(private readonly repo: RunRepository) {}

  async execute(id: string): Promise<Run | null> {
    return this.repo.get(id)
  }
}
