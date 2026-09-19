import type { OSImage } from "@/domain/site/types";
import type {
  CreateDeploymentTemplateInput,
  DeploymentTemplate,
  DeploymentTargetPreflightResult,
  DeployServersInput,
  DeployServersResult,
  EditServerTagsInput,
  ProvisioningOperationReference,
  RecoverServersOperationInput,
  ReleaseServersOperationInput,
  NetworkInspectionResult,
  ServerTagOption,
  ServerTagsResult,
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
  /**
   * Starts a durable "Return to Ready" recovery for a bounded batch of Servers whose
   * provisioning axis is not usable (failed, broken, or rescue). The backend gates each
   * target on the recovery policy and chooses the provider primitive by state.
   */
  createRecoverOperation(
    input: RecoverServersOperationInput,
  ): Promise<ProvisioningOperationReference>;
  listOSImages(integrationId: string): Promise<OSImage[]>;
  /**
   * Uploads a new provider-owned OS image to one provisioner and returns the created catalog
   * row. Swallow streams the file to the provider and keeps no copy; whether the result is a
   * custom image is decided by the provisioner, not by this call. `onProgress` reports upload
   * bytes for the (potentially multi-gigabyte) artifact. Rejects with the shared error envelope
   * when the provisioner does not support upload or the provider refuses the artifact.
   */
  uploadOSImage(
    integrationId: string,
    input: UploadOSImageInput,
    onProgress?: (progress: { loaded: number; total: number }) => void,
  ): Promise<OSImage>;
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
  /**
   * Lists the tags known for a Site, so the tag editor can offer existing names and disable the
   * provider-computed (automatic) ones. Ownership is capability-first (decision 031): the list is
   * the provisioner's own tags when it owns them, or the union of swallow-owned tags otherwise. A
   * Site with no provisioner yields an empty list.
   */
  listServerTags(siteId: string): Promise<ServerTagOption[]>;
  /**
   * Applies a tri-state tag edit to one or more Servers and returns each Server's effective tags.
   * Drives the provisioner when it owns tags and writes swallow-owned tags otherwise; either way the
   * returned tags are authoritative. Rejects with the shared error envelope on an invalid edit or a
   * provider refusal (for example an attempt to assign an automatic tag).
   */
  editServerTags(input: EditServerTagsInput): Promise<ServerTagsResult[]>;
}

/**
 * Operator intent for uploading a new OS image to a provisioner. `name` and `architecture` are
 * swallow-neutral; the backend maps them into the provider's own vocabulary. `filetype` is
 * provider-validated and, when omitted, uses the provider default. `file` is the artifact to
 * stream. The image's custom classification is provider-determined, never set here.
 */
export interface UploadOSImageInput {
  name: string;
  architecture: string;
  title?: string;
  filetype?: string;
  file: File;
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
