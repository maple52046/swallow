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
 * Live Kubernetes management owned by the Kubernetes dashboard module. Calls read or mutate the
 * deployed cluster through api-server and persist no Kubernetes objects in the dashboard.
 */
export interface KubernetesExplorerPort {
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
