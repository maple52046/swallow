import type { Host, StorageDevice, NetworkSwitch, Connection, SSHKey } from '@/domain/asset/types'

const daysAgo = (d: number) => new Date(Date.now() - d * 86400 * 1000).toISOString()

export const seedHosts: Host[] = [
  { id: 'host-01', name: 'gpu-host-01', status: 'healthy', site: 'dc-east', rack: 'R01', ipAddress: '10.0.1.11', bmcAddress: '10.0.2.11', os: 'Ubuntu 22.04 LTS', cpuModel: 'AMD EPYC 9454', cpuCores: 96, memoryGB: 512, gpuCount: 8, gpuVendors: ['nvidia'], driverVersion: '535.161.08', cudaVersion: '12.2', tags: ['prod', 'a100'] },
  { id: 'host-02', name: 'gpu-host-02', status: 'healthy', site: 'dc-east', rack: 'R01', ipAddress: '10.0.1.12', bmcAddress: '10.0.2.12', os: 'Ubuntu 22.04 LTS', cpuModel: 'AMD EPYC 9454', cpuCores: 96, memoryGB: 512, gpuCount: 8, gpuVendors: ['nvidia'], driverVersion: '535.161.08', cudaVersion: '12.2', tags: ['prod', 'a100'] },
  { id: 'host-03', name: 'gpu-host-03', status: 'healthy', site: 'dc-east', rack: 'R02', ipAddress: '10.0.1.13', bmcAddress: '10.0.2.13', os: 'Rocky Linux 9', cpuModel: 'Intel Xeon Platinum 8480+', cpuCores: 112, memoryGB: 768, gpuCount: 8, gpuVendors: ['nvidia'], driverVersion: '550.54.15', cudaVersion: '12.4', tags: ['prod', 'h100'] },
  { id: 'host-04', name: 'gpu-host-04', status: 'healthy', site: 'dc-east', rack: 'R02', ipAddress: '10.0.1.14', bmcAddress: '10.0.2.14', os: 'Rocky Linux 9', cpuModel: 'Intel Xeon Platinum 8480+', cpuCores: 112, memoryGB: 768, gpuCount: 8, gpuVendors: ['nvidia'], driverVersion: '550.54.15', cudaVersion: '12.4', tags: ['prod', 'h100'] },
  { id: 'host-05', name: 'gpu-host-05', status: 'healthy', site: 'dc-west', rack: 'R10', ipAddress: '10.0.3.11', bmcAddress: '10.0.4.11', os: 'Ubuntu 22.04 LTS', cpuModel: 'AMD EPYC 9654', cpuCores: 192, memoryGB: 1024, gpuCount: 8, gpuVendors: ['amd'], rocmVersion: '6.1.2', tags: ['prod', 'mi300x'] },
  { id: 'host-06', name: 'gpu-host-06', status: 'healthy', site: 'dc-west', rack: 'R10', ipAddress: '10.0.3.12', bmcAddress: '10.0.4.12', os: 'Ubuntu 22.04 LTS', cpuModel: 'AMD EPYC 9654', cpuCores: 192, memoryGB: 1024, gpuCount: 8, gpuVendors: ['amd'], rocmVersion: '6.0.0', tags: ['prod', 'mi300x', 'driver-drift'] },
  { id: 'host-07', name: 'gpu-host-07', status: 'degraded', site: 'dc-east', rack: 'R03', ipAddress: '10.0.1.17', bmcAddress: '10.0.2.17', os: 'Ubuntu 22.04 LTS', cpuModel: 'AMD EPYC 9454', cpuCores: 96, memoryGB: 512, gpuCount: 4, gpuVendors: ['nvidia'], driverVersion: '535.104.05', cudaVersion: '12.2', tags: ['prod', 'degraded'] },
  { id: 'host-08', name: 'gpu-host-08', status: 'critical', site: 'dc-east', rack: 'R03', ipAddress: '10.0.1.18', bmcAddress: '10.0.2.18', os: 'Ubuntu 22.04 LTS', cpuModel: 'AMD EPYC 9454', cpuCores: 96, memoryGB: 512, gpuCount: 4, gpuVendors: ['nvidia'], driverVersion: '535.104.05', cudaVersion: '12.2', tags: ['prod', 'overheating'] },
  { id: 'host-09', name: 'cpu-host-01', status: 'healthy', site: 'dc-east', rack: 'R04', ipAddress: '10.0.1.20', bmcAddress: '10.0.2.20', os: 'Rocky Linux 9', cpuModel: 'Intel Xeon Gold 6548Y+', cpuCores: 64, memoryGB: 256, gpuCount: 0, gpuVendors: [], tags: ['infra', 'control-plane'] },
  { id: 'host-10', name: 'cpu-host-02', status: 'healthy', site: 'dc-west', rack: 'R11', ipAddress: '10.0.3.20', bmcAddress: '10.0.4.20', os: 'Rocky Linux 9', cpuModel: 'Intel Xeon Gold 6548Y+', cpuCores: 64, memoryGB: 256, gpuCount: 0, gpuVendors: [], tags: ['infra'] },
]

