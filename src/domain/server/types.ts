export type ServerStatus = 'live' | 'warning' | 'error' | 'maintain' | 'offline'

export interface Server {
  id: string
  hostname: string
  status: ServerStatus
  ip: string
  cpuCores: number
  ramGB: number
  gpuType: string
  gpuCount: number
  ownerTeamId: string | null
  ownerUserId: string | null
  lastSeenAt: string
  createdAt: string
  updatedAt: string
}

export type AllocationState = 'free' | 'team' | 'user'

export function getAllocationState(server: Server): AllocationState {
  if (server.ownerUserId !== null) return 'user'
  if (server.ownerTeamId !== null) return 'team'
  return 'free'
}
