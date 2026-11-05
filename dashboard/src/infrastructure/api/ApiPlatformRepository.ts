import type { PlatformRepository } from '@/application/ports/PlatformRepository'
import type {
  Platform,
  DeployPlatformInput,
  PlatformChangeResult,
  MembershipReport,
  UninstallPlatformOptions,
} from '@/domain/platform/types'
import { APPROVE_PLATFORM_WRITE, toPlatformChangeResult, type PlatformSpecWrite } from '@/application/platform/platformChangeResult'
import { ApiRequestError, apiRequest } from './client'

/**
 * The platform contract maps directly onto the domain type, so there is no reshaping here.
 * The one translation is 404 on getPlatform, which is a stale link rather than an error to
 * surface, matching how the server repository treats a missing server.
 */
export class ApiPlatformRepository implements PlatformRepository {
  async listPlatforms(siteId?: string): Promise<Platform[]> {
    const suffix = siteId ? `?siteId=${encodeURIComponent(siteId)}` : ''
    return apiRequest<Platform[]>(`/api/v1/platforms${suffix}`)
  }

  async getPlatform(id: string): Promise<Platform | null> {
    try {
      return await apiRequest<Platform>(`/api/v1/platforms/${encodeURIComponent(id)}`)
    } catch (error) {
      if (error instanceof ApiRequestError && error.status === 404) {
        return null
      }
      throw error
    }
  }

  async createPlatform(input: DeployPlatformInput): Promise<PlatformChangeResult> {
    return toPlatformChangeResult(await apiRequest<PlatformSpecWrite>(`/api/v1/platforms?${APPROVE_PLATFORM_WRITE}`, {
      method: 'POST',
      body: JSON.stringify(toPlatformCreate(input)),
    }))
  }

  async deletePlatform(id: string, options?: UninstallPlatformOptions): Promise<PlatformChangeResult> {
    const written = await apiRequest<PlatformSpecWrite | undefined>(
      `/api/v1/platforms/${encodeURIComponent(id)}?${APPROVE_PLATFORM_WRITE}`,
      options ? { method: 'DELETE', body: JSON.stringify(options) } : { method: 'DELETE' },
    )
    // 204: nothing was installed, so the platform is gone at once. A 200 (its uninstall already
    // runs) and a 202 (the uninstall's plan) both carry the Platform.
    return written
      ? toPlatformChangeResult(written)
      : { platformId: id, planId: null, deleted: true, awaitingApproval: false, waitReason: '', blocked: false }
  }

  async deletePlatformRecord(id: string): Promise<void> {
    await apiRequest<void>(`/api/v1/platforms/${encodeURIComponent(id)}?recordOnly=true`, { method: 'DELETE' })
  }

  async syncPlatform(id: string): Promise<MembershipReport> {
    return apiRequest<MembershipReport>(`/api/v1/platforms/${encodeURIComponent(id)}/sync`, {
      method: 'POST',
    })
  }

}
/**
 * Writes the deployment the wizard describes as a Platform spec: the role or node assignments
 * become members with roles, and the remaining type fields become settings.
 */
export function toPlatformCreate(input: DeployPlatformInput) {
  const { siteId, name, type = 'kubernetes', roleAssignments, slurm, machinePreparation, ...settings } = input
  if (type === 'slurm' && slurm) {
    const { nodeAssignments, ...slurmSettings } = slurm
    const members = nodeAssignments.map((assignment) => ({
      serverId: assignment.serverId,
      roles: [
        ...(assignment.controller ? ['controller'] : []),
        ...(assignment.compute ? ['compute'] : []),
        ...(assignment.login ? ['login'] : []),
      ],
    }))
    return { siteId, name, type, spec: { members, settings: { ...settings, slurm: slurmSettings }, ...(machinePreparation ? { machinePreparation } : {}) } }
  }
  const members = (roleAssignments ?? []).map((assignment) => ({
    serverId: assignment.serverId,
    roles: assignment.role === 'control-plane'
      ? ['control-plane', ...(assignment.runWorkloads ? ['workloads'] : [])]
      : ['worker'],
  }))
  return { siteId, name, type, spec: { members, settings, ...(machinePreparation ? { machinePreparation } : {}) } }
}
