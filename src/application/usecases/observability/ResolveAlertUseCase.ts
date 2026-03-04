import type { Alert } from '@/domain/alert/types'
import type { ObservabilityRepository } from '@/application/ports/ObservabilityRepository'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class ResolveAlertUseCase {
  constructor(
    private readonly obsRepo: ObservabilityRepository,
    private readonly platformRepo: PlatformRepository,
  ) {}

  async execute(alertId: string): Promise<Alert> {
    const alert = await this.obsRepo.resolveAlert(alertId, 'user')
    await this.platformRepo.appendAuditEvent({
      type: 'alert.resolved',
      actor: 'user',
      resourceType: 'alert',
      resourceId: alertId,
      resourceName: alert.title,
      description: `Alert "${alert.title}" resolved`,
      timestamp: new Date().toISOString(),
    })
    return alert
  }
}
