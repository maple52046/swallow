/** Operator-visible lifecycle of a legacy run or durable workflow. */
export type OperationStatus =
  | "pending"
  | "running"
  | "waiting_external"
  | "waiting_dependency"
  | "canceling"
  | "succeeded"
  | "failed"
  | "partially_succeeded"
  | "canceled"
  | "requires_attention"
  | "indeterminate";

export type OperationStepStatus =
  | Exclude<
      OperationStatus,
      "canceling" | "partially_succeeded" | "indeterminate"
    >
  | "skipped";

/** The legacy execution projection remains present on every API response for compatibility. */
export interface OperationExecution {
  runId: string;
  playbook: string;
  status: OperationStatus;
  statusReason: string | null;
  startedAt: string | null;
  finishedAt: string | null;
}

export interface OperationResourceReference {
  kind: string;
  id: string;
}

export interface OperationNormalizedError {
  code: string;
  message: string;
  retryable: boolean;
  stage?: string;
}

export interface OperationExternalExecution {
  provider: string;
  id: string;
  generation: number;
}

export interface OperationArtifact {
  id: string;
  name: string;
  mediaType: string;
  sizeBytes: number;
  createdAt: string;
}

/** One durable and independently observable phase of an Operation. */
export interface OperationStep {
  id: string;
  kind: string;
  name: string;
  executor: "internal" | "ansible" | "maas" | string;
  dependsOn: string[];
  targets: OperationResourceReference[];
  status: OperationStepStatus;
  attempt: number;
  progress: number;
  waitingReason?: string;
  error: OperationNormalizedError | null;
  externalExecution: OperationExternalExecution | null;
  artifacts: OperationArtifact[];
  startedAt: string | null;
  finishedAt: string | null;
}

export interface OperationResourceLease {
  resourceKey: string;
  owner: string;
  fencingToken: number;
  expiresAt: string;
  updatedAt: string;
}

export interface OperationTimelineEvent {
  id: string;
  operationId: string;
  stepId?: string;
  type: string;
  message: string;
  details?: Record<string, unknown>;
  createdAt: string;
}

/**
 * An Operation is the durable record of an operator intent. Schema v3 exposes its Step
 * workflow while legacy records continue to expose only the compatibility execution.
 */
export interface Operation {
  id: string;
  schemaVersion?: number;
  kind: string;
  intent: string;
  intentSnapshot?: Record<string, unknown>;
  definition?: string;
  definitionVersion?: number;
  status?: OperationStatus;
  statusReason?: string | null;
  startState?: string;
  temporal?: { workflowId: string; runId?: string };
  siteId: string;
  platformId: string | null;
  targetResources?: OperationResourceReference[];
  targetServerIds: string[];
  steps?: OperationStep[];
  leases?: OperationResourceLease[];
  retryOfOperationId: string | null;
  requestCorrelation?: string | null;
  execution: OperationExecution;
  requestedBy: string;
  requestedAt: string;
  startedAt?: string | null;
  finishedAt?: string | null;
  updatedAt: string;
}

/** One task result on one host. Carries no task output, which could contain a secret. */
export interface TaskEvent {
  play: string;
  task: string;
  host: string;
  status: string;
  changed: boolean;
  startedAt: string | null;
  endedAt: string | null;
}

/** A run's task-level progress, derived from the runner's own event stream. */
export interface OperationEvents {
  runId: string;
  status: string;
  okCount: number;
  changedCount: number;
  failedCount: number;
  events: TaskEvent[];
}

export interface ListOperationsFilters {
  siteId?: string;
  platformId?: string;
  serverId?: string;
  kind?: string;
  status?: OperationStatus;
  active?: boolean;
  page?: number;
  pageSize?: number;
}

export function operationStatus(operation: Operation): OperationStatus {
  return operation.status ?? operation.execution.status;
}

export function isOrchestrationOperation(operation: Operation): boolean {
  return operation.schemaVersion === 3 && Array.isArray(operation.steps);
}

/** Whether a workflow can no longer advance without a new operator command. */
export function isTerminalStatus(status: OperationStatus): boolean {
  return (
    status === "succeeded" ||
    status === "failed" ||
    status === "partially_succeeded" ||
    status === "canceled" ||
    status === "indeterminate"
  );
}
