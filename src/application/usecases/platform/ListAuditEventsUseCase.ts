import type { AuditEvent } from '@/domain/audit/types'
import type { PlatformRepository, ListAuditFilters } from '@/application/ports/PlatformRepository'

export class ListAuditEventsUseCase {
  constructor(private readonly repo: PlatformRepository) {}

  async execute(filters?: ListAuditFilters): Promise<AuditEvent[]> {
    return this.repo.listAuditEvents(filters)
  }
}
