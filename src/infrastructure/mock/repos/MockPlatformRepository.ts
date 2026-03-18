import type { Agent, Model, Plugin, CreateModelInput } from '@/domain/platform/types'
import type { AuditEvent } from '@/domain/audit/types'
import type { PlatformRepository, ListAuditFilters } from '@/application/ports/PlatformRepository'
import { seedAgents, seedModels, seedPlugins, seedAuditEvents } from '@/infrastructure/mock/data/seedPlatform'
import { lsGet, lsSet } from '@/infrastructure/persistence/localStorage'

function genId() {
  return 'audit-' + Date.now().toString(36) + Math.random().toString(36).slice(2, 5)
}

export class MockPlatformRepository implements PlatformRepository {
  private agents: Map<string, Agent>
  private models: Map<string, Model>
  private plugins: Map<string, Plugin>
  private auditEvents: AuditEvent[]

  constructor() {
    this.agents = new Map(seedAgents.map((a) => [a.id, a]))
    this.models = new Map(lsGet<Model[]>('models', seedModels).map((m) => [m.id, m]))
    this.plugins = new Map(lsGet<Plugin[]>('plugins', seedPlugins).map((p) => [p.id, p]))
    this.auditEvents = lsGet<AuditEvent[]>('audit-events', seedAuditEvents)
  }

  async listAgents(): Promise<Agent[]> {
    return Array.from(this.agents.values())
  }

  async updateAgent(id: string, partial: Partial<Agent>): Promise<Agent> {
    const existing = this.agents.get(id)
    if (!existing) throw new Error('Agent not found: ' + id)
    const updated = { ...existing, ...partial }
    this.agents.set(id, updated)
    return updated
  }

  async listModels(): Promise<Model[]> {
    return Array.from(this.models.values())
  }

  async updateModel(id: string, partial: Record<string, unknown>): Promise<Model> {
    const existing = this.models.get(id)
    if (!existing) throw new Error('Model not found: ' + id)
    const updated = { ...existing, ...partial } as Model
    this.models.set(id, updated)
    lsSet('models', Array.from(this.models.values()))
    return updated
  }

  async addModel(model: CreateModelInput): Promise<Model> {
    const id = 'model-' + Date.now().toString(36) + Math.random().toString(36).slice(2, 5)
    const full = { ...model, id } as Model
    this.models.set(id, full)
    lsSet('models', Array.from(this.models.values()))
    return full
  }

  async deleteModel(id: string): Promise<void> {
    this.models.delete(id)
    lsSet('models', Array.from(this.models.values()))
  }

  async listPlugins(): Promise<Plugin[]> {
    return Array.from(this.plugins.values())
  }

  async updatePlugin(id: string, partial: Partial<Plugin>): Promise<Plugin> {
    const existing = this.plugins.get(id)
    if (!existing) throw new Error('Plugin not found: ' + id)
    const updated = { ...existing, ...partial }
    this.plugins.set(id, updated)
    lsSet('plugins', Array.from(this.plugins.values()))
    return updated
  }

  async listAuditEvents(filters?: ListAuditFilters): Promise<AuditEvent[]> {
    let items = [...this.auditEvents]
    if (filters?.type) items = items.filter((e) => e.type === filters.type)
    if (filters?.resourceType) items = items.filter((e) => e.resourceType === filters.resourceType)
    if (filters?.search) {
      const q = filters.search.toLowerCase()
      items = items.filter((e) => e.description.toLowerCase().includes(q) || e.resourceName.toLowerCase().includes(q) || e.actor.toLowerCase().includes(q))
    }
    items = items.sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime())
    if (filters?.limit) items = items.slice(filters.offset ?? 0, (filters.offset ?? 0) + filters.limit)
    return items
  }

  async appendAuditEvent(event: Omit<AuditEvent, 'id'>): Promise<AuditEvent> {
    const full: AuditEvent = { ...event, id: genId() }
    this.auditEvents = [full, ...this.auditEvents]
    lsSet('audit-events', this.auditEvents.slice(0, 500))
    return full
  }
}
