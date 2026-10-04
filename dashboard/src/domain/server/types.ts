/**
 * A server is a projection of a machine in a provisioner's inventory. The backend
 * produces them by reconciliation, which is why there is no create input here.
 *
 * See docs/development/glossaries/terms/server.md and docs/decisions/002-server-identity.md.
 */

/** Where a server came from. The external key the backend reconciles on. */
export interface ServerSource {
  siteId: string
  integrationId: string
  /** The provisioner's own identifier. Opaque: never parse it. */
  providerMachineId: string
}

/** Hardware identity, used by the backend to recognise a re-enrolled machine. */
export interface ServerHardware {
  systemUuid: string | null
  serialNumber: string | null
  macAddresses: string[]
}

export interface ServerGPU {
  vendor: string
  model: string
  count: number
}

/**
 * The swallow-defined OS Provisioning State (glossary term, decision 048): provider-neutral
 * values that each provisioner adapter maps its own lifecycle onto. Branch on this value;
 * `providerState` is the provisioner's own word (for example MAAS "Commissioning" while the
 * state is `inspecting`) and is for display only. The UI label is the value in title case
 * ("Ready", "Releasing").
 */
export type ProvisioningState =
  | 'new'
  | 'inspecting'
  | 'ready'
  | 'allocated'
  | 'deploying'
  | 'deployed'
  | 'releasing'
  | 'testing'
  | 'rescue'
  | 'broken'
  | 'failed'
  | 'retired'
  | 'unknown'

/**
 * The OS Provisioning States in which provider work is running, so the state changes without
 * operator action. One set shared by the Deployment cell's progress cue, the list's
 * changing/polling logic, the detail page's follow poll, and the action gates that refuse to
 * race in-flight work, so the four never disagree.
 */
export const IN_PROGRESS_PROVISIONING_STATES: ReadonlySet<ProvisioningState> = new Set<ProvisioningState>([
  'inspecting',
  'deploying',
  'releasing',
  'testing',
])

/** True when `state` is one of {@link IN_PROGRESS_PROVISIONING_STATES}; an unobserved axis is not. */
export function isProvisioningInProgress(state: ProvisioningState | undefined): boolean {
  return state !== undefined && IN_PROGRESS_PROVISIONING_STATES.has(state)
}

export type DeploymentState =
  | 'deploying'
  | 'verifying'
  | 'succeeded'
  | 'failed'
  | 'requires_attention'
  | 'canceled'

/** Swallow-owned result of the latest durable operating system deployment. */
export interface DeploymentAxis {
  state: DeploymentState
  operationId: string
  stepId: string
  attempt: number
  /**
   * Stable, machine-branchable error code of the failed Step (e.g.
   * `deployment_address_unavailable`). Lets the UI render a concise root cause without
   * parsing `statusReason`. Empty for a non-failed deployment.
   */
  code: string
  stage: string
  statusReason: string
  startedAt: string
  finishedAt: string | null
  updatedAt: string
}

export interface ProvisioningAxis {
  state: ProvisioningState
  providerState: string
  /**
   * The provisioner's own machine-level failure reason (e.g. "Failed to erase disks."), present
   * only for failure states (failed/broken/rescue) and absent otherwise. Display and diagnostics
   * only: it surfaces why a lifecycle action failed without reading the provider event log.
   */
  errorDescription?: string
  powerState: 'on' | 'off' | 'error' | 'unknown'
  osSystem: string
  distroSeries: string
  /**
   * Effective display name of the currently deployed OS image (provider catalog name overlaid
   * with any swallow custom name). Mirrored when a deploy completes and refreshed by reconcile
   * (and by an image rename), so a freshly deployed Server shows the friendly name promptly.
   * Empty when nothing is deployed or the image could not be resolved from the catalog, in which
   * case the UI falls back to `osSystem`/`distroSeries`.
   */
  deployedImageName: string
  /**
   * Effective default login user of the deployed OS image (decision 039), mirrored like
   * `deployedImageName`. Swallow automation logs in to the Server as this user, and it is the
   * account an operator's Access Key authorizes on Servers the provisioner deployed. Absent when
   * nothing is deployed or no default user is known (automation then falls back to the Site SSH
   * user).
   */
  deployedImageDefaultUser?: string
  /**
   * The OS runs from memory and the disks are untouched, so everything on the root
   * filesystem is lost on reboot.
   *
   * Must be surfaced wherever the state is: no other field distinguishes this from the
   * same OS installed on disk, and anything written to such a machine silently
   * un-happens.
   */
  ephemeral: boolean
  /** The provisioner's own kernel label, e.g. "ga-24.04". Display only. */
  hweKernel: string
  /** The provisioner is refusing state-changing actions; explains a rejected deploy. */
  locked: boolean
  /**
   * The provisioner's labels for the last inspection and test run. Display only. The field
   * keeps its published name because it is the provider's result label, not a lifecycle state.
   */
  commissioningStatus: string
  testingStatus: string
  integrationId: string
  observedAt: string
}

