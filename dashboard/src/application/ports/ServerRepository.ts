import type {
  DeployServerInput,
  ListServersFilters,
  PowerStateResult,
  ProvisionerDetail,
  ProvisioningActionResult,
  Server,
  ServerAction,
} from '@/domain/server/types'

export interface Paginated<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
}

/**
 * Servers are read-only: the backend produces them by reconciling provisioner
 * inventory, so there is no create or delete here. The actions delegate to the
 * server's own provisioner and return a state snapshot, not a completion.
 */
export interface ServerRepository {
  listServers(filters?: ListServersFilters): Promise<Paginated<Server>>
  getServer(id: string): Promise<Server | null>
  deployServer(id: string, input: DeployServerInput): Promise<ProvisioningActionResult>
  releaseServer(id: string): Promise<ProvisioningActionResult>

  /**
   * The live provisioner detail for one machine, plus the provisioner's capabilities.
   * Read on demand: it is not part of the mirrored projection.
   */
  getProvisionerDetail(id: string): Promise<ProvisionerDetail>

  /**
   * Run a provisioner action beyond deploy and release. Refused by the backend when the
   * provisioner does not support it, so a caller should gate on capabilities first.
   */
  runServerAction(id: string, action: ServerAction): Promise<ProvisioningActionResult>

  /** Read the live BMC power state. Changes nothing. */
  queryPowerState(id: string): Promise<PowerStateResult>
}
