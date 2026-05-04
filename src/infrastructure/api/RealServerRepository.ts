import type { Server, ServerStatus, CreateServerInput } from '@/domain/server/types'
import type { ServerRepository, ListServersFilters } from '@/application/ports/ServerRepository'
import { MockServerRepository } from '@/infrastructure/mock/repos/MockServerRepository'
import * as serverApi from './serverApi'
import type { ServerListItem } from './types'

function mapToServer(item: ServerListItem): Server {
  const inv = item.inventory
  const gpus = inv?.gpus ?? []
  return {
    id: item.id,
    hostname: item.hostname,
    ip: item.ip,
    status: item.status,
    createdAt: item.createdAt,
    updatedAt: item.updatedAt,
    cpuCores: inv?.cpu.cores ?? 0,
    ramGB: inv ? Math.round(inv.memory.totalKB / (1024 * 1024)) : 0,
    cpuUsagePct: 0,
    ramUsagePct: 0,
    gpuType: gpus[0]?.model ?? '',
    gpuCount: gpus.length,
    os: inv?.os.distribution && inv.os.version
      ? `${inv.os.distribution} ${inv.os.version}`
      : undefined,
    ownerTeamId: null,
    ownerUserId: null,
    lastSeenAt: item.agent?.lastSeenAt ?? item.updatedAt,
  }
}

export class RealServerRepository implements ServerRepository {
  // Mock repo handles mutations not yet implemented in the backend
  // (assignToTeam, assignToUser, unassign, updateStatus).
  private mock = new MockServerRepository()

  async listServers(filters?: ListServersFilters): Promise<Server[]> {
    const response = await serverApi.listServers({
      status: filters?.status,
      // ListServersFilters.search maps to backend query param `keyword`
      keyword: filters?.search,
    })

    let items = response.items.map(mapToServer)

    // TODO(api): allocation / teamId / userId filters are not yet supported by the backend.
    // Apply them client-side using the data we have.
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

    return items.sort((a, b) => a.hostname.localeCompare(b.hostname))
  }

  async getServer(id: string): Promise<Server | null> {
    // TODO(api): Backend does not implement GET /servers/:id yet.
    // Fetch the full list and find by ID as a fallback.
    const response = await serverApi.listServers({ pageSize: 100 })
    const item = response.items.find((s) => s.id === id)
    return item ? mapToServer(item) : null
  }

  async createServer(input: CreateServerInput): Promise<Server> {
    const { id } = await serverApi.createServer({
      hostname: input.hostname,
      ip: input.ip,
    })

    // The backend only returns the new server's ID.
    // Construct a Server object from the input + default values.
    const ts = new Date().toISOString()
    return {
      id,
      hostname: input.hostname,
      ip: input.ip,
      status: 'unknown',
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
  }

  // TODO(api): Backend does not implement DELETE /servers/:id as a repository method yet.
  // This delegates to the real API delete endpoint.
  async deleteServer(id: string): Promise<void> {
    await serverApi.deleteServer(id)
  }

  // The following methods are not yet implemented in the backend.
  // They delegate to the mock repository for in-memory simulation.

  async assignToTeam(id: string, teamId: string): Promise<Server> {
    // TODO(api): Backend does not currently implement owner assignment.
    return this.mock.assignToTeam(id, teamId)
  }

  async assignToUser(id: string, userId: string): Promise<Server> {
    // TODO(api): Backend does not currently implement owner assignment.
    return this.mock.assignToUser(id, userId)
  }

  async unassign(id: string): Promise<Server> {
    // TODO(api): Backend does not currently implement owner unassignment.
    return this.mock.unassign(id)
  }

  async updateStatus(id: string, status: ServerStatus): Promise<Server> {
    // TODO(api): Backend does not currently implement status update via API.
    return this.mock.updateStatus(id, status)
  }
}
