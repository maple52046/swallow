import type { Run, RunStatus } from '@/domain/run/types'

export interface ListRunsFilters {
  status?: RunStatus
  missionId?: string
  trigger?: string
  search?: string
  limit?: number
  offset?: number
}

export interface CreateRunInput {
  missionId: string
  missionName: string
  trigger: string
  model: string
  target: string
  goal: string
  stepNames: string[]
}

export interface RunRepository {
  list(filters?: ListRunsFilters): Promise<Run[]>
  get(id: string): Promise<Run | null>
  create(input: CreateRunInput): Promise<Run>
  update(id: string, partial: Partial<Run>): Promise<Run>
  cancel(id: string): Promise<Run>
}
