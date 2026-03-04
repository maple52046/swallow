import type { Host, StorageDevice, NetworkSwitch, AssetStatus } from '@/domain/asset/types'

export interface ListHostsFilters {
  status?: AssetStatus
  site?: string
  search?: string
}

export interface AssetRepository {
  listHosts(filters?: ListHostsFilters): Promise<Host[]>
  getHost(id: string): Promise<Host | null>
  listStorageDevices(): Promise<StorageDevice[]>
  listNetworkSwitches(): Promise<NetworkSwitch[]>
}
