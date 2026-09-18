import type {
  Platform,
  DeployPlatformInput,
  DeployPlatformResult,
  MembershipReport,
  SlurmCluster,
  UninstallPlatformOptions,
  MinimumResources,
  SlurmDeploymentRequirement,
} from '@/domain/platform/types'
import type {
  KubernetesApplication,
  KubernetesApplicationKind,
  KubernetesApplyResult,
  KubernetesClusterSummary,
  KubernetesConfigResource,
  KubernetesIngress,
  KubernetesNamespace,
  KubernetesNode,
  KubernetesPersistentVolumeClaim,
  KubernetesPod,
  KubernetesPodLogs,
  KubernetesService,
} from '@/domain/platform/kubernetes'

/**
 * Reads platforms and deploys new ones. A platform's members are servers carrying the
 * membership axis, read through the ServerRepository, so there is no member call here.
 * Registration of an existing platform involves a credential and is done through the API by
 * an operator, so it is not exposed to the dashboard.
 */
export interface PlatformRepository {
  /** All platforms, optionally scoped to one site. Small and bounded, so not paginated. */
  listPlatforms(siteId?: string): Promise<Platform[]>
  getPlatform(id: string): Promise<Platform | null>
  /**
   * Deploy a new platform (Kubernetes/k0s or Slurm, per `input.type`); resolves once the
   * platform and its operation are accepted.
   */
  deployPlatform(input: DeployPlatformInput): Promise<DeployPlatformResult>
  /**
   * Remove platform software from the original deployment targets and retain the record.
   * When `options.releaseServers` is set, release each target directly instead; release wipes
   * the operating system, so the software-uninstall steps are skipped.
   */
  uninstallPlatform(id: string, options?: UninstallPlatformOptions): Promise<DeployPlatformResult>
  /** Delete only the Swallow record and owned projections; hosts are untouched. */
  deletePlatform(id: string): Promise<void>
  /** Read the platform's membership now instead of waiting for the background interval. */
  syncPlatform(id: string): Promise<MembershipReport>
  /**
   * Read a Slurm platform's live cluster state (controllers, partitions, compute node
   * scheduler state) on demand. Resolves to null when there is no live view to show - a
   * non-Slurm platform, a Slurm platform whose slurmrestd integration is not recorded yet,
   * or slurmrestd being unreachable - so the Slurm view degrades to deployment intent plus
   * membership instead of surfacing an error.
   */
  getSlurmCluster(id: string): Promise<SlurmCluster | null>
  /** Reads the system-wide Slurm node eligibility floor; a null minimum is disabled. */
  getSlurmDeploymentRequirement(): Promise<SlurmDeploymentRequirement>
  /** Replaces the Slurm eligibility floor, or disables it when minimum is null. */
  putSlurmDeploymentRequirement(minimum: MinimumResources | null): Promise<SlurmDeploymentRequirement>

  /**
   * The live Kubernetes cluster explorer for a Swallow-deployed Kubernetes Platform. Every call
   * reads or writes the deployed cluster's own API on demand and persists nothing. A Platform
   * that is not an eligible deployed Kubernetes cluster (a legacy registered record, a
   * non-Kubernetes type, or one without a recorded credential) rejects these calls, which the
   * explorer surfaces as "unavailable" and degrades from.
   */
  kubernetes: KubernetesExplorer
}

/**
 * The read/write operations of the Kubernetes cluster explorer, grouped so the UI depends on
 * one cohesive surface. Reads throw on transport failure; eligibility failures are typed API
 * errors the caller maps to a degraded view.
 */
export interface KubernetesExplorer {
  summary(platformId: string): Promise<KubernetesClusterSummary>
  listNodes(platformId: string): Promise<KubernetesNode[]>
  setNodeSchedulable(platformId: string, nodeName: string, schedulable: boolean): Promise<KubernetesNode>

  listNamespaces(platformId: string): Promise<KubernetesNamespace[]>
  createNamespace(platformId: string, name: string): Promise<KubernetesNamespace>
  deleteNamespace(platformId: string, name: string): Promise<void>

  listApplications(platformId: string, namespace?: string): Promise<KubernetesApplication[]>
  getApplication(platformId: string, namespace: string, kind: KubernetesApplicationKind, name: string): Promise<KubernetesApplication>
  deleteApplication(platformId: string, namespace: string, kind: KubernetesApplicationKind, name: string): Promise<void>
  scaleApplication(platformId: string, namespace: string, kind: KubernetesApplicationKind, name: string, replicas: number): Promise<KubernetesApplication>
  restartApplication(platformId: string, namespace: string, kind: KubernetesApplicationKind, name: string): Promise<void>

  listPods(platformId: string, namespace?: string): Promise<KubernetesPod[]>
  podLogs(platformId: string, namespace: string, name: string, container?: string, tailLines?: number): Promise<KubernetesPodLogs>
  deletePod(platformId: string, namespace: string, name: string): Promise<void>

  listServices(platformId: string, namespace?: string): Promise<KubernetesService[]>
  listIngresses(platformId: string, namespace?: string): Promise<KubernetesIngress[]>
  listConfigMaps(platformId: string, namespace?: string): Promise<KubernetesConfigResource[]>
  listSecrets(platformId: string, namespace?: string): Promise<KubernetesConfigResource[]>
  listPersistentVolumeClaims(platformId: string, namespace?: string): Promise<KubernetesPersistentVolumeClaim[]>

  apply(platformId: string, manifest: string, dryRun: boolean): Promise<KubernetesApplyResult[]>
}
