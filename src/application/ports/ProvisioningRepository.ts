import type {
  ProvisioningImage,
  ProvisioningProfile,
  ProvisioningJob,
} from '@/domain/platform/types'

export interface ProvisioningRepository {
  listImages(): Promise<ProvisioningImage[]>
  listProfiles(): Promise<ProvisioningProfile[]>
  listJobs(): Promise<ProvisioningJob[]>
  getJob(id: string): Promise<ProvisioningJob | null>
}
