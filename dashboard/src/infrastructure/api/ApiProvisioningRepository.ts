import type {
  OSImageOverlayInput,
  ProvisioningRepository,
  UploadOSImageInput,
} from "@/application/ports/ProvisioningRepository";
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
}