export const seedStorageDevices: StorageDevice[] = [
  { id: 'stor-01', name: 'dc-east-stor-01', type: 'nvme', status: 'healthy', site: 'dc-east', rack: 'R05', capacityTB: 200, usedTB: 142, latencyMs: 0.2, iops: 8000000, vendor: 'Pure Storage', model: 'FlashArray//C60', firmware: '6.4.3', tags: ['prod', 'fast'] },
  { id: 'stor-02', name: 'dc-east-stor-02', type: 'nvme', status: 'healthy', site: 'dc-east', rack: 'R05', capacityTB: 200, usedTB: 88, latencyMs: 0.25, iops: 7500000, vendor: 'Pure Storage', model: 'FlashArray//C60', firmware: '6.4.3', tags: ['prod', 'fast'] },
  { id: 'stor-03', name: 'dc-east-ceph-01', type: 'ceph', status: 'healthy', site: 'dc-east', rack: 'R06', capacityTB: 2000, usedTB: 1240, latencyMs: 5, iops: 500000, vendor: 'Ceph Community', model: 'Ceph 18.2', firmware: '18.2.1', tags: ['prod', 'archival'] },
  { id: 'stor-04', name: 'dc-west-stor-01', type: 'nvme', status: 'degraded', site: 'dc-west', rack: 'R15', capacityTB: 100, usedTB: 75, latencyMs: 12, iops: 4000000, vendor: 'NetApp', model: 'AFF A900', firmware: '9.13', tags: ['prod'] },
]

export const seedNetworkSwitches: NetworkSwitch[] = [
  { id: 'sw-01', name: 'dc-east-ib-01', status: 'healthy', site: 'dc-east', rack: 'R07', vendor: 'NVIDIA', model: 'QM9700 InfiniBand HDR', firmware: '3.11.1012', portCount: 40, activePorts: 38, speed: '200Gb/s HDR', tags: ['prod', 'infiniband'] },
  { id: 'sw-02', name: 'dc-east-eth-01', status: 'healthy', site: 'dc-east', rack: 'R07', vendor: 'Arista', model: '7280R3-48YC6', firmware: '4.28.3M', portCount: 48, activePorts: 45, speed: '100GbE', tags: ['prod', 'ethernet'] },
  { id: 'sw-03', name: 'dc-west-ib-01', status: 'healthy', site: 'dc-west', rack: 'R17', vendor: 'NVIDIA', model: 'QM9700 InfiniBand HDR', firmware: '3.11.1012', portCount: 40, activePorts: 32, speed: '200Gb/s HDR', tags: ['prod', 'infiniband'] },
]

export const seedConnections: Connection[] = [
  { id: 'conn-01', name: 'dc-east-bastion', type: 'bastion', host: 'bastion.dc-east.example.com', port: 22, username: 'ops', labels: ['dc-east', 'prod'], createdAt: daysAgo(90), updatedAt: daysAgo(10) },
  { id: 'conn-02', name: 'gpu-host-01-ssh', type: 'ssh', host: '10.0.1.11', port: 22, username: 'root', labels: ['dc-east', 'host-01'], createdAt: daysAgo(90), updatedAt: daysAgo(10) },
  { id: 'conn-03', name: 'dc-west-bastion', type: 'bastion', host: 'bastion.dc-west.example.com', port: 22, username: 'ops', labels: ['dc-west', 'prod'], createdAt: daysAgo(60), updatedAt: daysAgo(5) },
]

export const seedSSHKeys: SSHKey[] = [
  { id: 'key-01', name: 'ops-prod-key', fingerprint: 'SHA256:kP8xZmQ7rLjN3vB2mT0sU5hF1gD9cE4wA6iR8yO1pK', vaultRef: 'vault:kv/ssh-keys/ops-prod', createdAt: daysAgo(90) },
  { id: 'key-02', name: 'slurm-headnode-key', fingerprint: 'SHA256:mN2vY4hJ6kL8pR1tE3wA9sD5fG7bX0cQ2oI4uZ8nM', vaultRef: 'vault:kv/ssh-keys/slurm-headnode', createdAt: daysAgo(120) },
]
