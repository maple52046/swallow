export type MissionStatus = 'active' | 'paused' | 'archived' | 'draft'
export type MissionTrigger = 'manual' | 'scheduled' | 'event'

export interface MissionStep {
  id: string
  order: number
  name: string
  description: string
  plugin: string
  action: string
  parameters: Record<string, unknown>
}

export interface MissionPlan {
  steps: MissionStep[]
  estimatedDurationSeconds: number
}

export interface MissionPermissions {
  allowedPlugins: string[]
  allowedTargets: string[]
  guardrails: string[]
}

export interface Mission {
  id: string
  name: string
  goal: string
  status: MissionStatus
  trigger: MissionTrigger
  schedule?: string
  model: string
  target: string
  plan: MissionPlan
  permissions: MissionPermissions
  createdAt: string
  updatedAt: string
  lastRunAt?: string
  runCount: number
  tags: string[]
}
