import type {
  Cluster,
  DeployClusterInput,
  DeployClusterResult,
  MembershipReport,
} from '@/domain/cluster/types'

/**
 * Reads clusters and deploys new ones. A cluster's members are servers carrying the
 * membership axis, read through the ServerRepository, so there is no member call here.
 * Registration of an existing cluster involves a credential and is done through the API by
 * an operator, so it is not exposed to the dashboard.
 */
export interface ClusterRepository {
  /** All clusters, optionally scoped to one site. Small and bounded, so not paginated. */
  listClusters(siteId?: string): Promise<Cluster[]>
  getCluster(id: string): Promise<Cluster | null>
  /** Deploy a new k0s cluster; resolves once the cluster and its operation are accepted. */
  deployCluster(input: DeployClusterInput): Promise<DeployClusterResult>
  /** Remove k0s from the original deployment targets and retain the record. */
  uninstallCluster(id: string): Promise<DeployClusterResult>
  /** Delete only the Swallow record and owned projections; hosts are untouched. */
  deleteCluster(id: string): Promise<void>
  /** Read the cluster's membership now instead of waiting for the background interval. */
  syncCluster(id: string): Promise<MembershipReport>
}