/** Platform membership, owned by the platform's own API. Never written by swallow. */
export interface MembershipAxis {
  platformId: string
  /** The platform's name for this machine. */
  nodeName: string
  role: string
  state: string
  observedAt: string
}

/** Liveness, owned by the metrics store. Resolved at query time, never stored. */
export interface HealthAxis {
  state: 'up' | 'down'
  observedAt: string
}

/**
 * Where a Server's effective Server Default User comes from (glossary Server Default User,
 * decision 045): `server` was set by an operator on this Server and verified by a Deployment Key
 * login; `os_image` is the deployed OS Image's default user.
 */
export type ServerDefaultUserSource = 'server' | 'os_image'

/**
 * The effective Server Default User the API resolves — the account swallow automation logs in as
 * with the Deployment Key and that Docker CE adds to the `docker` group. Read this rather than
 * `provisioning.deployedImageDefaultUser`, which stays the image's own value.
 */
export interface ServerDefaultUser {
  user: string
  source: ServerDefaultUserSource
}

/**
 * How a Server Default User may use sudo, as the verifying Deployment Key login observed it:
 * `passwordless`, `password_required` (automation uses the Site become password), or `unavailable`
 * (automation that needs root will fail).
 */
export type ServerDefaultUserSudo = 'passwordless' | 'password_required' | 'unavailable'

/**
 * Intent to set a Server Default User. `password` is the account's password, sent once so the API
 * can install the Deployment Key; it is never stored or returned, so callers must drop it after the
 * request.
 */
export interface SetServerDefaultUserInput {
  user: string
  password?: string
}

/** Outcome of setting a Server Default User (`PUT /servers/{id}/default-user`). */
export interface SetServerDefaultUserResult {
  defaultUser: ServerDefaultUser
  /** True when a password was given and the Deployment Key was installed (or already present). */
  keyInstalled: boolean
  sudo: ServerDefaultUserSudo
}

/**
 * What swallow's Redfish probe found about a Server's BMC (glossary BMC, decision 047):
 * `supported` — Redfish answers and the host has a virtual CD and boot override, so Boot Media can
 * be enabled; `unsupported` — Redfish answers but lacks one of them; `unreachable` — no Redfish
 * service at the BMC address, or it rejected the provisioner's BMC account; `no_bmc` — a virtual
 * machine, or the provisioner holds no BMC address. An unknown future value must be treated as not
 * supported.
 */
export type RedfishSupport = 'supported' | 'unsupported' | 'unreachable' | 'no_bmc'

/**
 * The latest Redfish capability probe of a Server's BMC — swallow-owned data with a probe time,
 * refreshed by the API's sweep and on demand. It never carries a credential.
 */
export interface RedfishCapability {
  support: RedfishSupport
  /** Why `support` is not `supported`; absent otherwise. */
  reason?: string
  serviceRoot?: string
  vendor?: string
  product?: string
  redfishVersion?: string
  firmwareVersion?: string
  /** The Redfish System identified as the host (a GPU baseboard is never chosen). */
  systemId?: string
  virtualMedia: boolean
  /** Boot override modes the BMC allows besides Disabled: `Once`, `Continuous`. */
  bootOverrideModes: string[]
  probedAt: string
}

