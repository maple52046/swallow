import type {
  ListOperationsFilters,
  Operation,
  OperationEvents,
  OperationArtifact,
  OperationTimelineEvent,
} from "@/domain/operation/types";

/** A page of results, matching the backend's pagination envelope. */
export interface Paginated<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
}

/**
 * Reads Workflows (Operations), their timeline, and their Tasks' diagnostics, and controls them.
 * Operations are created by the actions that need them (a platform deployment, for example), so
 * there is no generic create here.
 */
export interface OperationRepository {
  listOperations(
    filters?: ListOperationsFilters,
  ): Promise<Paginated<Operation>>;
  getOperation(id: string): Promise<Operation | null>;
  /**
   * Converge a durable orchestration Operation's intent again by launching a new Operation for
   * the same intent on the same Platform, linked back to the original. Unlike retryStep, which
   * retries one Task in place, rerun starts over from the current state; it is the recovery path
   * for a finished (failed/canceled) Operation that does not require deleting the Platform. A
   * parked original is canceled by the same request. Resolves to the new Operation.
   */
  rerunOperation(id: string): Promise<Operation>;
  getTimeline(id: string): Promise<OperationTimelineEvent[]>;
  cancelOperation(id: string): Promise<void>;
  retryStep(id: string, stepId: string): Promise<void>;
  getStepLogs(id: string, stepId: string): Promise<string>;
  getStepStderr(id: string, stepId: string): Promise<string>;
  getStepEvents(id: string, stepId: string): Promise<OperationEvents>;
  getStepArtifacts(id: string, stepId: string): Promise<OperationArtifact[]>;
}
