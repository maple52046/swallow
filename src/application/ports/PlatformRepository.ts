import type { Agent, Model, Plugin } from '@/domain/platform/types'
import type { AuditEvent } from '@/domain/audit/types'

export interface ListAuditFilters {
  type?: string
  resourceType?: string
  search?: string
  limit?: number
  offset?: number
}

export interface PlatformRepository {
  listAgents(): Promise<Agent[]>
  updateAgent(id: string, partial: Partial<Agent>): Promise<Agent>
  listModels(): Promise<Model[]>
  updateModel(id: string, partial: Partial<Model>): Promise<Model>
  listPlugins(): Promise<Plugin[]>
  updatePlugin(id: string, partial: Partial<Plugin>): Promise<Plugin>
  listAuditEvents(filters?: ListAuditFilters): Promise<AuditEvent[]>
  appendAuditEvent(event: Omit<AuditEvent, 'id'>): Promise<AuditEvent>
}
