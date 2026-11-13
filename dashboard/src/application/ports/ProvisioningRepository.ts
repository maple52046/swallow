import type { OSImage, OSImageMetadata, ProvisionerImageCatalog } from "@/domain/site/types";
import type {
  BootISO,
  BootISOCatalog,
  CreateBootISOInput,
  CreateDeploymentTemplateInput,
  CreateImageVerificationInput,
  DeploymentTemplate,
  DeploymentTargetPreflightResult,
  DeployServersInput,
  EditServerTagsInput,
  HostEnrollmentBundle,
  ProvisionerHttpProxy,
  ProvisionerSubnet,
  ProvisioningOperationReference,
  RecoverServersOperationInput,
  ReleaseServersOperationInput,
  NetworkInspectionResult,
  ServerTagOption,
  ServerTagsResult,
  SetProvisionerHttpProxyInput,
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
   * Reads the OS Image catalog of every provisioner of `siteId` (of every Site when omitted) in one
   * reference read any user may make. Resolves with one catalog per provisioner, ordered by Site and
   * name; a provisioner whose catalog could not be read is a catalog with an `error`, not a
   * rejection. Rejects with the shared error envelope for an unknown Site or a failed request.
   */
  listOSImageCatalogs(siteId?: string): Promise<ProvisionerImageCatalog[]>;
  /**
   * Uploads a new provider-owned OS image to one provisioner and returns the created catalog
   * row once the provisioner reports it fully staged. The implementation first hashes the file
   * in the browser, then records the upload intent, then streams the file; swallow forwards it
   * to the provider and keeps no copy, and whether the result is a custom image is decided by
   * the provisioner, not by this call. `onProgress` reports each phase for the (potentially
   * multi-gigabyte) artifact. Rejects with the shared error envelope when the provisioner does not
   * support upload, refuses the intent or the artifact, or has not staged the image in time.
   */
  uploadOSImage(
    integrationId: string,
    input: UploadOSImageInput,
    onProgress?: (progress: OSImageUploadProgress) => void,
  ): Promise<OSImage>;
  deleteOSImage(
    integrationId: string,
    imageId: string,
    architecture: string,
  ): Promise<void>;
  /**
   * Replaces the metadata (name, OS, release, tags, default user) of one image. Each non-empty
   * label becomes the effective value on the next catalog read while the provider value is
   * preserved; an empty field clears that label. When every field is empty the metadata is
   * removed entirely. The provider is never changed. Rejects with `validation_error` for an
   * over-long field or a non-POSIX default user.
   */
  setOSImageMetadata(
    integrationId: string,
    imageId: string,
    architecture: string,
    metadata: OSImageMetadata,
  ): Promise<void>;
  /**
   * Removes the metadata, reverting every field to its provider value. Safe to call when the
   * image has none.
   */
  clearOSImageMetadata(
    integrationId: string,
    imageId: string,
    architecture: string,
  ): Promise<void>;
  /**
   * Lists the tags known for a Site with each tag's kind and how many Servers carry it, so the tag
   * editor can offer existing names and the tag manager can show how widely each is used: the
   * provisioner's catalog plus the spec tags it does not hold yet. A Site with no provisioner
   * yields an empty list.
   */
  listServerTags(siteId: string): Promise<ServerTagOption[]>;
  /**
   * Applies a tri-state tag edit to one or more Servers (decision 071) and returns each Server's
   * resulting manual tags. Manual tags are written to the spec and realized later by the Server
   * controller; a removable automatic tag in `remove` is taken off the machines by the provisioner
   * before this resolves. Rejects with the shared error envelope on an invalid edit (for example
   * adding an automatic tag) or `provider_unavailable` when the provisioner could not be reached
   * (the specs are already written; repeating the edit finishes it).
   */
  editServerTags(input: EditServerTagsInput): Promise<ServerTagsResult[]>;
  /**
   * Deletes a tag across a Site (server-tags.md): a manual tag leaves every Server spec that names
   * it and then the provisioner; an automatic tag is deleted from the provisioner only, which may
   * apply it again later. Resolves with the Servers whose spec changed. Rejects with `not_found`
   * for a tag the Site does not know, `conflict` while a Server not adopted yet carries it, or
   * `provider_unavailable` (repeat the request: any spec change is already written).
   */
  deleteServerTag(siteId: string, name: string): Promise<ServerTagsResult[]>;
  /**
   * Renames a manual tag across a Site, merging it into an existing manual tag of the new name,
   * and resolves with the Servers whose spec changed. Rejects like `deleteServerTag`, and with
   * `validation_error` for an unchanged or invalid name or an automatic tag on either side.
   */
  renameServerTag(siteId: string, name: string, newName: string): Promise<ServerTagsResult[]>;
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
   * Rebuilds a Boot ISO in place from the current template and its own rack address (contract
   * boot-isos.md, decision 047) and resolves with it once the new file is served. Its id and URL
   * stay the same, so Servers using it boot the new script from their next boot. Rejects with
   * `not_found`, `conflict` (the builder is unavailable — the message says why), or
   * `internal_error` carrying the packaging tool's summary; a failed rebuild keeps the previous file.
   */
  rebuildBootISO(id: string): Promise<BootISO>;
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
  /**
   * Reads one provisioner Integration's Provisioner HTTP Proxy live from its provisioner (contract
   * provisioner-http-proxy.md). Rejects with `not_found` for an unknown Integration,
   * `validation_error` for one that is not a capable provisioner, and `provider_unavailable` when
   * the provisioner cannot be reached or the Integration has no credential.
   */
  getProvisionerHttpProxy(integrationId: string): Promise<ProvisionerHttpProxy>;
  /**
   * Writes a Provisioner HTTP Proxy through to the provisioner and resolves with the value read
   * back. It only affects deployments the provisioner starts afterwards. Rejects with
   * `validation_error` (mode, URL, or the provisioner's own refusal — the message says which) and
   * `provider_unavailable` (unreachable, or a MAAS account that is not an administrator). A
   * rejection may follow a partial write; read again to see what the provisioner holds.
   */
  setProvisionerHttpProxy(integrationId: string, input: SetProvisionerHttpProxyInput): Promise<ProvisionerHttpProxy>;
  /**
   * Lists one provisioner Integration's subnets with their Subnet DNS, read live from its
   * provisioner in its order (contract provisioner-subnet-dns.md). Rejects like
   * `getProvisionerHttpProxy`: `not_found`, `validation_error` for an Integration that is not a
   * capable provisioner, and `provider_unavailable`.
   */
  listProvisionerSubnets(integrationId: string): Promise<ProvisionerSubnet[]>;
  /**
   * Replaces one subnet's Subnet DNS and resolves with the subnet read back; `[]` clears it. It
   * only affects `automatic` and `static` deployments the provisioner starts afterwards. Rejects
   * with `validation_error` (an address, a repeat, more than eight, or the provisioner's refusal —
   * the message says which), `not_found` (Integration or subnet), and `provider_unavailable`
   * (unreachable, or a MAAS account that is not an administrator).
   */
  setProvisionerSubnetDnsServers(integrationId: string, subnetId: string, dnsServers: string[]): Promise<ProvisionerSubnet>;
}

/**
 * Progress of {@link ProvisioningRepository.uploadOSImage}, in bytes of the selected file.
 *
 * - `hashing`: the file is being read in the browser to compute the SHA-256 the upload declares;
 *   nothing has been sent yet.
 * - `uploading`: the file is being sent. `loaded` reaching `total` means every byte left the
 *   browser; the call still waits until the provisioner reports the image staged.
 *
 * `total` is 0 when the browser cannot report the upload length.
 */
export interface OSImageUploadProgress {
  phase: "hashing" | "uploading";
  loaded: number;
  total: number;
}

/**
 * User intent for uploading a new OS image to a provisioner. `name` and `architecture` are
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
   * Optional default login user stored in the new image's metadata (decision 039). The
   * backend rejects a non-POSIX value when the upload is created, before any bytes are sent.
   */
  defaultUser?: string;
  file: File;
}
