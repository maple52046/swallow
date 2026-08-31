/**
 * A cluster swallow knows about, and the shape of a request to deploy one.
 *
 * swallow owns a cluster's registration and policy, and — for a cluster it builds — the
 * intent to deploy it. Membership is read from the cluster's own API and appears on each
 * server's membership axis, so a cluster's members are servers, not a field here. See
 * docs/development/glossaries/terms/cluster.md and the Node Role glossary.
 */

export type ClusterType = 'kubernetes' | 'slurm'
/** Whether Swallow registered the cluster or deployed it through a durable operation. */
export type ClusterOrigin = 'registered' | 'deployed'

/** Lifecycle derived by the API from durable deploy and uninstall operations. */
export type ClusterLifecycleState =
  | 'registered'
  | 'deploying'
  | 'deploy_failed'
  | 'active'
  | 'uninstalling'
  | 'uninstall_failed'
  | 'uninstalled'

/** Deployment topology derived from the original role assignments. */
export type KubernetesTopology = 'standalone' | 'multi-node' | 'high-availability'

/** Non-secret deployment intent projected from durable Operation provenance. */
export interface ClusterDeployment {
  topology: KubernetesTopology
  roleAssignments: RoleAssignment[]
}

/** Which subsystem installs GPU drivers; has no default at creation. */
export type GPUStackOwner = 'provisioning' | 'gpu-operator'

/** Which subsystem installs this cluster's Prometheus exporters. Defaults to `ansible`. */
export type ExporterOwner = 'ansible' | 'k8s'

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
  origin: ClusterOrigin
  lifecycleState: ClusterLifecycleState
  lifecycleOperationId: string | null
  /** Null for registered Clusters and historical deployments without complete intent. */
  deployment: ClusterDeployment | null
  /** Null while a cluster is registered or declared but not yet reachable. */
  integrationId: string | null
  gpuStackOwner: GPUStackOwner
  /** Which subsystem installs this cluster's exporters; `ansible` unless set to `k8s`. */
  exporterOwner: ExporterOwner
  sync: ClusterSyncState
  createdAt: string
  updatedAt: string
}

/** The part a server plays in a cluster. The k0s term "controller" never appears here. */
export type NodeRole = 'control-plane' | 'worker'

/** Desired role and optional workload co-location for one deployment target. */
export interface RoleAssignment {
  serverId: string
  role: NodeRole
  /** A control-plane Server also registers as a schedulable Kubernetes node. */
  runWorkloads?: boolean
}

/**
 * A request to deploy a k0s cluster onto already-deployed servers. Optional CIDRs fall
 * back to backend defaults. `apiVip` is required only when role
 * assignments infer a highly available control plane; one-control-plane deployments use
 * that Server's observed address.
 */
export interface DeployClusterInput {
  siteId: string
  name: string
  gpuStackOwner: GPUStackOwner
  k0sVersion: string
  podCidr?: string
  serviceCidr?: string
  apiVip?: string
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
