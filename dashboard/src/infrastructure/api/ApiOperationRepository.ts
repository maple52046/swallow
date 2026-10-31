import type {
  OperationRepository,
  Paginated,
} from "@/application/ports/OperationRepository";
import type {
  ListOperationsFilters,
  Operation,
  OperationEvents,
  OperationArtifact,
  OperationTimelineEvent,
} from "@/domain/operation/types";
import { ApiRequestError, apiRequest, apiRequestText } from "./client";

/**
 * Converts compatibility payloads into the non-null collection contract used by the
 * Operation domain. Mongo records created before collection normalization may still
 * encode nil slices as null, so this adapter boundary must not leak them into React.
 */
function normalizeOperation(operation: Operation): Operation {
  return {
    ...operation,
    targetServerIds: operation.targetServerIds ?? [],
    targetResources: operation.targetResources ?? [],
    leases: operation.leases ?? [],
    steps: (operation.steps ?? []).map((step) => ({
      ...step,
      dependsOn: step.dependsOn ?? [],
      targets: step.targets ?? [],
      artifacts: step.artifacts ?? [],
    })),
  };
}

/**
 * The Workflow contract maps directly onto the domain type. Task logs are plain text, so they
 * go through the text helper; everything else is JSON. getOperation treats 404 as a stale
 * link (null) rather than an error to surface.
 */
export class ApiOperationRepository implements OperationRepository {
  async listOperations(
    filters?: ListOperationsFilters,
  ): Promise<Paginated<Operation>> {
    const query = new URLSearchParams();
    if (filters?.siteId) query.set("siteId", filters.siteId);
    if (filters?.platformId) query.set("platformId", filters.platformId);
    if (filters?.serverId) query.set("serverId", filters.serverId);
    if (filters?.kind) query.set("kind", filters.kind);
    if (filters?.status) query.set("status", filters.status);
    if (filters?.active) query.set("active", "true");
    if (filters?.page) query.set("page", String(filters.page));
    if (filters?.pageSize) query.set("pageSize", String(filters.pageSize));

    const suffix = query.toString() ? `?${query.toString()}` : "";
    const page = await apiRequest<Paginated<Operation>>(
      `/api/v1/workflows${suffix}`,
    );
    return { ...page, items: (page.items ?? []).map(normalizeOperation) };
  }

  async getOperation(id: string): Promise<Operation | null> {
    try {
      const operation = await apiRequest<Operation>(
        `/api/v1/workflows/${encodeURIComponent(id)}`,
      );
      return normalizeOperation(operation);
    } catch (error) {
      if (error instanceof ApiRequestError && error.status === 404) {
        return null;
      }
      throw error;
    }
  }

  async rerunOperation(id: string): Promise<Operation> {
    // The rerun response is a freshly created Operation projection, so normalize it like every
    // other payload.
    const operation = await apiRequest<Operation>(
      `/api/v1/workflows/${encodeURIComponent(id)}/rerun`,
      {
        method: "POST",
      },
    );
    return normalizeOperation(operation);
  }

  async getTimeline(id: string): Promise<OperationTimelineEvent[]> {
    const events = await apiRequest<OperationTimelineEvent[] | null>(
      `/api/v1/workflows/${encodeURIComponent(id)}/timeline`,
    );
    return events ?? [];
  }

  async cancelOperation(id: string): Promise<void> {
    await apiRequest(`/api/v1/workflows/${encodeURIComponent(id)}/cancel`, {
      method: "POST",
    });
  }

  async retryStep(id: string, stepId: string): Promise<void> {
    await apiRequest(
      `/api/v1/workflows/${encodeURIComponent(id)}/tasks/${encodeURIComponent(stepId)}/retry`,
      { method: "POST" },
    );
  }

  async getStepLogs(id: string, stepId: string): Promise<string> {
    return apiRequestText(
      `/api/v1/workflows/${encodeURIComponent(id)}/tasks/${encodeURIComponent(stepId)}/logs`,
    );
  }

  async getStepStderr(id: string, stepId: string): Promise<string> {
    return apiRequestText(
      `/api/v1/workflows/${encodeURIComponent(id)}/tasks/${encodeURIComponent(stepId)}/stderr`,
    );
  }

  async getStepEvents(id: string, stepId: string): Promise<OperationEvents> {
    return apiRequest<OperationEvents>(
      `/api/v1/workflows/${encodeURIComponent(id)}/tasks/${encodeURIComponent(stepId)}/events`,
    );
  }

  async getStepArtifacts(
    id: string,
    stepId: string,
  ): Promise<OperationArtifact[]> {
    const artifacts = await apiRequest<OperationArtifact[] | null>(
      `/api/v1/workflows/${encodeURIComponent(id)}/tasks/${encodeURIComponent(stepId)}/artifacts`,
    );
    return artifacts ?? [];
  }
}
