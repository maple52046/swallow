export type ServerStatus = 'live' | 'warning' | 'error' | 'maintain' | 'offline' | 'unknown'

export interface ServerLocation {
  datacenter?: string
  room?: string
  rack?: string
  datacenterId?: string
  roomId?: string
  rackId?: string
}

export interface ServerIPMIConfig {
  host: string
  port?: number
  username?: string
  password?: string
}

export interface ServerRedfishConfig {
  host: string
  port?: number
  username?: string
  password?: string
}

export interface ServerBMC {
  address: string
  vendor?: string
  ipmi?: ServerIPMIConfig
  redfish?: ServerRedfishConfig
}

export interface ServerSSH {
  host: string
  port: number
  username: string
  keyAuthEnabled?: boolean
  passwordAuthEnabled?: boolean
  password?: string
}

export interface Server {
  location?: ServerLocation
  bmc?: ServerBMC
  ssh?: ServerSSH
  os?: string
  id: string
  hostname: string
  status: ServerStatus
  ip: string
  cpuCores: number
  ramGB: number
  cpuUsagePct: number
  ramUsagePct: number
  gpuType: string
  gpuCount: number
  ownerTeamId: string | null
  ownerUserId: string | null
  lastSeenAt: string
  createdAt: string
  updatedAt: string
}

export interface CreateServerInput {
  hostname: string
  ip: string
  status?: ServerStatus
  cpuCores?: number
  ramGB?: number
  gpuType?: string
  gpuCount?: number
  ownerTeamId?: string | null
  ownerUserId?: string | null
  location?: ServerLocation
  bmc?: ServerBMC
  ssh?: ServerSSH
  os?: string
}

export type AllocationState = 'free' | 'team' | 'user'

export function getAllocationState(server: Server): AllocationState {
  if (server.ownerUserId !== null) return 'user'
  if (server.ownerTeamId !== null) return 'team'
  return 'free'
}