/** What last applied Boot Media to the BMC: the operator's enable (`preflight`) or an OS deployment's `ensure` Task. */
export type BootMediaApplier = 'preflight' | 'ensure'

/**
 * A Server's Boot Media setting (glossary Boot Media): the operator's intent that the Server boot
 * the installation's iPXE ISO first, plus the outcome of the last apply. It is intent and history,
 * not the BMC's live state. `bootOverride` is how persistently the BMC took it: `Continuous`
 * (survives reboots) or `Once` (the next boot only — every OS deployment re-applies it anyway).
 */
export interface BootMediaSetting {
  enabled: boolean
  updatedAt: string
  lastAppliedAt: string | null
  lastAppliedBy?: BootMediaApplier
  bootOverride?: string
  /** The most recent failed apply, cleared by a successful one; it never changes `enabled`. */
  lastError?: string
  lastErrorAt: string | null
}

/** The BMC's Boot Media state as read live for one request. `ready` means the next boot starts from the ISO. */
export interface BootMediaLiveState {
  mediaInserted: boolean
  /** Verbatim from the BMC, which may rewrite the URL. */
  mediaImage?: string
  overrideEnabled?: string
  overrideTarget?: string
  ready: boolean
}

/**
 * One Server's Boot Media (`GET /servers/{id}/boot-media`): the installation's ISO (one fixed URL,
 * never per Server), the Server's setting (`null` when never set), its Redfish capability (`null`
 * before the first probe), and — only when asked for — the BMC's live state (`null` when not read
 * or the read failed, `liveError` saying why).
 */
export interface ServerBootMedia {
  serverId: string
  image: { url: string; available: boolean; reason?: string }
  setting: BootMediaSetting | null
  redfish: RedfishCapability | null
  live: BootMediaLiveState | null
  liveError?: string
}

/**
 * Outcome of `PUT /servers/{id}/boot-media`. For a disable, `reverted` says whether the BMC was
 * reset (ISO ejected, override cleared) and `revertError` why not; the setting is saved either way.
 */
export interface SetServerBootMediaResult extends ServerBootMedia {
  reverted?: boolean
  revertError?: string
}

export interface Server {
  /** The only identifier to reference a server by. */
  id: string
  source: ServerSource

  /** Observed, mutable, and not unique. Two sites may share a hostname. */
  hostname: string | null
  fqdn: string | null
  /** Empty before the provisioner first inspects the hardware and during a reinstall. */
  addresses: string[]
  architecture: string
  cpuCores: number
  /** The provisioner's CPU model string, e.g. "Intel(R) Xeon(R) Platinum 8480+". */
  cpuModel: string
  /** Mebibytes, matching what the provisioner's own UI shows. */
  memoryMiB: number
  storageGB: number
  gpus: ServerGPU[]
  /** Hardware make as the provisioner's inspection reported it, for grouping the fleet. */
  systemVendor: string
  systemProduct: string
  /**
   * The provisioner's observed grouping labels for this machine — the effect of a Zone/Pool
   * placement, not the swallow-owned catalog. Swallow's managed Zones and Pools live under
   * Infrastructure and are assigned through Set zone/pool. See docs/decisions/029.
   */
  providerZone: string
  providerResourcePool: string
  /** The VM host a virtual machine belongs to, empty for bare metal. */
  providerPod: string
  /** The provisioner's own labels for the machine, e.g. "gpu". */
  tags: string[]

  hardware: ServerHardware

  /**
   * Independent projections with different owners. Deployment is Swallow's durable
   * workflow result; provisioning, membership, and health remain external observations.
   *
   * They remain separate because an OS can be installed while deployment verification
   * has failed, or a verified Server can later stop reporting health.
   */
  deployment: DeploymentAxis | null
  provisioning: ProvisioningAxis | null
  membership: MembershipAxis | null
  health: HealthAxis | null

  /** Effective Server Default User; absent when unknown (automation then probes fallback users). */
  defaultUser?: ServerDefaultUser

