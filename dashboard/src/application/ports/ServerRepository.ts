import type { NetworkLinkInput, NetworkTarget, ProvisioningTask } from '@/domain/provisioning/types'
import type {
  DeployServerInput,
  ListServersFilters,
  PowerStateResult,
  ProvisionerDetail,
  ReleaseServerInput,
  ProviderEvents,
  ProvisioningActionResult,
  RedfishCapability,
  Server,
  ServerAction,
  ServerBootMedia,
  SetServerBootMediaResult,
  SetServerDefaultUserInput,
  SetServerDefaultUserResult,
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
  /** Read one machine live and advance its provisioning state and observed addresses. */
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

  /** Read and mutate one Server's structured live network configuration. */
  getNetwork(id: string): Promise<NetworkTarget>
  createNetworkLink(id: string, interfaceId: string, input: NetworkLinkInput): Promise<NetworkTarget>
  replaceNetworkLink(id: string, interfaceId: string, linkId: string, input: NetworkLinkInput): Promise<NetworkTarget>
  deleteNetworkLink(id: string, interfaceId: string, linkId: string): Promise<NetworkTarget>

  /** Durable Swallow-owned provisioning follow-up shown in Activity. */
  listProvisioningTasks(id: string): Promise<ProvisioningTask[]>
  getProvisioningTask(taskId: string): Promise<ProvisioningTask>
  retryProvisioningTask(taskId: string): Promise<ProvisioningTask>

  /**
   * Run a provisioner action beyond deploy and release. Refused by the backend when the
   * provisioner does not support it, so a caller should gate on capabilities first.
   */
  runServerAction(id: string, action: ServerAction): Promise<ProvisioningActionResult>

  /** Read the live BMC power state. Changes nothing. */
  queryPowerState(id: string): Promise<PowerStateResult>

  /**
   * Sets the Server Default User (contract server-detail-actions.md "Default User"). The API logs
   * in to the host — once with `input.password` to install the Deployment Key when given, then with
   * the key to verify — and saves only after that works, so the call takes a few seconds. Rejects
   * with the API error: `validation_error` (bad name or rejected password), `conflict` (not
   * deployed, locked, no Deployment Key, or the key is not authorized), `provider_unavailable`
   * (host unreachable). The password must not be kept after the call.
   */
  setDefaultUser(id: string, input: SetServerDefaultUserInput): Promise<SetServerDefaultUserResult>
  /** Clears the value set on the Server so the OS Image's default user applies; never touches the host. */
  clearDefaultUser(id: string): Promise<void>

  /**
   * Reads the Server's Boot Media (contract server-detail-actions.md "Boot Media"). With `live` the
   * API also reads the BMC, which takes seconds; a failed live read still resolves, with
   * `live: null` and `liveError`.
   */
  getBootMedia(id: string, options?: { live?: boolean }): Promise<ServerBootMedia>
  /**
   * Enables (the preflight: the API probes the BMC, mounts the ISO, directs the boot, and saves only
   * when that worked — it can take minutes) or disables Boot Media. Rejects with the API error:
   * `conflict` (locked, no ISO, no BMC, unsupported, or the BMC refused — the message carries the
   * BMC's explanation) or `provider_unavailable` (BMC or provisioner unreachable).
   */
  setBootMedia(id: string, enabled: boolean): Promise<SetServerBootMediaResult>
  /** Re-probes the BMC's Redfish capability now; an unreachable BMC resolves with that support value. */
  probeRedfish(id: string): Promise<RedfishCapability>
}
