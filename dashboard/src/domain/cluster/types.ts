/**
 * A cluster swallow knows about, and the shape of a request to deploy one.
 *
 * swallow owns a cluster's registration and policy, and — for a cluster it builds — the
 * intent to deploy it. Membership is read from the cluster's own API and appears on each
 * server's membership axis, so a cluster's members are servers, not a field here. See
 * docs/glossaries/cluster.md and docs/development/glossaries/terms/node-role.md.
 */

export type ClusterType = 'kubernetes' | 'slurm'

/** Which subsystem installs GPU drivers; has no default at creation. */
export type GPUStackOwner = 'provisioning' | 'gpu-operator'

/**
 * Freshness of the last membership read. `matchedCount` below `memberCount` means the
 * cluster contains machines swallow does not manage, which is worth showing rather than
 * hiding.
 */
export interface ClusterSyncState {
  lastStartedAt: string | null
  lastSucceededAt: string | null
  lastError: string | null
  memberCount: number
  matchedCount: number
}

export interface Cluster {
  id: string
  siteId: string
  name: string
  type: ClusterType
  /** Null while a cluster is registered or declared but not yet reachable. */
  integrationId: string | null
  gpuStackOwner: GPUStackOwner
  sync: ClusterSyncState
  createdAt: string
  updatedAt: string
}

/** The part a server plays in a cluster. The k0s term "controller" never appears here. */
export type NodeRole = 'control-plane' | 'worker'

export interface RoleAssignment {
  serverId: string
  role: NodeRole
}

/**
 * A request to deploy a k0s cluster onto already-deployed servers. Optional network fields
 * fall back to the backend defaults; `apiVip` and at least three control-plane assignments
 * are required for a highly available control plane.
 */
export interface DeployClusterInput {
  siteId: string
  name: string
  gpuStackOwner: GPUStackOwner
  k0sVersion: string
  podCidr?: string
  serviceCidr?: string
  apiVip: string
  apiVipPrefix?: number
  roleAssignments: RoleAssignment[]
}

/** The accepted deployment: the created cluster and the operation building it. */
export interface DeployClusterResult {
  clusterId: string
  operationId: string
}

/** The report a membership sync returns; `unmatched` names members with no server. */
export interface MembershipReport {
  clusterId: string
  clusterName: string
  members: number
  matched: number
  cleared: number
  unmatched: string[]
  error: string | null
}
