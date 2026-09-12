import type { OSImage } from "@/domain/site/types";
import type {
  CreateDeploymentTemplateInput,
  DeploymentTemplate,
  DeploymentTargetPreflightResult,
  DeployServersInput,
  DeployServersResult,
  ProvisioningOperationReference,
  ReleaseServersOperationInput,
  NetworkInspectionResult,
  UpdateDeploymentTemplateInput,
} from "@/domain/provisioning/types";

/**
 * Application port for the provisioning workspace.
 *
 * Implementations must keep template cloud-init write-only, return a false-valid
 * readiness report as data, and reserve thrown errors for failed inspections or
 * mutations.
 */
export interface ProvisioningRepository {
  listTemplates(filters?: {
    siteId?: string;
    integrationId?: string;
  }): Promise<DeploymentTemplate[]>;
  getTemplate(id: string): Promise<DeploymentTemplate | null>;
  createTemplate(
    input: CreateDeploymentTemplateInput,
  ): Promise<DeploymentTemplate>;
  updateTemplate(
    id: string,
    input: UpdateDeploymentTemplateInput,
  ): Promise<DeploymentTemplate>;
  deleteTemplate(id: string): Promise<void>;
  replaceTemplateUserData(id: string, userData: string): Promise<void>;
  clearTemplateUserData(id: string): Promise<void>;
  preflightDeploymentTargets(
    serverIds: string[],
  ): Promise<DeploymentTargetPreflightResult>;
  inspectDeploymentNetworks(
    serverIds: string[],
  ): Promise<NetworkInspectionResult>;
  deployServers(input: DeployServersInput): Promise<DeployServersResult>;
  createDeploymentOperation(
    input: DeployServersInput,
  ): Promise<ProvisioningOperationReference>;
  createReleaseOperation(
    input: ReleaseServersOperationInput,
  ): Promise<ProvisioningOperationReference>;
  listOSImages(integrationId: string): Promise<OSImage[]>;
  deleteOSImage(
    integrationId: string,
    imageId: string,
    architecture: string,
  ): Promise<void>;
  /**
   * Sets the swallow-owned display overlay (name, OS, release) for one image. Each provided,
   * non-empty field becomes the effective value on the next catalog read while the provider
   * value is preserved; an empty field clears that override. When every field is empty the
   * overlay is removed entirely. The provider is never changed.
   */
  setOSImageOverlay(
    integrationId: string,
    imageId: string,
    architecture: string,
    overlay: OSImageOverlayInput,
  ): Promise<void>;
  /**
   * Removes the swallow-owned overlay, reverting every field to its provider value. Safe to
   * call when no overlay exists.
   */
  clearOSImageOverlay(
    integrationId: string,
    imageId: string,
    architecture: string,
  ): Promise<void>;
}

/**
 * The overridable display fields and tags of an OS image overlay. An empty field clears that
 * override; an empty `tags` list clears the tags.
 */
export interface OSImageOverlayInput {
  name: string;
  osSystem: string;
  release: string;
  tags: string[];
}
