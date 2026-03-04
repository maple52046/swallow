import type { StorageDevice } from '@/domain/asset/types'
import type { AssetRepository } from '@/application/ports/AssetRepository'

export class ListStorageUseCase {
  constructor(private readonly repo: AssetRepository) {}

  async execute(): Promise<StorageDevice[]> {
    return this.repo.listStorageDevices()
  }
}
