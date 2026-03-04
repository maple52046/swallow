import type { Alert } from '@/domain/alert/types'
import type { ObservabilityRepository, ListAlertsFilters } from '@/application/ports/ObservabilityRepository'

export class ListAlertsUseCase {
  constructor(private readonly repo: ObservabilityRepository) {}

  async execute(filters?: ListAlertsFilters): Promise<Alert[]> {
    return this.repo.listAlerts(filters)
  }
}
