import type { K8sCluster, SlurmCluster } from '@/domain/plane/types'
import type { PlaneRepository } from '@/application/ports/PlaneRepository'

export class GetPlaneUseCase {
  constructor(private readonly repo: PlaneRepository) {}

  async executeK8s(id: string): Promise<K8sCluster | null> {
    return this.repo.getK8sCluster(id)
  }

  async executeSlurm(id: string): Promise<SlurmCluster | null> {
    return this.repo.getSlurmCluster(id)
  }
}
