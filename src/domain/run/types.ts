export type RunStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'canceled'
export type RunTrigger = 'manual' | 'scheduled' | 'event' | 'rerun'

export interface RunStep {
  id: string
  order: number
  name: string
  status: 'pending' | 'running' | 'succeeded' | 'failed' | 'skipped'
  startedAt?: string
  completedAt?: string
  durationMs?: number
}

export interface RunArtifact {
  id: string
  name: string
  type: 'json' | 'report' | 'log' | 'profile'
  url: string
  sizeBytes: number
  createdAt: string
  content?: string
}

export interface Run {
  id: string
  missionId: string
  missionName: string
  status: RunStatus
  trigger: RunTrigger
  model: string
  target: string
  goal: string
  steps: RunStep[]
  logs: string[]
  artifacts: RunArtifact[]
  queuedAt: string
  startedAt?: string
  completedAt?: string
  durationMs?: number
}
