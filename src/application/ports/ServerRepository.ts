import type { Server, ServerStatus } from '@/domain/server/types'

export interface ListServersFilters {
  status?: ServerStatus
  allocation?: 'free' | 'assigned'
  teamId?: string
  userId?: string
  search?: string
}

export interface ServerRepository {
  listServers(filters?: ListServersFilters): Promise<Server[]>
  getServer(id: string): Promise<Server | null>
  assignToTeam(id: string, teamId: string): Promise<Server>
  assignToUser(id: string, userId: string): Promise<Server>
  unassign(id: string): Promise<Server>
  updateStatus(id: string, status: ServerStatus): Promise<Server>
}
