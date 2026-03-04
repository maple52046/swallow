import type { Agent } from '@/domain/platform/types'
import type { PlatformRepository } from '@/application/ports/PlatformRepository'

export class EnableDisableAgentUseCase {
  constructor(private readonly repo: PlatformRepository) {}

  async execute(id: string, enabled: boolean): Promise<Agent> {
    const agent = await this.repo.updateAgent(id, { enabled })
    await this.repo.appendAuditEvent({
      type: enabled ? 'agent.enabled' : 'agent.disabled',
      actor: 'user',
      resourceType: 'agent',
      resourceId: id,
      resourceName: agent.name,
      description: `Agent "${agent.name}" ${enabled ? 'enabled' : 'disabled'}`,
      timestamp: new Date().toISOString(),
    })
    return agent
  }
}
