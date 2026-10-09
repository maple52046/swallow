import type { NetworkLinkInput, NetworkTarget, ProvisioningTask } from '@/domain/provisioning/types'
import type {
  BootMediaProbe,
  DeployServerInput,
  HypervisorVirtualMachines,
  ListServersFilters,
  PowerStateResult,
  ProvisionerDetail,
  ReleaseServerInput,
  ProviderEvents,
  ProvisioningActionResult,
  Server,
  ServerAction,
  ServerBootMedia,
  ServerPowerConfiguration,
  SetServerBootMediaResult,
  SetServerDefaultUserInput,
  SetServerDefaultUserResult,
  SetServerPowerConfigurationInput,
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

  /** Read the live power state through the Server's power driver. Changes nothing. */
  queryPowerState(id: string): Promise<PowerStateResult>

  /**
   * Reads the Server's Power Configuration live from its provisioner (contract
   * server-detail-actions.md "Power Configuration", decision 054). Never carries the password.
   * Rejects with the API error: `validation_error` (the provisioner lacks the capability),
   * `not_found`, or `provider_unavailable` (provisioner unreachable, or its account may not read
   * power parameters).
   */
  getPowerConfiguration(id: string): Promise<ServerPowerConfiguration>
  /**
   * Replaces the Server's Power Configuration at the provisioner and resolves with it read back.
   * It neither switches power nor resumes an inspection waiting for attention. Rejects with the API
   * error: `validation_error` (unsupported driver, a parameter missing or not applicable, or the
   * provisioner's own refusal — the message explains), `conflict` (locked, or powered through a VM
   * host), `not_found`, or `provider_unavailable`. The password must not be kept after the call.
   */
  setPowerConfiguration(id: string, input: SetServerPowerConfigurationInput): Promise<ServerPowerConfiguration>

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
   * Enables Boot Media with the Boot ISO `isoId` (the preflight: the API probes the BMC, mounts the
   * ISO, directs the boot, and saves only when that worked — it takes about five minutes, and
   * `getBootMedia` reports its progress as `apply` meanwhile), or disables it (`isoId` omitted; the
   * chosen ISO is kept for next time). Enabling an enabled Server re-applies it, or switches to
   * another ISO after ejecting the previous one. Rejects with the API error: `validation_error`
   * (no `isoId`, or an ISO of another provisioner), `not_found` (unknown ISO), `conflict` (a
   * preflight already running, locked, ISO not served, no BMC, unsupported, or the BMC refused —
   * the message carries the BMC's explanation) or `provider_unavailable` (BMC or provisioner
   * unreachable).
   */
  setBootMedia(id: string, enabled: boolean, isoId?: string): Promise<SetServerBootMediaResult>
  /**
   * Re-probes the Server's Boot Media method now — its BMC's Redfish capability, or a virtual
   * machine's Hypervisor — and resolves with both stored capabilities. An unreachable BMC or
   * Hypervisor resolves with that support value; only an unknown Server or an unreachable
   * provisioner rejects.
   */
  probeBootMedia(id: string): Promise<BootMediaProbe>

  /**
   * Lists the libvirt domains of a Hypervisor — a deployed swallow Server the API logs in to with
   * the Deployment Key, as `account` or its Server Default User (decision 055). Rejects with the API
   * error: `validation_error` (an account that is not a login name), `not_found`, `conflict` (not
   * deployed, no Deployment Key, the key or libvirt refused for the account — the message says
   * which), or `provider_unavailable` (unreachable over SSH).
   */
  listVirtualMachines(hypervisorId: string, account?: string): Promise<HypervisorVirtualMachines>
}
