import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Alert,
  AlertVariant,
  Button,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  Flex,
  Label,
  LabelGroup,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Tab,
  Tabs,
  TabTitleText,
  Tooltip,
} from "@patternfly/react-core";
import { BanIcon, RedoIcon, SyncAltIcon } from "@patternfly/react-icons";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { useApp } from "@/di/AppProvider";
import {
  isTerminalStatus,
  operationStatus,
  type Operation,
  type OperationArtifact,
  type OperationEvents,
  type OperationStep,
  type OperationTimelineEvent,
} from "@/domain/operation/types";
import { EmptyState } from "@/presentation/components/EmptyState";
import {
  KeyValueGrid,
  SectionHeader,
  StickyTableFrame,
} from "@/presentation/components/OperatorPrimitives";
import { PageHeader } from "@/presentation/components/PageHeader";
import { StatusBadge } from "@/presentation/components/StatusBadge";
import { useToast } from "@/presentation/components/toast/toastContext";
import { useSiteScope } from "@/presentation/contexts/SiteScopeContext";
import { useTargetLockProtection } from "@/presentation/hooks/useTargetLockProtection";
import { formatDateTime } from "@/shared/utils/time";
import { OperationEventWorkspace } from "./OperationEventWorkspace";
import { OperationLogWorkspace } from "./OperationLogWorkspace";

interface DurableOperationDetailProps {
  operation: Operation;
  reload: () => void;
}

