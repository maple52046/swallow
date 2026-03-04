import type { NetworkSwitch } from '@/domain/asset/types'
import type { AssetRepository } from '@/application/ports/AssetRepository'

export class ListSwitchesUseCase {
  constructor(private readonly repo: AssetRepository) {}

  async execute(): Promise<NetworkSwitch[]> {
    return this.repo.listNetworkSwitches()
  }
}
