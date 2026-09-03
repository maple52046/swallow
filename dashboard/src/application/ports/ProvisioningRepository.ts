import type { OSImage } from '@/domain/site/types'
import type {
  CreateDeploymentTemplateInput,
  DeploymentTemplate,
  DeploymentTargetPreflightResult,
  DeployServersInput,
  DeployServersResult,
  NetworkInspectionResult,
  UpdateDeploymentTemplateInput,
} from '@/domain/provisioning/types'

/**
 * Application port for the provisioning workspace.
 *
 * Implementations must keep template cloud-init write-only, return a false-valid
 * readiness report as data, and reserve thrown errors for failed inspections or
 * mutations.
 */
export interface ProvisioningRepository {
  listTemplates(filters?: { siteId?: string; integrationId?: string }): Promise<DeploymentTemplate[]>
  getTemplate(id: string): Promise<DeploymentTemplate | null>
  createTemplate(input: CreateDeploymentTemplateInput): Promise<DeploymentTemplate>
  updateTemplate(id: string, input: UpdateDeploymentTemplateInput): Promise<DeploymentTemplate>
  deleteTemplate(id: string): Promise<void>
  replaceTemplateUserData(id: string, userData: string): Promise<void>
  clearTemplateUserData(id: string): Promise<void>
  preflightDeploymentTargets(serverIds: string[]): Promise<DeploymentTargetPreflightResult>
  inspectDeploymentNetworks(serverIds: string[]): Promise<NetworkInspectionResult>
  deployServers(input: DeployServersInput): Promise<DeployServersResult>
  listOSImages(integrationId: string): Promise<OSImage[]>
}