/** Unified, Step-first debugger for schema-v3 Operations. */
export function DurableOperationDetail({
  operation,
  reload,
}: DurableOperationDetailProps) {
  const { operations } = useApp();
  const { scopedHref } = useSiteScope();
  const { showToast } = useToast();
  const status = operationStatus(operation);
  const steps = useMemo(() => operation.steps ?? [], [operation.steps]);
  const targetProtection = useTargetLockProtection(operation.targetServerIds);
  const retryDisabledReason = targetProtection.checking
    ? "Checking target protection."
    : targetProtection.error
      ? targetProtection.error
      : targetProtection.lockedNames.length > 0
        ? `${targetProtection.lockedNames.join(", ")} ${targetProtection.lockedNames.length === 1 ? "is" : "are"} locked. Unlock ${targetProtection.lockedNames.length === 1 ? "it" : "them"} before retrying this Step.`
        : undefined;
  const preferredStep =
    steps.find((step) =>
      ["running", "waiting_external", "requires_attention", "failed"].includes(
        step.status,
      ),
    ) ?? steps[0];
  const [selectedStepId, setSelectedStepId] = useState(preferredStep?.id ?? "");
  const [tab, setTab] = useState<string | number>("stdout");
  const [cancelOpen, setCancelOpen] = useState(false);
  const [retryCandidate, setRetryCandidate] = useState<OperationStep | null>(null);
  const [controlling, setControlling] = useState(false);
  const selectedStep =
    steps.find((step) => step.id === selectedStepId) ?? preferredStep;

  useEffect(() => {
    if (!steps.some((step) => step.id === selectedStepId))
      setSelectedStepId(preferredStep?.id ?? "");
  }, [preferredStep?.id, selectedStepId, steps]);

  const cancel = useCallback(async () => {
    setControlling(true);
    try {
      await operations.cancelOperation(operation.id);
      setCancelOpen(false);
      showToast({
        tone: "success",
        title: "Cancellation requested",
        description:
          "Running provider work will be canceled when it is safe to do so.",
      });
      reload();
    } catch (error) {
      showToast({
        tone: "error",
        title: "Could not cancel Operation",
        description: error instanceof Error ? error.message : "Unknown error",
      });
    } finally {
      setControlling(false);
    }
  }, [operation.id, operations, reload, showToast]);

  const retryStep = useCallback(
    async (step: OperationStep) => {
      setControlling(true);
      try {
        await operations.retryStep(operation.id, step.id);
        showToast({
          tone: "success",
          title: "Step retry requested",
          description: `${step.name} will continue as attempt ${step.attempt + 1}.`,
        });
        setRetryCandidate(null);
        reload();
      } catch (error) {
        showToast({
          tone: "error",
          title: "Could not retry Step",
          description: error instanceof Error ? error.message : "Unknown error",
        });
      } finally {
        setControlling(false);
      }
    },
    [operation.id, operations, reload, showToast],
  );

  const details = [
    {
      label: "Operation ID",
      value: <span className="mono">{operation.id}</span>,
    },
    {
      label: "Definition",
      value: `${operation.definition ?? "-"} v${operation.definitionVersion ?? "-"}`,
    },
    {
      label: "Workflow ID",
      value: (
        <span className="mono">{operation.temporal?.workflowId ?? "-"}</span>
      ),
    },
    {
      label: "Run ID",
      value: <span className="mono">{operation.temporal?.runId ?? "-"}</span>,
    },
    { label: "Requested by", value: operation.requestedBy || "system" },
    {
      label: "Request correlation",
      value: (
        <span className="mono">{operation.requestCorrelation ?? "-"}</span>
      ),
    },
    { label: "Targets", value: `${operation.targetServerIds.length} Servers` },
    {
      label: "Resource leases",
      value: operation.leases?.length ? (
        <LabelGroup aria-label="Active resource leases">
          {operation.leases.map((lease) => (
            <Label key={lease.resourceKey} variant="outline">
              {lease.resourceKey} · fence {lease.fencingToken}
            </Label>
          ))}
        </LabelGroup>
      ) : "No active leases",
    },
    { label: "Updated", value: formatDateTime(operation.updatedAt) },
  ];

  return (
    <div className="operator-page">
      <PageHeader
        title={operation.intent || operation.kind}
        breadcrumbs={[
          { label: "Workflows", href: scopedHref("/workflows") },
          { label: operation.id },
        ]}
        subtitle={`${operation.kind} - ${operation.definition ?? "durable workflow"}`}
        metadata={
          <Flex
            gap={{ default: "gapSm" }}
            alignItems={{ default: "alignItemsCenter" }}
          >
            <StatusBadge status={status} />
            {operation.statusReason && <span>{operation.statusReason}</span>}
          </Flex>
        }
        actions={
          <>
            <Button variant="secondary" icon={<SyncAltIcon />} onClick={reload}>
              Refresh
            </Button>
            {!isTerminalStatus(status) && (
              <Button
                variant="danger"
                icon={<BanIcon />}
                onClick={() => setCancelOpen(true)}
              >
                Cancel
              </Button>
            )}
          </>
        }
      />

      <section className="sw-section">
        <SectionHeader
          title="Operation Steps"
          description="Each provider or executor phase is persisted and retried independently."
        />
        <StickyTableFrame>
          <Table
            aria-label="Operation Steps"
            variant="compact"
            className="sw-operation-steps-table"
          >
            <Thead>
              <Tr>
                <Th>Step</Th>
                <Th>Targets</Th>
                <Th>Executor</Th>
                <Th>Status</Th>
                <Th>Attempt</Th>
                <Th>Duration</Th>
                <Th>Reason</Th>
                <Th screenReaderText="Actions" />
              </Tr>
            </Thead>
            <Tbody>
              {steps.map((step) => {
                const retryable =
                  (step.status === "failed" ||
                    step.status === "requires_attention") &&
                  step.error?.retryable;
                return (
                  <Tr
                    key={step.id}
                    isClickable
                    isRowSelected={selectedStep?.id === step.id}
                  >
                    <Td dataLabel="Step">
                      <Button
                        variant="link"
                        isInline
                        onClick={() => {
                          setSelectedStepId(step.id);
                          setTab("stdout");
                        }}
                      >
                        {step.name}
                      </Button>
                      <small className="sw-block-subtle mono">
                        {step.kind}
                      </small>
                    </Td>
                    <Td dataLabel="Targets">{formatTargets(step)}</Td>
                    <Td dataLabel="Executor">
                      <Label variant="outline">{step.executor}</Label>
                    </Td>
                    <Td dataLabel="Status">
                      <StatusBadge status={step.status} />
                    </Td>
                    <Td dataLabel="Attempt">{step.attempt}</Td>
                    <Td dataLabel="Duration">
                      {formatDuration(step.startedAt, step.finishedAt)}
                    </Td>
                    <Td dataLabel="Reason">
                      {step.error?.message ?? step.waitingReason ?? "-"}
                    </Td>
                    <Td isActionCell>
                      {retryable && (
                        <Tooltip
                          content={
                            retryDisabledReason ??
                            (step.kind === "provision-os"
                              ? "Retry verification; a target with no provider address will be released and redeployed"
                              : "Retry this failed Step without repeating completed Steps")
                          }
                        >
                          <Button
                            variant="plain"
                            icon={<RedoIcon />}
                            aria-label={retryDisabledReason
                              ? `Retry: ${retryDisabledReason}`
                              : `Retry ${step.name}`}
                            isDisabled={controlling || Boolean(retryDisabledReason)}
                            onClick={() => {
                              if (step.kind === "provision-os")
                                setRetryCandidate(step);
                              else void retryStep(step);
                            }}
                          />
                        </Tooltip>
                      )}
                    </Td>
                  </Tr>
                );
              })}
            </Tbody>
          </Table>
        </StickyTableFrame>
        {steps.length === 0 && (
          <EmptyState
            title="No Steps"
            message="This durable Operation has no projected Steps."
          />
        )}
      </section>

      {selectedStep && (
        <section className="sw-section sw-operation-debugger">
          <SectionHeader
            title={selectedStep.name}
            description={`Attempt ${selectedStep.attempt} - ${selectedStep.executor}`}
          />
          {selectedStep.error && (
            <Alert
              variant={
                selectedStep.status === "requires_attention"
                  ? AlertVariant.warning
                  : AlertVariant.danger
              }
              title={selectedStep.error.code}
              isInline
            >
              {selectedStep.error.message}
              {selectedStep.error.stage
                ? ` Stage: ${selectedStep.error.stage}.`
                : ""}
            </Alert>
          )}
          {selectedStep.waitingReason && (
            <Alert variant={AlertVariant.info} title="Waiting" isInline>
              {selectedStep.waitingReason}
            </Alert>
          )}
          <Tabs
            activeKey={tab}
            onSelect={(_event, key) => setTab(key)}
            aria-label="Step diagnostics"
          >
            <Tab eventKey="stdout" title={<TabTitleText>Stdout</TabTitleText>}>
              <div className="sw-tab-content">
                <OperationLogWorkspace
                  operationId={operation.id}
                  stepId={selectedStep.id}
                />
              </div>
            </Tab>
            <Tab eventKey="events" title={<TabTitleText>Events</TabTitleText>}>
              <div className="sw-tab-content">
                <StepEvents operation={operation} step={selectedStep} />
              </div>
            </Tab>
            <Tab
              eventKey="artifacts"
              title={<TabTitleText>Artifacts</TabTitleText>}
            >
              <div className="sw-tab-content">
                <StepArtifacts operationId={operation.id} step={selectedStep} />
              </div>
            </Tab>
            <Tab
              eventKey="details"
              title={<TabTitleText>Details</TabTitleText>}
            >
              <div className="sw-tab-content">
                <StepDetails step={selectedStep} />
              </div>
            </Tab>
          </Tabs>
        </section>
      )}

      <section className="sw-section">
        <SectionHeader
          title="Timeline"
          description="Normalized workflow events retained independently from provider diagnostics."
        />
        <OperationTimeline operation={operation} />
      </section>
      <section className="sw-section">
        <SectionHeader title="Operation details" />
        <KeyValueGrid items={details} />
      </section>

      <Modal
        isOpen={retryCandidate !== null}
        onClose={() => !controlling && setRetryCandidate(null)}
        variant="small"
        aria-labelledby="retry-os-deployment-title"
      >
        <ModalHeader
          title="Retry failed OS deployment"
          labelId="retry-os-deployment-title"
          description="Recheck a failed operating system deployment and recover it when necessary."
        />
        <ModalBody>
          <Alert
            variant={AlertVariant.warning}
            title="This retry may redeploy the Server"
            isInline
          >
            Swallow first rechecks the installed image, provider address, and SSH.
            If MAAS still reports no address, Swallow will release the unusable
            installation, wait for Ready, then redeploy the same image, network
            settings, and protected cloud-init data. An SSH-only failure is not redeployed.
          </Alert>
          <p>
            Other successful targets and completed Steps are preserved. No
            release occurs until you confirm this retry.
          </p>
        </ModalBody>
        <ModalFooter>
          <Button
            variant="danger"
            isLoading={controlling}
            onClick={() => retryCandidate && void retryStep(retryCandidate)}
          >
            Retry deployment
          </Button>
          <Button
            variant="link"
            isDisabled={controlling}
            onClick={() => setRetryCandidate(null)}
          >
            Cancel
          </Button>
        </ModalFooter>
      </Modal>

      <Modal
        isOpen={cancelOpen}
        onClose={() => !controlling && setCancelOpen(false)}
        variant="small"
        aria-labelledby="cancel-operation-title"
      >
        <ModalHeader
          title="Cancel Operation"
          labelId="cancel-operation-title"
          description="Completed side effects are preserved. Swallow will stop work that has not started and ask active providers to cancel when supported."
        />
        <ModalBody>
          This does not automatically release an installed OS or uninstall a
          Platform.
        </ModalBody>
        <ModalFooter>
          <Button
            variant="danger"
            isLoading={controlling}
            onClick={() => void cancel()}
          >
            Cancel Operation
          </Button>
          <Button
            variant="link"
            isDisabled={controlling}
            onClick={() => setCancelOpen(false)}
          >
            Keep running
          </Button>
        </ModalFooter>
      </Modal>
    </div>
  );
}

