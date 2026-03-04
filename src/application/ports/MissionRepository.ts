import type { Mission, MissionStatus } from '@/domain/mission/types'

export interface ListMissionsFilters {
  status?: MissionStatus
  search?: string
  trigger?: string
  limit?: number
  offset?: number
}

export interface MissionRepository {
  list(filters?: ListMissionsFilters): Promise<Mission[]>
  get(id: string): Promise<Mission | null>
  create(mission: Omit<Mission, 'id' | 'createdAt' | 'updatedAt' | 'runCount'>): Promise<Mission>
  update(id: string, partial: Partial<Mission>): Promise<Mission>
  delete(id: string): Promise<void>
}