  /**
   * The provisioner stopped reporting it, so ordinary reconciliation retains the
   * projection. This differs from an explicit provider-backed deletion.
   */
  absent: boolean
  lastSeenAt: string | null

  createdAt: string
  updatedAt: string
}

export interface ListServersFilters {
  siteId?: string
  integrationId?: string
  provisioningState?: string
  platformId?: string
  keyword?: string
  includeAbsent?: boolean
  page?: number
  pageSize?: number
}

export interface DeployServerInput {
  /** An OS image id, e.g. "ubuntu/jammy". Required: the provisioner must not choose. */
  distroSeries: string
  osSystem?: string
  userData?: string
  comment?: string
  /**
   * Run the OS from memory and leave the disks untouched.
   *
   * Refused rather than ignored by a provisioner that cannot do it, so the caller can
   * trust that a successful response means what it says.
   */
  ephemeral?: boolean
}

/** Controls how a provisioner releases a Server and whether it erases disks first. */
export interface ReleaseServerInput {
  erase: boolean
  secureErase: boolean
  quickErase: boolean
  comment?: string
  unbindStaticIPs: boolean
}

/** The state a lifecycle action returned. A snapshot, not a completion report. */
export interface ProvisioningActionResult {
  serverId: string
  state: ProvisioningState
  providerState: string
  powerState: ProvisioningAxis['powerState']
  osSystem: string
  distroSeries: string
  ephemeral: boolean
  hweKernel: string
  locked: boolean
  commissioningStatus: string
  testingStatus: string
  observedAt: string
  taskId?: string
}

/**
 * The provisioner actions beyond deploy and release. Each maps to a POST under the
 * server, and each is refused by a provisioner that does not offer it — so a client
 * should present only the ones its capabilities allow.
 */
export type ServerAction =
  | 'power-on'
  | 'power-off'
  | 'inspect'
  | 'test'
  | 'abort'
  | 'override-failed-testing'
  | 'lock'
  | 'unlock'
  | 'mark-broken'
  | 'mark-fixed'
  | 'rescue-mode'
  | 'exit-rescue-mode'
  | 'recover'

/**
 * What a provisioner offers. A client shows exactly the actions that exist rather than
 * buttons that always fail, and the backend refuses any action whose flag is false.
 */
export interface ProvisionerCapabilities {
  ephemeralDeploy: boolean
  power: boolean
  hardwareValidation: boolean
  operatorState: boolean
  machineDetail: boolean
  hardwareInventory: boolean
  machineRemoval: boolean
  releaseOptions: boolean
  networkConfiguration: boolean
}

/** A labelled value in a provisioner detail section. */
export interface DetailField {
  label: string
  value: string
}

/** A titled group of provisioner detail fields. */
export interface DetailSection {
  title: string
  fields: DetailField[]
}

/** Titled tabular provisioner detail, e.g. disks or NICs. Columns are provider-defined. */
export interface DetailTable {
  title: string
  columns: string[]
  rows: string[][]
}

/**
 * The live single-machine view proxied from the provisioner, plus the capabilities that
 * decide which actions to show. Read on demand rather than mirrored: it is only ever
 * looked at one machine at a time.
 */
export interface ProvisionerDetail {
  capabilities: ProvisionerCapabilities
  sections: DetailSection[]
  tables: DetailTable[]
}

/** The live power state returned by a query. */
export interface PowerStateResult {
  serverId: string
  powerState: string
}

/** One machine event retained by the provisioner and proxied live by Swallow. */
export interface ProviderEvent {
  id: string
  level: string
  type: string
  message: string
  actor: string | null
  occurredAt: string
}

/** Provider-event capability and the newest retained events for one Server. */
export interface ProviderEvents {
  supported: boolean
  events: ProviderEvent[]
}

/** Display label for a server. There is no swallow-owned name. */
export function serverDisplayName(server: Server): string {
  return server.hostname ?? server.fqdn ?? server.id
}

export function serverPrimaryAddress(server: Server): string | null {
  return server.addresses.length > 0 ? server.addresses[0] : null
}
