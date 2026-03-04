export type AuditEventType =
  | 'mission.created'
  | 'mission.updated'
  | 'mission.paused'
  | 'mission.resumed'
  | 'mission.archived'
  | 'mission.cloned'
  | 'run.started'
  | 'run.completed'
  | 'run.failed'
  | 'run.canceled'
  | 'alert.acknowledged'
  | 'alert.resolved'
  | 'plane.registered'
  | 'plane.disconnected'
  | 'plugin.enabled'
  | 'plugin.disabled'
  | 'model.default_set'
  | 'agent.enabled'
  | 'agent.disabled'
  | 'settings.updated'

export interface AuditEvent {
  id: string
  type: AuditEventType
  actor: string
  resourceType: string
  resourceId: string
  resourceName: string
  description: string
  metadata?: Record<string, unknown>
  timestamp: string
  ipAddress?: string
}
