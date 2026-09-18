/**
 * Live Kubernetes cluster explorer view types.
 *
 * These describe what a Swallow-deployed Kubernetes Platform's own API reports at read time.
 * Nothing here is a Swallow-owned record: the explorer reads and writes the cluster live and
 * stores nothing (see docs/decisions/032-self-deployed-platform-management.md). They mirror the
 * `platforms-kubernetes.md` provider contract, not the cluster's raw object shapes.
 */

/** The supported Kubernetes Application kinds (see the Kubernetes Application glossary term). */
export type KubernetesApplicationKind = 'Deployment' | 'DaemonSet' | 'StatefulSet' | 'Pod'

/** The live cluster header: version and observed counts. */
export interface KubernetesClusterSummary {
  version: string
  nodeCount: number
  readyNodeCount: number
  namespaceCount: number
}

/** One cluster node, correlated to a Server projection when one matches. */
export interface KubernetesNode {
  name: string
  role: 'control-plane' | 'worker'
  ready: boolean
  unschedulable: boolean
  /** Swallow's matched Server id, or null when no projection matches the node. */
  serverId: string | null
  addresses: string[]
  kubeletVersion: string
}

/** One namespace; `system` marks a cluster-owned namespace hidden by default. */
export interface KubernetesNamespace {
  name: string
  phase: string
  system: boolean
}

/** One pod, in a pod list or an Application's detail. */
export interface KubernetesPod {
  namespace: string
  name: string
  phase: string
  ready: boolean
  nodeName: string
  restarts: number
  containers: string[]
  startedAt: string
}

/** A live workload aggregation. `pods` is present only in the detail read. */
export interface KubernetesApplication {
  namespace: string
  name: string
  kind: KubernetesApplicationKind
  images: string[]
  replicas: number
  readyReplicas: number
  createdAt: string
  pods?: KubernetesPod[]
}

/** One Service in the read-only list. */
export interface KubernetesService {
  namespace: string
  name: string
  type: string
  clusterIP: string
  ports: string[]
  externalIPs: string[]
}

/** One Ingress in the read-only list. */
export interface KubernetesIngress {
  namespace: string
  name: string
  hosts: string[]
  ingressClass: string
  addresses: string[]
}

/** One ConfigMap or Secret; only data key names are exposed, never Secret values. */
export interface KubernetesConfigResource {
  namespace: string
  name: string
  type: string
  keys: string[]
  dataCount: number
}

/** One PersistentVolumeClaim in the read-only list. */
export interface KubernetesPersistentVolumeClaim {
  namespace: string
  name: string
  phase: string
  capacity: string
  storageClass: string
  accessModes: string[]
}

/** One object's outcome from a manifest apply. */
export interface KubernetesApplyResult {
  kind: string
  namespace: string
  name: string
  /** `created`, `configured`, `unchanged`, or `validated` (dry run). */
  action: string
}

/** A parsed pod log snapshot. */
export interface KubernetesPodLogs {
  container: string
  logs: string
}
