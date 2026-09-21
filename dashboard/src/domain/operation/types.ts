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

/**
 * Live, coarse progress of an in-flight Ansible Step: the play/task the runner is currently on and
 * cumulative host-result counts. Present only while a run is in flight and absent otherwise; it is
 * advisory (the authoritative outcome is `status`/`error`) and carries no task output. There is no
 * percentage because Ansible's total task count is not known up front. `changed` is a subset of `ok`.
 */
export interface OperationStepLive {
  currentPlay?: string;
  currentTask?: string;
  total: number;
  ok: number;
  changed: number;
  failed: number;
  unreachable: number;
  skipped: number;
  updatedAt: string;
}

/** One durable and independently observable phase of an Operation. */
export interface OperationStep {
  id: string;
  kind: string;
  name: string;
  /**
   * The Job this Step belongs to (ADR 017), e.g. "ensure-os" or "configure-k0s". Empty for a
   * legacy/flat operation that has no Jobs. The detail view groups Steps by this value.
   */
  job?: string;
  executor: "internal" | "ansible" | "maas" | string;
  dependsOn: string[];
  targets: OperationResourceReference[];
  status: OperationStepStatus;
  attempt: number;
  progress: number;
  waitingReason?: string;
  error: OperationNormalizedError | null;
  externalExecution: OperationExternalExecution | null;
  /** Live progress while the Step runs; null/absent before it starts and after it finishes. */
  live?: OperationStepLive | null;
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

/**
 * A run's task-level progress, derived from the runner's own event stream. For an Ansible Step the
 * events accumulate live during the run, so polling this shows progress rather than waiting for the
 * whole run to finish.
 */
export interface OperationEvents {
  runId: string;
  status: string;
  okCount: number;
  changedCount: number;
  failedCount: number;
  unreachableCount: number;
  skippedCount: number;
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

/**
 * Whether a workflow is actively making progress, as opposed to terminal or parked awaiting an
 * operator (`requires_attention`) or tearing down (`canceling`). Use this — not merely "not
 * terminal" — to decide when to show an in-progress affordance such as the OS Images "verifying"
 * spinner: a parked or canceling workflow is not progressing and must not read as still working.
 */
export function isInFlightStatus(status: OperationStatus): boolean {
  return (
    status === "pending" ||
    status === "running" ||
    status === "waiting_external" ||
    status === "waiting_dependency"
  );
}
