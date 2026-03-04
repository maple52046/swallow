import type { ProvisioningImage, ProvisioningProfile, ProvisioningJob } from '@/domain/platform/types'
import type { ProvisioningRepository } from '@/application/ports/ProvisioningRepository'

const daysAgo = (d: number) => new Date(Date.now() - d * 86400000).toISOString()
const hoursAgo = (h: number) => new Date(Date.now() - h * 3600000).toISOString()

const IMAGES: ProvisioningImage[] = [
  { id: 'img-ubuntu-22-nvidia', name: 'Ubuntu 22.04 + NVIDIA', os: 'Ubuntu', version: '22.04 LTS', arch: 'x86_64', size: '4.2GB', tags: ['nvidia', 'cuda', 'prod'], createdAt: daysAgo(30) },
  { id: 'img-rocky9-nvidia', name: 'Rocky Linux 9 + NVIDIA', os: 'Rocky Linux', version: '9.3', arch: 'x86_64', size: '3.8GB', tags: ['nvidia', 'prod'], createdAt: daysAgo(25) },
  { id: 'img-ubuntu-22-amd', name: 'Ubuntu 22.04 + ROCm', os: 'Ubuntu', version: '22.04 LTS', arch: 'x86_64', size: '5.1GB', tags: ['amd', 'rocm', 'prod'], createdAt: daysAgo(20) },
  { id: 'img-ubuntu-22-base', name: 'Ubuntu 22.04 Base', os: 'Ubuntu', version: '22.04 LTS', arch: 'x86_64', size: '1.8GB', tags: ['base', 'minimal'], createdAt: daysAgo(45) },
]

const PROFILES: ProvisioningProfile[] = [
  { id: 'profile-nvidia-prod', name: 'NVIDIA Production Stack', description: 'Ubuntu 22.04 + NVIDIA Driver 535 + CUDA 12.2 + NCCL', imageId: 'img-ubuntu-22-nvidia', imageName: 'Ubuntu 22.04 + NVIDIA', packages: ['nvidia-driver-535', 'cuda-12-2', 'nccl-2.18'], scripts: ['post-install-nvidia.sh', 'validate-gpu.sh'], targetVendors: ['nvidia'], createdAt: daysAgo(30), updatedAt: daysAgo(5) },
  { id: 'profile-h100-prod', name: 'H100 Production Stack', description: 'Rocky Linux 9 + NVIDIA Driver 550 + CUDA 12.4', imageId: 'img-rocky9-nvidia', imageName: 'Rocky Linux 9 + NVIDIA', packages: ['nvidia-driver-550', 'cuda-12-4', 'nccl-2.20'], scripts: ['post-install-h100.sh', 'validate-gpu.sh'], targetVendors: ['nvidia'], createdAt: daysAgo(20), updatedAt: daysAgo(2) },
  { id: 'profile-amd-prod', name: 'AMD ROCm 6.1 Stack', description: 'Ubuntu 22.04 + ROCm 6.1.2 + RCCL', imageId: 'img-ubuntu-22-amd', imageName: 'Ubuntu 22.04 + ROCm', packages: ['rocm-6.1.2', 'rccl-2.18'], scripts: ['post-install-rocm.sh', 'validate-gpu.sh'], targetVendors: ['amd'], createdAt: daysAgo(25), updatedAt: daysAgo(3) },
]

const JOBS: ProvisioningJob[] = [
  { id: 'job-001', profileId: 'profile-amd-prod', profileName: 'AMD ROCm 6.1 Stack', targetHostId: 'host-06', targetHostName: 'gpu-host-06', status: 'running', progress: 65, startedAt: hoursAgo(0.5), createdAt: hoursAgo(0.6), logs: ['[00:00] Starting provisioning', '[00:05] Image downloaded', '[00:20] ROCm packages installing...'] },
  { id: 'job-002', profileId: 'profile-nvidia-prod', profileName: 'NVIDIA Production Stack', targetHostId: 'host-07', targetHostName: 'gpu-host-07', status: 'succeeded', progress: 100, startedAt: daysAgo(1), completedAt: daysAgo(0.99), createdAt: daysAgo(1.01), logs: ['[00:00] Starting', '[00:30] All packages installed', '[00:35] Validation passed'] },
]

export class MockProvisioningRepository implements ProvisioningRepository {
  async listImages(): Promise<ProvisioningImage[]> { return [...IMAGES] }
  async listProfiles(): Promise<ProvisioningProfile[]> { return [...PROFILES] }
  async listJobs(): Promise<ProvisioningJob[]> { return [...JOBS] }
  async getJob(id: string): Promise<ProvisioningJob | null> { return JOBS.find((j) => j.id === id) ?? null }
}
