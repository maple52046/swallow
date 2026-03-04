export type PlaneType = 'kubernetes' | 'slurm'
export type PlaneStatus = 'connected' | 'disconnected' | 'degraded' | 'unknown'

export interface Plane {
  id: string
  name: string
  type: PlaneType
  status: PlaneStatus
  endpointRef: string
  authRef: string
  labels: string[]
  version?: string
  registeredAt: string
  lastSyncAt?: string
}

export interface K8sNode {
  name: string
  status: 'ready' | 'not-ready' | 'cordoned'
  role: 'control-plane' | 'worker'
  gpuCount: number
  cpuCount: number
  memoryGB: number
  osImage: string
  kubeletVersion: string
}

export interface K8sAddon {
  name: string
  version: string
  status: 'healthy' | 'degraded' | 'unknown'
  latestVersion?: string
}

export interface K8sCluster extends Plane {
  type: 'kubernetes'
  nodeCount: number
  gpuNodeCount: number
  nodes: K8sNode[]
  addons: K8sAddon[]
}

export interface SlurmPartition {
  name: string
  state: 'up' | 'down' | 'drain'
  nodeCount: number
  idleNodes: number
  allocNodes: number
  totalCPUs: number
  totalGPUs: number
}

export interface SlurmJob {
  id: string
  name: string
  user: string
  partition: string
  status: 'running' | 'pending' | 'completed' | 'failed' | 'canceled'
  cpus: number
  gpus: number
  submittedAt: string
  startedAt?: string
  completedAt?: string
}

export interface SlurmCluster extends Plane {
  type: 'slurm'
  partitions: SlurmPartition[]
  totalNodes: number
  idleNodes: number
  allocNodes: number
  recentJobs: SlurmJob[]
}
