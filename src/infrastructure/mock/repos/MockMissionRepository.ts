import type { Mission } from '@/domain/mission/types'
import type { MissionRepository, ListMissionsFilters } from '@/application/ports/MissionRepository'
import { seedMissions } from '@/infrastructure/mock/data/seedMissions'
import { lsGet, lsSet } from '@/infrastructure/persistence/localStorage'

function genId() { return 'mission-' + Date.now().toString(36) + Math.random().toString(36).slice(2, 6) }

export class MockMissionRepository implements MissionRepository {
  private store: Map<string, Mission>

  constructor() {
    const saved = lsGet<Mission[]>('missions', seedMissions)
    this.store = new Map(saved.map((m) => [m.id, m]))
  }

  private save() { lsSet('missions', Array.from(this.store.values())) }

  async list(filters?: ListMissionsFilters): Promise<Mission[]> {
    let items = Array.from(this.store.values())
    if (filters?.status) items = items.filter((m) => m.status === filters.status)
    if (filters?.trigger) items = items.filter((m) => m.trigger === filters.trigger)
    if (filters?.search) {
      const q = filters.search.toLowerCase()
      items = items.filter((m) => m.name.toLowerCase().includes(q) || m.goal.toLowerCase().includes(q) || m.tags.some((t) => t.includes(q)))
    }
    if (filters?.limit) items = items.slice(filters.offset ?? 0, (filters.offset ?? 0) + filters.limit)
    return items.sort((a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime())
  }

  async get(id: string): Promise<Mission | null> { return this.store.get(id) ?? null }

  async create(data: Omit<Mission, 'id' | 'createdAt' | 'updatedAt' | 'runCount'>): Promise<Mission> {
    const now = new Date().toISOString()
    const mission: Mission = { ...data, id: genId(), createdAt: now, updatedAt: now, runCount: 0 }
    this.store.set(mission.id, mission)
    this.save()
    return mission
  }

  async update(id: string, partial: Partial<Mission>): Promise<Mission> {
    const existing = this.store.get(id)
    if (!existing) throw new Error('Mission not found: ' + id)
    const updated = { ...existing, ...partial, updatedAt: new Date().toISOString() }
    this.store.set(id, updated)
    this.save()
    return updated
  }

  async delete(id: string): Promise<void> { this.store.delete(id); this.save() }
}
