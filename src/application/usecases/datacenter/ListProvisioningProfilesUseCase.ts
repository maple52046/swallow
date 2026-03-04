import type { ProvisioningProfile } from '@/domain/platform/types'
import type { ProvisioningRepository } from '@/application/ports/ProvisioningRepository'

export class ListProvisioningProfilesUseCase {
  constructor(private readonly repo: ProvisioningRepository) {}

  async execute(): Promise<ProvisioningProfile[]> {
    return this.repo.listProfiles()
  }
}
