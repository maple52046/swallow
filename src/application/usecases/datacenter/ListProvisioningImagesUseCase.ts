import type { ProvisioningImage } from '@/domain/platform/types'
import type { ProvisioningRepository } from '@/application/ports/ProvisioningRepository'

export class ListProvisioningImagesUseCase {
  constructor(private readonly repo: ProvisioningRepository) {}

  async execute(): Promise<ProvisioningImage[]> {
    return this.repo.listImages()
  }
}
