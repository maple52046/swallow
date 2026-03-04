import type { Run } from '@/domain/run/types'
import type { RunRepository, ListRunsFilters } from '@/application/ports/RunRepository'

export class ListRunsUseCase {
  constructor(private readonly repo: RunRepository) {}

  async execute(filters?: ListRunsFilters): Promise<Run[]> {
    return this.repo.list(filters)
  }
}
