import type { Plane, K8sCluster, SlurmCluster } from '@/domain/plane/types'
import type { PlaneRepository, RegisterPlaneInput } from '@/application/ports/PlaneRepository'
import { seedK8sClusters, seedSlurmClusters } from '@/infrastructure/mock/data/seedPlanes'
import { lsGet, lsSet } from '@/infrastructure/persistence/localStorage'

function genId() {
  return 'plane-' + Date.now().toString(36) + Math.random().toString(36).slice(2, 5)
}

export class MockPlaneRepository implements PlaneRepository {
  private k8s: Map<string, K8sCluster>
  private slurm: Map<string, SlurmCluster>

  constructor() {
    const savedK8s = lsGet<K8sCluster[]>('planes-k8s', seedK8sClusters)
    const savedSlurm = lsGet<SlurmCluster[]>('planes-slurm', seedSlurmClusters)
    this.k8s = new Map(savedK8s.map((c) => [c.id, c]))
    this.slurm = new Map(savedSlurm.map((c) => [c.id, c]))
  }

  private save() {
    lsSet('planes-k8s', Array.from(this.k8s.values()))
    lsSet('planes-slurm', Array.from(this.slurm.values()))
  }

  async list(): Promise<Plane[]> {
    return [...Array.from(this.k8s.values()), ...Array.from(this.slurm.values())].sort((a, b) =>
      new Date(b.registeredAt).getTime() - new Date(a.registeredAt).getTime(),
    )
  }

  async get(id: string): Promise<Plane | null> {
    return (this.k8s.get(id) ?? this.slurm.get(id)) ?? null
  }

  async getK8sCluster(id: string): Promise<K8sCluster | null> {
    return this.k8s.get(id) ?? null
  }

  async getSlurmCluster(id: string): Promise<SlurmCluster | null> {
    return this.slurm.get(id) ?? null
  }

  async register(input: RegisterPlaneInput): Promise<Plane> {
    const now = new Date().toISOString()
    const base = {
      id: genId(),
      name: input.name,
      type: input.type,
      status: 'connected' as const,
      endpointRef: input.endpointRef,
      authRef: input.authRef,
      labels: input.labels,
      registeredAt: now,
      lastSyncAt: now,
    }
    if (input.type === 'kubernetes') {
      const cluster: K8sCluster = { ...base, type: 'kubernetes', nodeCount: 0, gpuNodeCount: 0, nodes: [], addons: [] }
      this.k8s.set(cluster.id, cluster)
      this.save()
      return cluster
    } else {
      const cluster: SlurmCluster = { ...base, type: 'slurm', totalNodes: 0, idleNodes: 0, allocNodes: 0, partitions: [], recentJobs: [] }
      this.slurm.set(cluster.id, cluster)
      this.save()
      return cluster
    }
  }

  async update(id: string, partial: Partial<Plane>): Promise<Plane> {
    if (this.k8s.has(id)) {
      const updated = { ...this.k8s.get(id)!, ...partial }
      this.k8s.set(id, updated as K8sCluster)
      this.save()
      return updated
    }
    if (this.slurm.has(id)) {
      const updated = { ...this.slurm.get(id)!, ...partial }
      this.slurm.set(id, updated as SlurmCluster)
      this.save()
      return updated
    }
    throw new Error('Plane not found: ' + id)
  }
}
