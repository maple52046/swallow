import type { ProvisioningJob } from '@/domain/platform/types'
import type { ProvisioningRepository } from '@/application/ports/ProvisioningRepository'

export class ListProvisioningJobsUseCase {
  constructor(private readonly repo: ProvisioningRepository) {}

  async execute(): Promise<ProvisioningJob[]> {
    return this.repo.listJobs()
  }
}
