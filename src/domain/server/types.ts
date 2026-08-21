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
  integrationId: string
  observedAt: string
}

/** Cluster membership, owned by the cluster's own API. Never written by gdcm. */
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
  /** Mebibytes, matching what the provisioner's own UI shows. */
  memoryMiB: number
  storageGB: number
  gpus: ServerGPU[]
  /** The provisioner's own grouping labels. Not a gdcm placement hierarchy. */
  providerZone: string
  providerResourcePool: string

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

  /** The provisioner stopped reporting it. Never deleted: absence is usually transient. */
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

/** The state a lifecycle action returned. A snapshot, not a completion report. */
export interface ProvisioningActionResult {
  serverId: string
  state: ProvisioningState
  providerState: string
  powerState: string
  osSystem: string
  distroSeries: string
  ephemeral: boolean
  hweKernel: string
  observedAt: string
}

/** Display label for a server. There is no gdcm-owned name. */
export function serverDisplayName(server: Server): string {
  return server.hostname ?? server.fqdn ?? server.id
}

export function serverPrimaryAddress(server: Server): string | null {
  return server.addresses.length > 0 ? server.addresses[0] : null
}
