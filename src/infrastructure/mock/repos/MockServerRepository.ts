import type { Server, ServerStatus, CreateServerInput } from '@/domain/server/types'
import type { ServerRepository, ListServersFilters } from '@/application/ports/ServerRepository'
import { seedServers } from '@/infrastructure/mock/data/seedServers'
import { lsGet, lsSet } from '@/infrastructure/persistence/localStorage'

const LS_KEY = 'servers'

function now() {
  return new Date().toISOString()
}

export class MockServerRepository implements ServerRepository {
  private servers: Map<string, Server>

  constructor() {
    const stored = lsGet<Server[] | null>(LS_KEY, null)
    if (stored) {
      // Merge seed defaults for any fields missing in localStorage (e.g. after schema updates)
      const seedById = new Map(seedServers.map((s) => [s.id, s]))
      const merged = stored.map((s) => ({ ...seedById.get(s.id), ...s })) as Server[]
      this.servers = new Map(merged.map((s) => [s.id, s]))
    } else {
      this.servers = new Map(seedServers.map((s) => [s.id, s]))
    }
  }

  private persist() {
    lsSet(LS_KEY, Array.from(this.servers.values()))
  }

  async listServers(filters?: ListServersFilters): Promise<Server[]> {
    let items = Array.from(this.servers.values())

    if (filters?.status) {
      items = items.filter((s) => s.status === filters.status)
    }
    if (filters?.allocation === 'free') {
      items = items.filter((s) => s.ownerTeamId === null && s.ownerUserId === null)
    } else if (filters?.allocation === 'assigned') {
      items = items.filter((s) => s.ownerTeamId !== null || s.ownerUserId !== null)
    }
    if (filters?.teamId) {
      items = items.filter((s) => s.ownerTeamId === filters.teamId)
    }
    if (filters?.userId) {
      items = items.filter((s) => s.ownerUserId === filters.userId)
    }
    if (filters?.search) {
      const q = filters.search.toLowerCase()
      items = items.filter(
        (s) => s.hostname.toLowerCase().includes(q) || s.ip.includes(q),
      )
    }

    return items.sort((a, b) => a.hostname.localeCompare(b.hostname))
  }

  async getServer(id: string): Promise<Server | null> {
    return this.servers.get(id) ?? null
  }

  async createServer(input: CreateServerInput): Promise<Server> {
    const id = 'srv-' + Date.now().toString(36) + Math.random().toString(36).slice(2, 5)
    const ts = now()
    const server: Server = {
      id,
      hostname: input.hostname,
      ip: input.ip,
      status: input.status ?? 'unknown',
      cpuCores: input.cpuCores ?? 0,
      ramGB: input.ramGB ?? 0,
      cpuUsagePct: 0,
      ramUsagePct: 0,
      gpuType: input.gpuType ?? '',
      gpuCount: input.gpuCount ?? 0,
      ownerTeamId: input.ownerTeamId ?? null,
      ownerUserId: input.ownerUserId ?? null,
      location: input.location,
      bmc: input.bmc,
      ssh: input.ssh,
      os: input.os,
      lastSeenAt: ts,
      createdAt: ts,
      updatedAt: ts,
    }
    this.servers.set(id, server)
    this.persist()
    return server
  }

  async assignToTeam(id: string, teamId: string): Promise<Server> {
    const server = this.servers.get(id)
    if (!server) throw new Error(`Server not found: ${id}`)
    const updated: Server = { ...server, ownerTeamId: teamId, ownerUserId: null, updatedAt: now() }
    this.servers.set(id, updated)
    this.persist()
    return updated
  }

  async assignToUser(id: string, userId: string): Promise<Server> {
    const server = this.servers.get(id)
    if (!server) throw new Error(`Server not found: ${id}`)
    const updated: Server = { ...server, ownerTeamId: null, ownerUserId: userId, updatedAt: now() }
    this.servers.set(id, updated)
    this.persist()
    return updated
  }

  async unassign(id: string): Promise<Server> {
    const server = this.servers.get(id)
    if (!server) throw new Error(`Server not found: ${id}`)
    const updated: Server = { ...server, ownerTeamId: null, ownerUserId: null, updatedAt: now() }
    this.servers.set(id, updated)
    this.persist()
    return updated
  }

  async updateStatus(id: string, status: ServerStatus): Promise<Server> {
    const server = this.servers.get(id)
    if (!server) throw new Error(`Server not found: ${id}`)
    const updated: Server = { ...server, status, updatedAt: now() }
    this.servers.set(id, updated)
    this.persist()
    return updated
  }
}