function formatTargets(step: OperationStep): string {
  const targets = step.targets ?? [];
  if (targets.length === 0) return "-";
  const visible = targets.slice(0, 2).map((target) => target.id);
  return `${visible.join(", ")}${targets.length > visible.length ? ` +${targets.length - visible.length}` : ""}`;
}

function formatDuration(start: string | null, finish: string | null): string {
  if (!start) return "-";
  const milliseconds = Math.max(
    0,
    new Date(finish ?? Date.now()).getTime() - new Date(start).getTime(),
  );
  if (milliseconds < 1000) return "<1s";
  const seconds = Math.floor(milliseconds / 1000);
  return seconds < 60
    ? `${seconds}s`
    : `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
}

function StepEvents({
  operation,
  step,
}: {
  operation: Operation;
  step: OperationStep;
}) {
  const { operations } = useApp();
  const [events, setEvents] = useState<OperationEvents | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let canceled = false;
    operations
      .getStepEvents(operation.id, step.id)
      .then((result) => {
        if (!canceled) setEvents(result);
      })
      .catch((caught: Error) => {
        if (!canceled) setError(caught.message);
      });
    return () => {
      canceled = true;
    };
  }, [operation.id, operation.updatedAt, operations, step.id]);
  if (error)
    return (
      <Alert variant={AlertVariant.warning} title="Events unavailable" isInline>
        {error}
      </Alert>
    );
  return (
    <OperationEventWorkspace
      events={events}
      running={
        !["succeeded", "failed", "canceled", "skipped"].includes(step.status)
      }
    />
  );
}

function StepArtifacts({
  operationId,
  step,
}: {
  operationId: string;
  step: OperationStep;
}) {
  const { operations } = useApp();
  const [artifacts, setArtifacts] = useState<OperationArtifact[]>(
    step.artifacts ?? [],
  );
  useEffect(() => {
    let canceled = false;
    operations
      .getStepArtifacts(operationId, step.id)
      .then((items) => {
        if (!canceled) setArtifacts(items ?? []);
      })
      .catch(() => undefined);
    return () => {
      canceled = true;
    };
  }, [operationId, operations, step.id, step.artifacts]);
  if (artifacts.length === 0)
    return (
      <EmptyState
        title="No artifacts"
        message="This Step has no retained artifact metadata."
      />
    );
  return (
    <DescriptionList>
      {artifacts.map((artifact) => (
        <DescriptionListGroup key={artifact.id}>
          <DescriptionListTerm>{artifact.name}</DescriptionListTerm>
          <DescriptionListDescription>
            {artifact.mediaType} - {artifact.sizeBytes.toLocaleString()} bytes -{" "}
            {formatDateTime(artifact.createdAt)}
          </DescriptionListDescription>
        </DescriptionListGroup>
      ))}
    </DescriptionList>
  );
}

function StepDetails({ step }: { step: OperationStep }) {
  return (
    <KeyValueGrid
      items={[
        { label: "Step ID", value: <span className="mono">{step.id}</span> },
        { label: "Dependencies", value: step.dependsOn?.join(", ") || "-" },
        { label: "Progress", value: `${step.progress}%` },
        {
          label: "External provider",
          value: step.externalExecution?.provider ?? "-",
        },
        {
          label: "External execution",
          value: (
            <span className="mono">{step.externalExecution?.id ?? "-"}</span>
          ),
        },
        {
          label: "Started",
          value: formatDateTime(step.startedAt ?? undefined),
        },
        {
          label: "Finished",
          value: formatDateTime(step.finishedAt ?? undefined),
        },
        {
          label: "Retryable",
          value: step.error ? (step.error.retryable ? "Yes" : "No") : "-",
        },
      ]}
    />
  );
}

function OperationTimeline({ operation }: { operation: Operation }) {
  const { operations } = useApp();
  const [events, setEvents] = useState<OperationTimelineEvent[]>([]);
  useEffect(() => {
    let canceled = false;
    operations
      .getTimeline(operation.id)
      .then((items) => {
        if (!canceled) setEvents(items);
      })
      .catch(() => undefined);
    return () => {
      canceled = true;
    };
  }, [operation.id, operation.updatedAt, operations]);
  const stepNames = useMemo(
    () => new Map((operation.steps ?? []).map((step) => [step.id, step.name])),
    [operation.steps],
  );
  if (events.length === 0) return <EmptyState title="No timeline events" />;
  return (
    <ol className="sw-operation-event-timeline">
      {events.map((event) => (
        <li key={event.id}>
          <time>{formatDateTime(event.createdAt)}</time>
          <div>
            <strong>
              {event.stepId
                ? (stepNames.get(event.stepId) ?? event.stepId)
                : event.type}
            </strong>
            <span>{event.message}</span>
          </div>
        </li>
      ))}
    </ol>
  );
}
