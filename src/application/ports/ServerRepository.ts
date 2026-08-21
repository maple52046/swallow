import type {
  DeployServerInput,
  ListServersFilters,
  ProvisioningActionResult,
  Server,
} from '@/domain/server/types'

export interface Paginated<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
}

/**
 * Servers are read-only: the backend produces them by reconciling provisioner
 * inventory, so there is no create or delete here. The two actions delegate to the
 * server's own provisioner and return a state snapshot, not a completion.
 */
export interface ServerRepository {
  listServers(filters?: ListServersFilters): Promise<Paginated<Server>>
  getServer(id: string): Promise<Server | null>
  deployServer(id: string, input: DeployServerInput): Promise<ProvisioningActionResult>
  releaseServer(id: string): Promise<ProvisioningActionResult>
}
