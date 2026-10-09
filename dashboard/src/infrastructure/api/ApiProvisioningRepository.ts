import type {
  OSImageOverlayInput,
  ProvisioningRepository,
  UploadOSImageInput,
} from "@/application/ports/ProvisioningRepository";
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
import type { OSImage } from "@/domain/site/types";
import { ApiRequestError, apiRequest, apiUpload, type UploadProgress } from "./client";

/**
 * HTTP adapter for the active provider-owned provisioning contract.
 *
 * It sends cloud-init only on write calls and never caches it. A completed target
 * preflight remains a domain result even when it reports blocking issues.
 */
export class ApiProvisioningRepository implements ProvisioningRepository {
  async listTemplates(filters?: {
    siteId?: string;
    integrationId?: string;
  }): Promise<DeploymentTemplate[]> {
    const query = new URLSearchParams();
    if (filters?.siteId) query.set("siteId", filters.siteId);
    if (filters?.integrationId)
      query.set("integrationId", filters.integrationId);
    const suffix = query.toString() ? `?${query}` : "";
    return apiRequest<DeploymentTemplate[]>(
      `/api/v1/provisioning/templates${suffix}`,
    );
  }

  async getTemplate(id: string): Promise<DeploymentTemplate | null> {
    try {
      return await apiRequest<DeploymentTemplate>(
        `/api/v1/provisioning/templates/${encodeURIComponent(id)}`,
      );
    } catch (error) {
      if (error instanceof ApiRequestError && error.status === 404) return null;
      throw error;
    }
  }

  async createTemplate(
    input: CreateDeploymentTemplateInput,
  ): Promise<DeploymentTemplate> {
    return apiRequest<DeploymentTemplate>("/api/v1/provisioning/templates", {
      method: "POST",
      body: JSON.stringify(input),
    });
  }

  async updateTemplate(
    id: string,
    input: UpdateDeploymentTemplateInput,
  ): Promise<DeploymentTemplate> {
    return apiRequest<DeploymentTemplate>(
      `/api/v1/provisioning/templates/${encodeURIComponent(id)}`,
      { method: "PATCH", body: JSON.stringify(input) },
    );
  }

  async deleteTemplate(id: string): Promise<void> {
    await apiRequest<void>(
      `/api/v1/provisioning/templates/${encodeURIComponent(id)}`,
      {
        method: "DELETE",
      },
    );
  }

  async replaceTemplateUserData(id: string, userData: string): Promise<void> {
    await apiRequest<void>(
      `/api/v1/provisioning/templates/${encodeURIComponent(id)}/user-data`,
      { method: "PUT", body: JSON.stringify({ userData }) },
    );
  }

  async clearTemplateUserData(id: string): Promise<void> {
    await apiRequest<void>(
      `/api/v1/provisioning/templates/${encodeURIComponent(id)}/user-data`,
      { method: "DELETE" },
    );
  }

  async preflightDeploymentTargets(
    serverIds: string[],
  ): Promise<DeploymentTargetPreflightResult> {
    return apiRequest<DeploymentTargetPreflightResult>(
      "/api/v1/provisioning/deployments/preflight",
      {
        method: "POST",
        body: JSON.stringify({ serverIds }),
      },
    );
  }

  async inspectDeploymentNetworks(
    serverIds: string[],
  ): Promise<NetworkInspectionResult> {
    return apiRequest<NetworkInspectionResult>(
      "/api/v1/provisioning/networks/inspect",
      {
        method: "POST",
        body: JSON.stringify({ serverIds }),
      },
    );
  }

  async deployServers(input: DeployServersInput): Promise<DeployServersResult> {
    return apiRequest<DeployServersResult>("/api/v1/provisioning/deployments", {
      method: "POST",
      body: JSON.stringify(input),
    });
  }

  async createDeploymentOperation(
    input: DeployServersInput,
  ): Promise<ProvisioningOperationReference> {
    return apiRequest<ProvisioningOperationReference>(
      "/api/v1/provisioning/deployment-operations",
      { method: "POST", body: JSON.stringify(input) },
    );
  }

  async createReleaseOperation(
    input: ReleaseServersOperationInput,
  ): Promise<ProvisioningOperationReference> {
    return apiRequest<ProvisioningOperationReference>(
      "/api/v1/provisioning/release-operations",
      { method: "POST", body: JSON.stringify(input) },
    );
  }

  async createRecoverOperation(
    input: RecoverServersOperationInput,
  ): Promise<ProvisioningOperationReference> {
    return apiRequest<ProvisioningOperationReference>(
      "/api/v1/provisioning/recover-operations",
      { method: "POST", body: JSON.stringify(input) },
    );
  }

  async createImageVerification(
    input: CreateImageVerificationInput,
  ): Promise<ProvisioningOperationReference> {
    return apiRequest<ProvisioningOperationReference>(
      "/api/v1/provisioning/image-verifications",
      { method: "POST", body: JSON.stringify(input) },
    );
  }

  async listOSImages(integrationId: string): Promise<OSImage[]> {
    const query = new URLSearchParams({ integrationId });
    return apiRequest<OSImage[]>(`/api/v1/provisioning/images?${query}`);
  }

