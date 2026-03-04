import type { Alert } from '@/domain/alert/types'
import type { ObservabilityRepository } from '@/application/ports/ObservabilityRepository'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class AckAlertUseCase {
  constructor(
    private readonly obsRepo: ObservabilityRepository,
    private readonly platformRepo: PlatformRepository,
  ) {}

  async execute(alertId: string): Promise<Alert> {
    const alert = await this.obsRepo.acknowledgeAlert(alertId, 'user')
    await this.platformRepo.appendAuditEvent({
      type: 'alert.acknowledged',
      actor: 'user',
      resourceType: 'alert',
      resourceId: alertId,
      resourceName: alert.title,
      description: `Alert "${alert.title}" acknowledged`,
      timestamp: new Date().toISOString(),
    })
    return alert
  }
}
