/**
 * A server is a projection of a machine in a provisioner's inventory. The backend
 * produces them by reconciliation, which is why there is no create input here.
 *
 * See docs/glossaries/server.md and docs/decisions/002-server-identity.md.
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
 * Provisioning readiness, owned by the provisioner. Branch on `state`;
 * `providerState` is the provisioner's own label and is for display only.
 */
export type ProvisioningState =
  | 'new'
  | 'commissioning'
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

export interface ProvisioningAxis {
  state: ProvisioningState
  providerState: string
  powerState: 'on' | 'off' | 'error' | 'unknown'
  osSystem: string
  distroSeries: string
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
  /** The provisioner's labels for the last inspection and test run. Display only. */
  commissioningStatus: string
  testingStatus: string
  integrationId: string
  observedAt: string
}

/** Cluster membership, owned by the cluster's own API. Never written by swallow. */
export interface MembershipAxis {
  clusterId: string
  /** The cluster's name for this machine. */
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

export interface Server {
  /** The only identifier to reference a server by. */
  id: string
  source: ServerSource

  /** Observed, mutable, and not unique. Two sites may share a hostname. */
  hostname: string | null
  fqdn: string | null
  /** Empty before commissioning and during a reinstall. */
  addresses: string[]
  architecture: string
  cpuCores: number
  /** The provisioner's CPU model string, e.g. "Intel(R) Xeon(R) Platinum 8480+". */
  cpuModel: string
  /** Mebibytes, matching what the provisioner's own UI shows. */
  memoryMiB: number
  storageGB: number
  gpus: ServerGPU[]
  /** Hardware make as the provisioner commissioned it, for grouping the fleet. */
  systemVendor: string
  systemProduct: string
  /** The provisioner's own grouping labels. Not a swallow placement hierarchy. */
  providerZone: string
  providerResourcePool: string
  /** The VM host a virtual machine belongs to, empty for bare metal. */
  providerPod: string
  /** The provisioner's own labels for the machine, e.g. "gpu". */
  tags: string[]

  hardware: ServerHardware

  /**
   * Three independent axes with three different owners. Each is null until its
   * owner has been observed at least once.
   *
   * Never collapse these into one badge: a server that is deployed, in no cluster,
   * and not reporting metrics is either a spare awaiting allocation or a broken
   * host, and no rule can tell which.
   */
  provisioning: ProvisioningAxis | null
  membership: MembershipAxis | null
  health: HealthAxis | null

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
  clusterId?: string
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
}

/**
 * The provisioner actions beyond deploy and release. Each maps to a POST under the
 * server, and each is refused by a provisioner that does not offer it — so a client
 * should present only the ones its capabilities allow.
 */
export type ServerAction =
  | 'power-on'
  | 'power-off'
  | 'commission'
  | 'test'
  | 'abort'
  | 'override-failed-testing'
  | 'lock'
  | 'unlock'
  | 'mark-broken'
  | 'mark-fixed'
  | 'rescue-mode'
  | 'exit-rescue-mode'

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
