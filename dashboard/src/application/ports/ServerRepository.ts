import type {
  DeployServerInput,
  ListServersFilters,
  PowerStateResult,
  ProvisionerDetail,
  ReleaseServerInput,
  ProviderEvents,
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
 * The backend creates Servers only by reconciling provisioner inventory. Explicit
 * deletion is provider-backed: an implementation must not report success until both the
 * provisioner Machine and the Server projection are gone. Other actions return a state
 * snapshot rather than a completion.
 */
export interface ServerRepository {
  listServers(filters?: ListServersFilters): Promise<Paginated<Server>>
  getServer(id: string): Promise<Server | null>
  deployServer(id: string, input: DeployServerInput): Promise<ProvisioningActionResult>
  releaseServer(id: string, input?: ReleaseServerInput): Promise<ProvisioningActionResult>
  /** Read one machine live and advance only its mirrored provisioning state. */
  refreshServer(id: string): Promise<ProvisioningActionResult>
  /** Permanently remove the backing provisioner Machine and then its Server projection. */
  deleteServer(id: string): Promise<void>

  /**
   * The live provisioner detail for one machine, plus the provisioner's capabilities.
   * Read on demand: it is not part of the mirrored projection.
   */
  getProvisionerDetail(id: string): Promise<ProvisionerDetail>

  /** Read recent provider-owned machine history; this is not a Swallow audit log. */
  getProviderEvents(id: string, limit?: number): Promise<ProviderEvents>

  /**
   * Run a provisioner action beyond deploy and release. Refused by the backend when the
   * provisioner does not support it, so a caller should gate on capabilities first.
   */
  runServerAction(id: string, action: ServerAction): Promise<ProvisioningActionResult>

  /** Read the live BMC power state. Changes nothing. */
  queryPowerState(id: string): Promise<PowerStateResult>
}