  async uploadOSImage(
    integrationId: string,
    input: UploadOSImageInput,
    onProgress?: (progress: UploadProgress) => void,
  ): Promise<OSImage> {
    // multipart/form-data: the browser sets the Content-Type boundary, and the file streams as
    // the `content` part. Optional fields are omitted rather than sent blank so the backend
    // applies provider defaults.
    const form = new FormData();
    form.set("integrationId", integrationId);
    form.set("name", input.name);
    form.set("architecture", input.architecture);
    if (input.title) form.set("title", input.title);
    if (input.filetype) form.set("filetype", input.filetype);
    if (input.defaultUser) form.set("defaultUser", input.defaultUser);
    form.set("content", input.file);
    return apiUpload<OSImage>("/api/v1/provisioning/images", form, {
      onProgress,
    });
  }

  async deleteOSImage(
    integrationId: string,
    imageId: string,
    architecture: string,
  ): Promise<void> {
    const query = new URLSearchParams({ integrationId, imageId, architecture });
    await apiRequest<void>(`/api/v1/provisioning/images?${query}`, {
      method: "DELETE",
    });
  }

  async setOSImageOverlay(
    integrationId: string,
    imageId: string,
    architecture: string,
    overlay: OSImageOverlayInput,
  ): Promise<void> {
    const query = new URLSearchParams({ integrationId, imageId, architecture });
    await apiRequest<void>(`/api/v1/provisioning/images/overlay?${query}`, {
      method: "PATCH",
      body: JSON.stringify(overlay),
    });
  }

  async clearOSImageOverlay(
    integrationId: string,
    imageId: string,
    architecture: string,
  ): Promise<void> {
    const query = new URLSearchParams({ integrationId, imageId, architecture });
    await apiRequest<void>(`/api/v1/provisioning/images/overlay?${query}`, {
      method: "DELETE",
    });
  }

  async listServerTags(siteId: string): Promise<ServerTagOption[]> {
    const query = new URLSearchParams({ siteId });
    // The endpoint wraps the list in a `tags` envelope; unwrap to the port's flat array.
    const response = await apiRequest<{ tags: ServerTagOption[] }>(
      `/api/v1/provisioning/tags?${query}`,
    );
    return response.tags ?? [];
  }

  async editServerTags(
    input: EditServerTagsInput,
  ): Promise<ServerTagsResult[]> {
    // The endpoint wraps the per-Server results in a `servers` envelope; unwrap to the port's array.
    const response = await apiRequest<{ servers: ServerTagsResult[] }>(
      "/api/v1/provisioning/tags",
      { method: "POST", body: JSON.stringify(input) },
    );
    return response.servers ?? [];
  }

  async listBootISOs(filters?: {
    siteId?: string;
    integrationId?: string;
  }): Promise<BootISOCatalog> {
    const query = new URLSearchParams();
    if (filters?.siteId) query.set("siteId", filters.siteId);
    if (filters?.integrationId)
      query.set("integrationId", filters.integrationId);
    const suffix = query.toString() ? `?${query}` : "";
    // Never cached: `inUseBy` changes whenever a Server's Boot Media is enabled or switched.
    const response = await apiRequest<BootISOCatalog>(
      `/api/v1/provisioning/boot-isos${suffix}`,
      { cache: "no-store" },
    );
    return {
      builder: response.builder ?? { available: false },
      items: response.items ?? [],
    };
  }

  async createBootISO(input: CreateBootISOInput): Promise<BootISO> {
    return apiRequest<BootISO>("/api/v1/provisioning/boot-isos", {
      method: "POST",
      body: JSON.stringify(input),
    });
  }

  async deleteBootISO(id: string): Promise<void> {
    await apiRequest<void>(
      `/api/v1/provisioning/boot-isos/${encodeURIComponent(id)}`,
      { method: "DELETE" },
    );
  }

  async getHostEnrollmentBundle(
    integrationId: string,
    swallowUrl: string,
  ): Promise<HostEnrollmentBundle> {
    // The response carries the provisioner's credential (contract server-enrollment.md), so it is
    // fetched with no-store and returned straight to the caller; nothing here keeps or logs it.
    return apiRequest<HostEnrollmentBundle>(
      `/api/v1/provisioning/integrations/${encodeURIComponent(integrationId)}/enroll-bundle`,
      { method: "POST", cache: "no-store", body: JSON.stringify({ swallowUrl }) },
    );
  }

  async enrollVirtualMachines(
    input: VirtualMachineEnrollmentInput,
  ): Promise<VirtualMachineEnrollmentAccepted> {
    // Optional fields are sent only when chosen, so the contract's defaults (the Server Default
    // User, no Boot ISO, ask before stopping a running domain) apply otherwise.
    const body: VirtualMachineEnrollmentInput = {
      integrationId: input.integrationId,
      hypervisorServerId: input.hypervisorServerId,
      domains: input.domains,
    };
    if (input.bootIsoId) body.bootIsoId = input.bootIsoId;
    if (input.account?.trim()) body.account = input.account.trim();
    if (input.powerOffRunning) body.powerOffRunning = true;
    return apiRequest<VirtualMachineEnrollmentAccepted>(
      "/api/v1/provisioning/virtual-machine-enrollments",
      { method: "POST", body: JSON.stringify(body) },
    );
  }
}
