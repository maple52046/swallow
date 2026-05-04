// ── Auth ────────────────────────────────────────────────────────────────────

export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  accessToken: string
}

// Backend uses 'user' for the regular member role.
// The frontend domain uses 'member'. Map via mapBackendRole() in AuthContext.
export interface MeResponse {
  id: string
  username: string
  role: 'admin' | 'owner' | 'user'
}

// ── Servers ──────────────────────────────────────────────────────────────────

export interface ServerListItem {
  id: string
  hostname: string
  ip: string
  status: 'unknown' | 'live' | 'warning' | 'error' | 'maintain' | 'offline'
  createdAt: string
  updatedAt: string
  inventory?: {
    cpu: { model: string; cores: number; threads: number }
    memory: { totalKB: number }
    gpus: Array<{ vendor: string; model: string; index: number }>
    os: { type: string; distribution: string; version: string; kernelVersion: string; architecture: string }
  }
  agent?: {
    status: string
    lastSeenAt: string
    agentVersion: string
  }
}

export interface ServerListResponse {
  items: ServerListItem[]
  total: number
  page: number
  pageSize: number
}

export interface CreateServerRequest {
  hostname: string
  ip: string
}

export interface CreateServerResponse {
  id: string
}

export interface DeleteServerResponse {
  success: boolean
}

export interface ListServersParams {
  page?: number
  pageSize?: number
  status?: string
  keyword?: string
}
