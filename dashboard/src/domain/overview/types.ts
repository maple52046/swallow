import type { Operation } from '@/domain/operation/types'

/** One integration row in the operator overview. */
export interface OverviewIntegration {
  id: string
  siteId: string
  name: string
  kind: string
  providerKind: string
  enabled: boolean
  lastSucceededAt: string | null
  lastError: string | null
}

/** One firing alert correlated to Swallow resources where possible. */
export interface OverviewAlert {
  fingerprint: string
  name: string
  severity: string
  state: string
  summary: string
  description: string
  labels: Record<string, string>
  startsAt: string | null
  serverId: string | null
  siteId: string | null
  platformId: string | null
}

/** Provider-owned aggregate read model for the operator landing screen. */
export interface Overview {
  generatedAt: string
  scope: { siteId: string | null }
  inventory: {
    sites: number
    servers: number
    absent: number
    deployed: number
    platformed: number
    gpuDevices: number
    health: { up: number; down: number; unknown: number }
  }
  integrations: {
    total: number
    failing: number
    items: OverviewIntegration[]
  }
  platforms: {
    total: number
    unreachable: number
    unmatchedMembers: number
  }
  operations: {
    active: number
    failedLast24Hours: number
    recent: Operation[]
  }
  monitoring: {
    available: boolean
    error: { code: string; message: string } | null
    firing: {
      critical: number
      warning: number
      items: OverviewAlert[]
    }
  }
}
