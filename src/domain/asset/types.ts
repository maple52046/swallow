export type AssetStatus = 'healthy' | 'degraded' | 'critical' | 'offline' | 'unknown'
export type AssetType = 'host' | 'gpu' | 'storage' | 'switch'
export type ConnectionType = 'ssh' | 'bastion' | 'vpn'

export interface Host {
  id: string
  name: string
  status: AssetStatus
  site: string
  rack: string
  ipAddress: string
  bmcAddress: string
  os: string
  cpuModel: string
  cpuCores: number
  memoryGB: number
  gpuCount: number
  gpuVendors: string[]
  driverVersion?: string
  cudaVersion?: string
  rocmVersion?: string
  tags: string[]
}

export interface StorageDevice {
  id: string
  name: string
  type: 'nvme' | 'ssd' | 'hdd' | 'nfs' | 'ceph'
  status: AssetStatus
  site: string
  rack: string
  capacityTB: number
  usedTB: number
  latencyMs: number
  iops: number
  vendor: string
  model: string
  firmware: string
  tags: string[]
}

export interface NetworkSwitch {
  id: string
  name: string
  status: AssetStatus
  site: string
  rack: string
  vendor: string
  model: string
  firmware: string
  portCount: number
  activePorts: number
  speed: string
  tags: string[]
}

export interface Connection {
  id: string
  name: string
  type: ConnectionType
  host: string
  port: number
  username: string
  labels: string[]
  createdAt: string
  updatedAt: string
}

export interface SSHKey {
  id: string
  name: string
  publicKey?: string
  type?: string
  fingerprint: string
  vaultRef: string
  createdAt: string
}

export interface ImportSSHKeyInput {
  name: string
  publicKey: string
}

export interface AccessPolicy {
  id: string
  name: string
  rules: string[]
  createdAt: string
}
