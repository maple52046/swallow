import type { OSImage } from "@/domain/site/types";
import type {
  BootISO,
  BootISOCatalog,
  CreateBootISOInput,
  CreateDeploymentTemplateInput,
  CreateImageVerificationInput,
  DeploymentTemplate,
  DeploymentTargetPreflightResult,
  DeployServersInput,
  DeployServersResult,
  EditServerTagsInput,
  HostEnrollmentBundle,
  ProvisioningOperationReference,
  RecoverServersOperationInput,
  ReleaseServersOperationInput,
  NetworkInspectionResult,
  ServerTagOption,
  ServerTagsResult,
  UpdateDeploymentTemplateInput,
  VirtualMachineEnrollmentAccepted,
  VirtualMachineEnrollmentInput,
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
  /**
   * Launches a verify-os-image Operation that proves a custom OS Image works for one deploy target
   * by deploying it on the chosen ready Server, recording the swallow-owned verification, and
   * auto-releasing the Server. Returns the operation reference.
   */
  createImageVerification(
    input: CreateImageVerificationInput,
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
   * Sets the swallow-owned overlay (name, OS, release, tags, default user) for one image. Each
   * non-empty field becomes the effective value on the next catalog read while the provider
   * value is preserved; an empty field clears that override. When every field is empty the
   * overlay is removed entirely. The provider is never changed. Rejects with `validation_error`
   * for an over-long field or a non-POSIX default user.
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
  /**
   * Lists Boot ISOs (contract boot-isos.md), optionally narrowed to a Site and/or provisioner
   * Integration, together with whether this installation can build them. A builder that cannot
   * build is data (`builder.available: false` with a reason), not a rejection; the list still
   * resolves so existing ISOs stay visible and deletable.
   */
  listBootISOs(filters?: {
    siteId?: string;
    integrationId?: string;
  }): Promise<BootISOCatalog>;
  /**
   * Builds a Boot ISO synchronously (seconds) and resolves with it once its file exists. Rejects
   * with the shared error envelope: `validation_error` (name, rack address, or not a provisioner),
   * `conflict` (name taken for that provisioner, or the builder unavailable — the message says
   * why), or `internal_error` carrying the packaging tool's summary; nothing is stored on failure.
   */
  createBootISO(input: CreateBootISOInput): Promise<BootISO>;
  /**
   * Deletes a Boot ISO and its file. Rejects with `conflict` while a Server's enabled Boot Media
   * uses it (the message names how many), and `not_found` when it is already gone.
   */
  deleteBootISO(id: string): Promise<void>;
  /**
   * Reads the existing-OS enrollment bundle of one provisioner Integration (contract
   * server-enrollment.md): its endpoint, its credential, and the one-line command a host runs to
   * enroll itself while keeping its OS. `swallowUrl` is the address the host reaches swallow at
   * (the console's own origin); the command downloads the enrollment script and the CLI from it.
   * The credential is a secret the caller must keep in memory only. Rejects with
   * `validation_error` for an unusable `swallowUrl` or a provisioner without existing-host
   * enrollment, and `provider_unavailable` when the Integration has no stored credential.
   */
  getHostEnrollmentBundle(integrationId: string, swallowUrl: string): Promise<HostEnrollmentBundle>;
  /**
   * Starts enrolling libvirt virtual machines of a Hypervisor by domain name (contract
   * server-enrollment.md, decision 055) and resolves with the enroll-virtual-machines Workflow's id
   * as soon as it is accepted; the per-domain Tasks run in the worker and report attention there.
   * Rejects with `validation_error` (malformed request, a provisioner that cannot register machines,
   * or another provisioner's Boot ISO), `not_found`, or `conflict` (Hypervisor not deployed, locked,
   * outside the provisioner's Site, busy with another Workflow, no Deployment Key, or the Boot ISO
   * not served).
   */
  enrollVirtualMachines(input: VirtualMachineEnrollmentInput): Promise<VirtualMachineEnrollmentAccepted>;
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
  /**
   * Optional default login user stored on the new image's swallow overlay (decision 039). The
   * backend rejects a non-POSIX value before any bytes are uploaded.
   */
  defaultUser?: string;
  file: File;
}

/**
 * The complete swallow-owned overlay of an OS image: display overrides, tags, and default user.
 * The backend replaces the whole overlay on every set, so callers editing one field must send the
 * current values of the others. An empty field clears that override (for `defaultUser`, the
 * built-in default applies again); an empty `tags` list clears the tags.
 */
export interface OSImageOverlayInput {
  name: string;
  osSystem: string;
  release: string;
  tags: string[];
  defaultUser: string;
}
