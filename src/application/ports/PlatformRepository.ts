import type { Agent, Model, Plugin, CreateModelInput } from '@/domain/platform/types'
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
  updateModel(id: string, partial: Record<string, unknown>): Promise<Model>
  addModel(model: CreateModelInput): Promise<Model>
  deleteModel(id: string): Promise<void>
  listPlugins(): Promise<Plugin[]>
  updatePlugin(id: string, partial: Partial<Plugin>): Promise<Plugin>
  listAuditEvents(filters?: ListAuditFilters): Promise<AuditEvent[]>
  appendAuditEvent(event: Omit<AuditEvent, 'id'>): Promise<AuditEvent>
}
