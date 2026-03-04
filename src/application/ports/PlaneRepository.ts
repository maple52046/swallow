import type { Plane, K8sCluster, SlurmCluster } from '@/domain/plane/types'

export interface RegisterPlaneInput {
  name: string
  type: 'kubernetes' | 'slurm'
  endpointRef: string
  authRef: string
  labels: string[]
}

export interface PlaneRepository {
  list(): Promise<Plane[]>
  get(id: string): Promise<Plane | null>
  getK8sCluster(id: string): Promise<K8sCluster | null>
  getSlurmCluster(id: string): Promise<SlurmCluster | null>
  register(input: RegisterPlaneInput): Promise<Plane>
  update(id: string, partial: Partial<Plane>): Promise<Plane>
}
