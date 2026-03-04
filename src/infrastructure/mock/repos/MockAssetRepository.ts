import type { Host, StorageDevice, NetworkSwitch } from '@/domain/asset/types'
import type { AssetRepository, ListHostsFilters } from '@/application/ports/AssetRepository'
import { seedHosts, seedStorageDevices, seedNetworkSwitches } from '@/infrastructure/mock/data/seedHosts'

export class MockAssetRepository implements AssetRepository {
  private hosts = new Map(seedHosts.map((h) => [h.id, h]))
  private storage = [...seedStorageDevices]
  private switches = [...seedNetworkSwitches]

  async listHosts(filters?: ListHostsFilters): Promise<Host[]> {
    let items = Array.from(this.hosts.values())
    if (filters?.status) items = items.filter((h) => h.status === filters.status)
    if (filters?.site) items = items.filter((h) => h.site === filters.site)
    if (filters?.search) {
      const q = filters.search.toLowerCase()
      items = items.filter((h) => h.name.toLowerCase().includes(q) || h.ipAddress.includes(q) || h.tags.some((t) => t.includes(q)))
    }
    return items.sort((a, b) => a.name.localeCompare(b.name))
  }

  async getHost(id: string): Promise<Host | null> {
    return this.hosts.get(id) ?? null
  }

  async listStorageDevices(): Promise<StorageDevice[]> {
    return [...this.storage]
  }

  async listNetworkSwitches(): Promise<NetworkSwitch[]> {
    return [...this.switches]
  }
}
