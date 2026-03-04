import type { Plugin } from '@/domain/platform/types'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class EnableDisablePluginUseCase {
  constructor(private readonly repo: PlatformRepository) {}

  async execute(id: string, enabled: boolean): Promise<Plugin> {
    const plugin = await this.repo.updatePlugin(id, { enabled })
    await this.repo.appendAuditEvent({
      type: enabled ? 'plugin.enabled' : 'plugin.disabled',
      actor: 'user',
      resourceType: 'plugin',
      resourceId: id,
      resourceName: plugin.name,
      description: `Plugin "${plugin.name}" ${enabled ? 'enabled' : 'disabled'}`,
      timestamp: new Date().toISOString(),
    })
    return plugin
  }
}
