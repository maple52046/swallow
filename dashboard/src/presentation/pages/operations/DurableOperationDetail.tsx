import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import { Badge, Button, HStack, IconButton, Stack, Table, Tabs, Text, VisuallyHidden } from '@chakra-ui/react'
import { Ban, Check, ChevronDown, ChevronRight, CircleDot, Redo, RefreshCw, TriangleAlert, X, type LucideIcon } from 'lucide-react'
import type { ProviderEvents } from '@/domain/server/types'
import { useApp } from '@/di/AppProvider'
import {
  isTerminalStatus,
  operationStatus,
  type Operation,
  type OperationArtifact,
  type OperationEvents,
  type OperationStep,
  type OperationStepStatus,
  type OperationTimelineEvent,
} from '@/domain/operation/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { KeyValueGrid, SectionHeader, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { PageHeader } from '@/presentation/components/PageHeader'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import { Modal } from '@/presentation/components/ui/modal'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useTargetLockProtection } from '@/presentation/hooks/useTargetLockProtection'
import { formatDateTime } from '@/shared/utils/time'
import { OperationEventWorkspace } from './OperationEventWorkspace'
import { OperationLogWorkspace } from './OperationLogWorkspace'

interface DurableOperationDetailProps {
  operation: Operation
  reload: () => void
}

/** Unified, Step-first debugger for schema-v3 Operations. */
export function DurableOperationDetail({ operation, reload }: DurableOperationDetailProps) {
  const { operations } = useApp()
  const { scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const status = operationStatus(operation)
  const steps = useMemo(() => operation.steps ?? [], [operation.steps])
  const targetProtection = useTargetLockProtection(operation.targetServerIds)
  const retryDisabledReason = targetProtection.checking
    ? 'Checking target protection.'
    : targetProtection.error
      ? targetProtection.error
      : targetProtection.lockedNames.length > 0
        ? `${targetProtection.lockedNames.join(', ')} ${targetProtection.lockedNames.length === 1 ? 'is' : 'are'} locked. Unlock ${targetProtection.lockedNames.length === 1 ? 'it' : 'them'} before retrying this Step.`
        : undefined
  const preferredStep =
    steps.find((step) => ['running', 'waiting_external', 'requires_attention', 'failed'].includes(step.status)) ?? steps[0]
  const [selectedStepId, setSelectedStepId] = useState(preferredStep?.id ?? '')
  // A failed Step opens on its Stderr (the error-only report) so the cause is visible without a
  // click; healthy Steps open on Stdout.
  const [tab, setTab] = useState(preferredStep?.error ? 'stderr' : 'stdout')
  const [cancelOpen, setCancelOpen] = useState(false)
  const [retryCandidate, setRetryCandidate] = useState<OperationStep | null>(null)
  const [controlling, setControlling] = useState(false)
  const selectedStep = steps.find((step) => step.id === selectedStepId) ?? preferredStep

  useEffect(() => {
    if (!steps.some((step) => step.id === selectedStepId)) setSelectedStepId(preferredStep?.id ?? '')
  }, [preferredStep?.id, selectedStepId, steps])

  // Steps are grouped by Job so a deployment reads proportionally: the many provisioning Steps fold
  // under one group while the configure Job stays visible. jobOverrides holds explicit user
  // toggles; a Job with no override uses the smart default (collapse only when every Step is
  // terminal-good), so active or failed work is always shown and a user can still open any Job.
  const groups = useMemo(() => groupStepsByJob(steps), [steps])
  const [jobOverrides, setJobOverrides] = useState<Record<string, boolean>>({})
  const toggleJob = useCallback((job: string, currentlyCollapsed: boolean) => {
    setJobOverrides((previous) => ({ ...previous, [job]: !currentlyCollapsed }))
  }, [])

  const cancel = useCallback(async () => {
    setControlling(true)
    try {
      await operations.cancelOperation(operation.id)
      setCancelOpen(false)
      showToast({ tone: 'success', title: 'Cancellation requested', description: 'Running provider work will be canceled when it is safe to do so.' })
      reload()
    } catch (error) {
      showToast({ tone: 'error', title: 'Could not cancel Operation', description: error instanceof Error ? error.message : 'Unknown error' })
    } finally {
      setControlling(false)
    }
  }, [operation.id, operations, reload, showToast])

  const retryStep = useCallback(
    async (step: OperationStep) => {
      setControlling(true)
      try {
        await operations.retryStep(operation.id, step.id)
        showToast({ tone: 'success', title: 'Step retry requested', description: `${step.name} will continue as attempt ${step.attempt + 1}.` })
        setRetryCandidate(null)
        reload()
      } catch (error) {
        showToast({ tone: 'error', title: 'Could not retry Step', description: error instanceof Error ? error.message : 'Unknown error' })
      } finally {
        setControlling(false)
      }
    },
    [operation.id, operations, reload, showToast],
  )

  const details = [
    { label: 'Operation ID', value: <span className="mono">{operation.id}</span> },
    { label: 'Definition', value: `${operation.definition ?? '-'} v${operation.definitionVersion ?? '-'}` },
    { label: 'Workflow ID', value: <span className="mono">{operation.temporal?.workflowId ?? '-'}</span> },
    { label: 'Run ID', value: <span className="mono">{operation.temporal?.runId ?? '-'}</span> },
    { label: 'Requested by', value: operation.requestedBy || 'system' },
    { label: 'Request correlation', value: <span className="mono">{operation.requestCorrelation ?? '-'}</span> },
    { label: 'Targets', value: `${operation.targetServerIds.length} Servers` },
    {
      label: 'Resource leases',
      value: operation.leases?.length ? (
        <HStack gap="1" wrap="wrap" aria-label="Active resource leases">
          {operation.leases.map((lease) => (
            <Badge key={lease.resourceKey} variant="outline">
              {lease.resourceKey} · fence {lease.fencingToken}
            </Badge>
          ))}
        </HStack>
      ) : (
        'No active leases'
      ),
    },
    { label: 'Updated', value: formatDateTime(operation.updatedAt) },
  ]

  // Renders one Step row. Extracted so the flat (no-Job) and Job-grouped paths share one row, and
  // so a grouped row can be indented under its Job header.
  const renderStepRow = (step: OperationStep, indented: boolean) => {
    const retryable = (step.status === 'failed' || step.status === 'requires_attention') && step.error?.retryable
    return (
      <Table.Row key={step.id} bg={selectedStep?.id === step.id ? 'bg.subtle' : undefined}>
        <Table.Cell className={indented ? 'sw-operation-step-nested' : undefined}>
          <Button
            variant="plain"
            size="sm"
            px="0"
            h="auto"
            colorPalette="brand"
            onClick={() => {
              setSelectedStepId(step.id)
              setTab(step.error ? 'stderr' : 'stdout')
            }}
          >
            {step.name}
          </Button>
          <Text as="small" display="block" color="fg.muted" className="mono">
            {step.kind}
          </Text>
        </Table.Cell>
        <Table.Cell>{formatTargets(step)}</Table.Cell>
        <Table.Cell>
          <Badge variant="outline">{step.executor}</Badge>
        </Table.Cell>
        <Table.Cell>
          <StatusBadge status={step.status} />
        </Table.Cell>
        <Table.Cell>{step.attempt}</Table.Cell>
        <Table.Cell>{formatDuration(step.startedAt, step.finishedAt)}</Table.Cell>
        <Table.Cell>
          {step.error?.message ? (
            <span className="sw-reason-cell" title={step.error.message}>
              {firstLine(step.error.message)}
            </span>
          ) : step.live?.currentTask ? (
            <span className="sw-reason-cell" title={step.live.currentTask}>
              {step.live.currentTask}
            </span>
          ) : (
            step.waitingReason ?? '-'
          )}
        </Table.Cell>
        <Table.Cell textAlign="end">
          {retryable && (
            <Tooltip
              content={
                retryDisabledReason ??
                (step.kind === 'provision-os'
                  ? 'Retry verification; a target with no provider address will be released and redeployed'
                  : 'Retry this failed Step without repeating completed Steps')
              }
            >
              <IconButton
                variant="ghost"
                size="sm"
                aria-label={retryDisabledReason ? `Retry: ${retryDisabledReason}` : `Retry ${step.name}`}
                disabled={controlling || Boolean(retryDisabledReason)}
                onClick={() => {
                  if (step.kind === 'provision-os') setRetryCandidate(step)
                  else void retryStep(step)
                }}
              >
                <Redo size={16} />
              </IconButton>
            </Tooltip>
          )}
        </Table.Cell>
      </Table.Row>
    )
  }

  return (
    <div className="operator-page">
      <PageHeader
        title={operation.intent || operation.kind}
        breadcrumbs={[{ label: 'Workflows', href: scopedHref('/workflows') }, { label: operation.id }]}
        metadata={
          <HStack gap="2">
            <StatusBadge status={status} />
            {operation.statusReason && <Text as="span" color="fg.muted">{operation.statusReason}</Text>}
          </HStack>
        }
        actions={
          <>
            <Button variant="outline" onClick={reload}>
              <RefreshCw size={16} />
              Refresh
            </Button>
            {!isTerminalStatus(status) && (
              <Button colorPalette="red" onClick={() => setCancelOpen(true)}>
                <Ban size={16} />
                Cancel
              </Button>
            )}
          </>
        }
      />

      <section className="sw-section">
        <SectionHeader title="Operation details" />
        <KeyValueGrid items={details} />
      </section>

      <section className="sw-section">
        <SectionHeader title="Steps" />
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="Workflow steps" className="sw-operation-steps-table">
            <Table.Body>
              {groups.map((group) => {
                // Legacy/flat operations carry no Job: render a header plus their Steps as plain rows.
                if (!group.job) {
                  return (
                    <Fragment key="__ungrouped__">
                      {stepColumnsRow('__ungrouped-columns', false)}
                      {group.steps.map((step) => renderStepRow(step, false))}
                    </Fragment>
                  )
                }
                const collapsed = jobOverrides[group.job] ?? defaultCollapsed(group.steps)
                const progress = jobProgress(group.steps)
                return (
                  <Fragment key={group.job}>
                    <Table.Row className="sw-operation-job-row">
                      <Table.Cell colSpan={8}>
                        <HStack gap="3" wrap="wrap">
                          <IconButton
                            variant="ghost"
                            size="xs"
                            aria-label={`${collapsed ? 'Expand' : 'Collapse'} ${jobLabel(group.job)}`}
                            onClick={() => toggleJob(group.job, collapsed)}
                          >
                            {collapsed ? <ChevronRight size={16} /> : <ChevronDown size={16} />}
                          </IconButton>
                          <strong>{jobLabel(group.job)}</strong>
                          <StatusBadge status={rollupStatus(group.steps)} />
                          <Text as="span" color="fg.muted">
                            {progress.done}/{progress.total} done
                          </Text>
                          <Text as="span" color="fg.muted">
                            {jobGroupDuration(group.steps)}
                          </Text>
                        </HStack>
                      </Table.Cell>
                    </Table.Row>
                    {!collapsed && (
                      <Fragment>
                        {stepColumnsRow(`${group.job}-columns`, true)}
                        {group.steps.map((step) => renderStepRow(step, true))}
                      </Fragment>
                    )}
                  </Fragment>
                )
              })}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
        {steps.length === 0 && <EmptyState title="No steps" message="No steps are available yet." />}
      </section>

      {selectedStep && (
        <section className="sw-section sw-operation-debugger">
          <SectionHeader title={selectedStep.name} description={`Attempt ${selectedStep.attempt} - ${selectedStep.executor}`} />
          {selectedStep.error && (
            <Alert
              status={selectedStep.status === 'requires_attention' ? 'warning' : 'error'}
              title={selectedStep.error.code}
            >
              {/* Keep the alert a one-line headline; the full failure output lives in the Stderr tab. */}
              {firstLine(selectedStep.error.message)}
              {selectedStep.error.stage ? ` (stage: ${selectedStep.error.stage})` : ''}
            </Alert>
          )}
          {selectedStep.waitingReason && (
            <Alert status="info" title="Waiting">
              {selectedStep.waitingReason}
            </Alert>
          )}
          <Tabs.Root value={tab} onValueChange={(details) => setTab(details.value)} aria-label="Step diagnostics">
            <Tabs.List px="4">
              <Tabs.Trigger value="stdout">Stdout</Tabs.Trigger>
              <Tabs.Trigger value="stderr">Stderr</Tabs.Trigger>
              <Tabs.Trigger value="events">Events</Tabs.Trigger>
              <Tabs.Trigger value="artifacts">Artifacts</Tabs.Trigger>
              <Tabs.Trigger value="details">Details</Tabs.Trigger>
            </Tabs.List>
            <Tabs.Content value="stdout">
              <div className="sw-tab-content">
                {selectedStep.executor === 'ansible' ? (
                  <OperationLogWorkspace operationId={operation.id} stepId={selectedStep.id} />
                ) : (
                  <ProviderStepNote hasServerTarget={Boolean(stepServerTarget(selectedStep))} />
                )}
              </div>
            </Tabs.Content>
            <Tabs.Content value="stderr">
              <div className="sw-tab-content">
                {selectedStep.executor === 'ansible' ? (
                  <OperationLogWorkspace operationId={operation.id} stepId={selectedStep.id} variant="stderr" />
                ) : (
                  <StepErrorReport step={selectedStep} />
                )}
              </div>
            </Tabs.Content>
            <Tabs.Content value="events">
              <div className="sw-tab-content">
                {selectedStep.executor === 'ansible' ? (
                  <StepEvents operation={operation} step={selectedStep} />
                ) : stepServerTarget(selectedStep) ? (
                  <StepProviderEvents serverId={stepServerTarget(selectedStep) as string} />
                ) : (
                  <ProviderStepNote hasServerTarget={false} />
                )}
              </div>
            </Tabs.Content>
            <Tabs.Content value="artifacts">
              <div className="sw-tab-content">
                <StepArtifacts operationId={operation.id} step={selectedStep} />
              </div>
            </Tabs.Content>
            <Tabs.Content value="details">
              <div className="sw-tab-content">
                <StepDetails step={selectedStep} />
              </div>
            </Tabs.Content>
          </Tabs.Root>
        </section>
      )}

      <section className="sw-section">
        <SectionHeader title="Timeline" />
        <div className="sw-section-body">
          <OperationTimeline operation={operation} />
        </div>
      </section>

      <Modal
        open={retryCandidate !== null}
        onClose={() => !controlling && setRetryCandidate(null)}
        size="md"
        closeOnInteractOutside={!controlling}
        title="Retry failed OS deployment"
        footer={
          <>
            <Button variant="ghost" disabled={controlling} onClick={() => setRetryCandidate(null)}>
              Cancel
            </Button>
            <Button colorPalette="red" loading={controlling} onClick={() => retryCandidate && void retryStep(retryCandidate)}>
              Retry deployment
            </Button>
          </>
        }
      >
        <Stack gap="3">
          <Alert status="warning" title="This retry may redeploy the Server">
            Swallow first rechecks the installed image, provider address, and SSH. If MAAS still reports no address,
            Swallow will release the unusable installation, wait for Ready, then redeploy the same image, network
            settings, and protected cloud-init data. An SSH-only failure is not redeployed. Other successful targets
            and completed Steps are preserved.
          </Alert>
        </Stack>
      </Modal>

      <Modal
        open={cancelOpen}
        onClose={() => !controlling && setCancelOpen(false)}
        size="md"
        closeOnInteractOutside={!controlling}
        title="Cancel Operation"
        footer={
          <>
            <Button variant="ghost" disabled={controlling} onClick={() => setCancelOpen(false)}>
              Keep running
            </Button>
            <Button colorPalette="red" loading={controlling} onClick={() => void cancel()}>
              Cancel Operation
            </Button>
          </>
        }
      >
        <Alert status="warning" title="Completed work is not rolled back">
          Swallow stops work that has not started and asks active providers to cancel when supported. Installed
          operating systems and Platform software remain in place.
        </Alert>
      </Modal>
    </div>
  )
}

/**
 * First line of a possibly multi-line message, for a compact single-line cell. The executor
 * emits one failure line per host separated by newlines; the table shows the first with the
 * full text on hover, while the Alert and Stdout tab carry the rest.
 */
function firstLine(message: string): string {
  const line = message.split('\n', 1)[0]?.trim() ?? ''
  return line || message.trim()
}

function formatTargets(step: OperationStep): string {
  const targets = step.targets ?? []
  if (targets.length === 0) return '-'
  const visible = targets.slice(0, 2).map((target) => target.id)
  return `${visible.join(', ')}${targets.length > visible.length ? ` +${targets.length - visible.length}` : ''}`
}

function formatDuration(start: string | null, finish: string | null): string {
  if (!start) return '-'
  const milliseconds = Math.max(0, new Date(finish ?? Date.now()).getTime() - new Date(start).getTime())
  if (milliseconds < 1000) return '<1s'
  const seconds = Math.floor(milliseconds / 1000)
  return seconds < 60 ? `${seconds}s` : `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}

/** One Job and the Steps that belong to it, in workflow order. */
interface StepGroup {
  job: string
  steps: OperationStep[]
}

// Human labels for the Jobs a platform deployment emits; an unknown Job id is humanized.
const JOB_LABELS: Record<string, string> = {
  'ensure-os': 'Provision OS',
  'configure-k0s': 'Configure Kubernetes',
  'configure-slurm': 'Configure Slurm',
}

function jobLabel(job: string): string {
  return (
    JOB_LABELS[job] ??
    job
      .split('-')
      .map((word) => (word ? word[0].toUpperCase() + word.slice(1) : word))
      .join(' ')
  )
}

/**
 * Groups Steps by their Job, preserving order and first-appearance sequence (which is dependency
 * order: ensure-os before the configure Job). Steps with no Job collapse into a single '' group the
 * caller renders flat, so legacy operations look unchanged.
 */
function groupStepsByJob(steps: OperationStep[]): StepGroup[] {
  const order: string[] = []
  const byJob = new Map<string, OperationStep[]>()
  for (const step of steps) {
    const job = step.job ?? ''
    const existing = byJob.get(job)
    if (existing) {
      existing.push(step)
    } else {
      byJob.set(job, [step])
      order.push(job)
    }
  }
  return order.map((job) => ({ job, steps: byJob.get(job) ?? [] }))
}

function jobProgress(steps: OperationStep[]): { done: number; total: number } {
  const done = steps.filter((step) => step.status === 'succeeded' || step.status === 'skipped').length
  return { done, total: steps.length }
}

/**
 * A Job folds by default only when every Step is terminal-good, so any running, waiting, pending,
 * failed, or attention Step keeps it open — problems and active work are never hidden by default.
 */
function defaultCollapsed(steps: OperationStep[]): boolean {
  return steps.length > 0 && steps.every((step) => step.status === 'succeeded' || step.status === 'skipped')
}

/** Reduces a Job's Steps to one badge status by severity, then activity, then completion. */
function rollupStatus(steps: OperationStep[]): OperationStepStatus {
  const has = (status: OperationStepStatus) => steps.some((step) => step.status === status)
  if (has('failed')) return 'failed'
  if (has('requires_attention')) return 'requires_attention'
  if (has('canceled')) return 'canceled'
  if (has('running')) return 'running'
  if (has('waiting_external')) return 'waiting_external'
  if (has('waiting_dependency')) return 'waiting_dependency'
  if (steps.every((step) => step.status === 'succeeded' || step.status === 'skipped')) return 'succeeded'
  return 'pending'
}

/**
 * Spans a Job from its earliest Step start to its latest finish. While any Step is unfinished it
 * passes a null finish so formatDuration measures to now (an in-progress Job keeps ticking).
 */
function jobGroupDuration(steps: OperationStep[]): string {
  const starts = steps.map((step) => step.startedAt).filter((value): value is string => Boolean(value))
  if (starts.length === 0) return '-'
  const start = starts.reduce((earliest, value) => (value < earliest ? value : earliest))
  const allFinished = steps.every((step) => step.finishedAt)
  const finishes = steps.map((step) => step.finishedAt).filter((value): value is string => Boolean(value))
  const finish = allFinished && finishes.length > 0 ? finishes.reduce((latest, value) => (value > latest ? value : latest)) : null
  return formatDuration(start, finish)
}

/**
 * The Step table's column header, emitted once per Job group (and once for ungrouped legacy Steps)
 * instead of once at the top, so each Job reads as its own labelled block. `indented` lines the
 * first column up with a Job's nested Step names.
 */
function stepColumnsRow(key: string, indented: boolean) {
  return (
    <Table.Row key={key} className="sw-operation-columns-row">
      <Table.ColumnHeader className={indented ? 'sw-operation-step-nested' : undefined}>Step</Table.ColumnHeader>
      <Table.ColumnHeader>Targets</Table.ColumnHeader>
      <Table.ColumnHeader>Executor</Table.ColumnHeader>
      <Table.ColumnHeader>Status</Table.ColumnHeader>
      <Table.ColumnHeader>Attempt</Table.ColumnHeader>
      <Table.ColumnHeader>Duration</Table.ColumnHeader>
      <Table.ColumnHeader>Reason</Table.ColumnHeader>
      <Table.ColumnHeader>
        <VisuallyHidden>Actions</VisuallyHidden>
      </Table.ColumnHeader>
    </Table.Row>
  )
}

function StepEvents({ operation, step }: { operation: Operation; step: OperationStep }) {
  const { operations } = useApp()
  const [events, setEvents] = useState<OperationEvents | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    let canceled = false
    operations
      .getStepEvents(operation.id, step.id)
      .then((result) => {
        if (!canceled) setEvents(result)
      })
      .catch((caught: Error) => {
        if (!canceled) setError(caught.message)
      })
    return () => {
      canceled = true
    }
  }, [operation.id, operation.updatedAt, operations, step.id])
  if (error) {
    return (
      <Alert status="warning" title="Events unavailable">
        {error}
      </Alert>
    )
  }
  const running = !['succeeded', 'failed', 'canceled', 'skipped'].includes(step.status)
  const live = step.live
  return (
    <Stack gap="3">
      {running && live && (live.currentTask || live.currentPlay) && (
        <Alert status="info" title={`Running: ${live.currentTask || live.currentPlay}`}>
          {`${live.currentPlay && live.currentTask ? `Play "${live.currentPlay}" - ` : ''}${live.ok} ok, ${live.changed} changed, ${live.failed} failed${live.unreachable ? `, ${live.unreachable} unreachable` : ''}${live.skipped ? `, ${live.skipped} skipped` : ''}`}
        </Alert>
      )}
      <OperationEventWorkspace events={events} running={running} />
    </Stack>
  )
}

function StepArtifacts({ operationId, step }: { operationId: string; step: OperationStep }) {
  const { operations } = useApp()
  const [artifacts, setArtifacts] = useState<OperationArtifact[]>(step.artifacts ?? [])
  useEffect(() => {
    let canceled = false
    operations
      .getStepArtifacts(operationId, step.id)
      .then((items) => {
        if (!canceled) setArtifacts(items ?? [])
      })
      .catch(() => undefined)
    return () => {
      canceled = true
    }
  }, [operationId, operations, step.id, step.artifacts])
  if (artifacts.length === 0) {
    return <EmptyState title="No artifacts" message="This Step has no retained artifact metadata." />
  }
  return (
    <DescriptionList
      emptyText="No data"
      items={artifacts.map((artifact) => ({
        label: artifact.name,
        value: `${artifact.mediaType} - ${artifact.sizeBytes.toLocaleString()} bytes - ${formatDateTime(artifact.createdAt)}`,
      }))}
    />
  )
}

/**
 * The failure report for a provisioner or internal Step, which produces no Ansible stdout/stderr:
 * its outcome is the normalized error (code, stage, retryable, and the full operator-facing
 * message). Shown on the Stderr tab in place of the Ansible log workspace so a failed provider Step
 * reads its cause here instead of the misleading "No errors" empty state. A Step with no error
 * completed its provider work; its timing is on the Details tab and the provider's own event log is
 * on the Server (see the Stdout/Events note).
 */
function StepErrorReport({ step }: { step: OperationStep }) {
  if (!step.error) {
    return (
      <EmptyState
        title="No error reported"
        message="This step completed its provider work without a recorded error. Provider and internal steps produce no command output — see Details for timing, and the Server's Activity tab for the provisioner's own event log."
      />
    )
  }
  return (
    <Stack gap="3" p="4">
      <KeyValueGrid
        items={[
          { label: 'Code', value: <span className="mono">{step.error.code || '-'}</span> },
          { label: 'Stage', value: step.error.stage || '-' },
          { label: 'Retryable', value: step.error.retryable ? 'Yes' : 'No' },
        ]}
      />
      <Text whiteSpace="pre-wrap" className="sw-error-detail">
        {step.error.message}
      </Text>
    </Stack>
  )
}

/** The first server-target id of a Step, used to fetch the provisioner's event log for it. */
function stepServerTarget(step: OperationStep): string | undefined {
  return step.targets?.find((target) => target.kind === 'server')?.id
}

/**
 * Explains that a provisioner/internal Step drives the provider (or records a Swallow fact) through
 * API calls and therefore has no Ansible stdout — so the Stdout tab is not blank by accident. It
 * points to the sibling tabs (Stderr for the outcome, Events for the provider's event timeline) on
 * this same page; it deliberately does not link back to the Server, which would loop the operator
 * between the Server and the Operation they navigated in from.
 */
function ProviderStepNote({ hasServerTarget }: { hasServerTarget: boolean }) {
  return (
    <EmptyState
      title="No command output"
      message={
        'This step drives the provider (or records a Swallow fact) through API calls, so it has no stdout. Its outcome is on the Stderr tab' +
        (hasServerTarget ? ", and the provider's own event timeline is on the Events tab" : '') +
        '; timing is on Details.'
      }
    />
  )
}

/**
 * The target Server's provider event log, rendered inline on the Operation step for a provisioner
 * Step (for example the MAAS PXE-boot / deploying / disk-erasing / marking-failed timeline). This is
 * the closest thing to "what happened" for a step that runs no Ansible, and it is shown here — on
 * the Operation the operator is already looking at — rather than linking back to the Server, so
 * there is no Server-to-Operation-to-Server navigation loop. It is provider-retained history, not a
 * complete Swallow audit log.
 */
function StepProviderEvents({ serverId }: { serverId: string }) {
  const { servers } = useApp()
  const [state, setState] = useState<
    { status: 'loading' } | { status: 'error'; message: string } | { status: 'ready'; data: ProviderEvents }
  >({ status: 'loading' })
  const [nonce, setNonce] = useState(0)
  useEffect(() => {
    let cancelled = false
    servers
      .getProviderEvents(serverId, 50)
      .then((data) => {
        if (!cancelled) setState({ status: 'ready', data })
      })
      .catch((error: Error) => {
        if (!cancelled) setState({ status: 'error', message: error.message })
      })
    return () => {
      cancelled = true
    }
  }, [serverId, servers, nonce])
  return (
    <Stack gap="3" p="4">
      <HStack justify="space-between" align="start" gap="4" wrap="wrap">
        <Text color="fg.muted">
          The provisioner&apos;s own event log for this Server — the closest thing to &quot;what happened&quot; for a
          provider step. Provider-retained history, not a complete Swallow audit log.
        </Text>
        <Button variant="outline" size="sm" onClick={() => setNonce((value) => value + 1)}>
          <RefreshCw size={14} />
          Refresh
        </Button>
      </HStack>
      {state.status === 'loading' && <Text color="fg.muted">Loading provider events…</Text>}
      {state.status === 'error' && (
        <Alert status="warning" title="Provider events are unavailable">
          {state.message}
        </Alert>
      )}
      {state.status === 'ready' && !state.data.supported && (
        <EmptyState title="Not supported" message="This provisioner does not expose machine events." />
      )}
      {state.status === 'ready' && state.data.supported && state.data.events.length === 0 && (
        <EmptyState title="No provider events" message="No provider events are retained for this Server." />
      )}
      {state.status === 'ready' && state.data.events.length > 0 && (
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="Provider events for the target Server">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Time</Table.ColumnHeader>
                <Table.ColumnHeader>Level</Table.ColumnHeader>
                <Table.ColumnHeader>Type</Table.ColumnHeader>
                <Table.ColumnHeader>Message</Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {state.data.events.map((event) => (
                <Table.Row key={event.id}>
                  <Table.Cell>{formatDateTime(event.occurredAt)}</Table.Cell>
                  <Table.Cell>
                    <StatusBadge status={event.level} />
                  </Table.Cell>
                  <Table.Cell>{event.type || '-'}</Table.Cell>
                  <Table.Cell>{event.message || '-'}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
      )}
    </Stack>
  )
}

function StepDetails({ step }: { step: OperationStep }) {
  return (
    <KeyValueGrid
      items={[
        { label: 'Step ID', value: <span className="mono">{step.id}</span> },
        { label: 'Dependencies', value: step.dependsOn?.join(', ') || '-' },
        ...(step.live
          ? [{ label: 'Current task', value: step.live.currentTask || step.live.currentPlay || '-' }]
          : []),
        {
          label: 'Progress',
          value: step.live
            ? `${step.live.ok} ok, ${step.live.changed} changed, ${step.live.failed} failed` +
              `${step.live.unreachable ? `, ${step.live.unreachable} unreachable` : ''}` +
              `${step.live.skipped ? `, ${step.live.skipped} skipped` : ''} (${step.live.total} tasks)`
            : `${step.progress}%`,
        },
        { label: 'External provider', value: step.externalExecution?.provider ?? '-' },
        { label: 'External execution', value: <span className="mono">{step.externalExecution?.id ?? '-'}</span> },
        { label: 'Started', value: formatDateTime(step.startedAt ?? undefined) },
        { label: 'Finished', value: formatDateTime(step.finishedAt ?? undefined) },
        { label: 'Retryable', value: step.error ? (step.error.retryable ? 'Yes' : 'No') : '-' },
      ]}
    />
  )
}

type TimelineTone = 'success' | 'danger' | 'warning' | 'info' | 'neutral'

/**
 * Maps a normalized workflow event type to a timeline marker tone and glyph. Matching is by
 * keyword so a new or unknown event type still resolves to a sensible neutral marker rather
 * than rendering nothing. The tone drives the marker colour in CSS (`data-tone`).
 */
function timelineEventTone(type: string): { tone: TimelineTone; Icon: LucideIcon } {
  const value = type.toLowerCase()
  if (/(succeed|complete|active|ready)/.test(value)) return { tone: 'success', Icon: Check }
  if (/(fail|error)/.test(value)) return { tone: 'danger', Icon: X }
  if (/cancel/.test(value)) return { tone: 'neutral', Icon: Ban }
  if (/(attention|warn)/.test(value)) return { tone: 'warning', Icon: TriangleAlert }
  if (/(start|running|request|accept|queue|retry|resume|dispatch)/.test(value)) {
    return { tone: 'info', Icon: CircleDot }
  }
  return { tone: 'neutral', Icon: CircleDot }
}

/** Renders an event type token such as `operation_requested` as "Operation requested". */
function humanizeEventType(type: string): string {
  const spaced = type.replace(/[_-]+/g, ' ').trim()
  return spaced ? spaced.charAt(0).toUpperCase() + spaced.slice(1) : type
}

function OperationTimeline({ operation }: { operation: Operation }) {
  const { operations } = useApp()
  const [events, setEvents] = useState<OperationTimelineEvent[]>([])
  useEffect(() => {
    let canceled = false
    operations
      .getTimeline(operation.id)
      .then((items) => {
        if (!canceled) setEvents(items)
      })
      .catch(() => undefined)
    return () => {
      canceled = true
    }
  }, [operation.id, operation.updatedAt, operations])
  const stepNames = useMemo(() => new Map((operation.steps ?? []).map((step) => [step.id, step.name])), [operation.steps])
  if (events.length === 0) return <EmptyState title="No timeline events" />
  return (
    <ol className="sw-timeline">
      {events.map((event) => {
        const { tone, Icon } = timelineEventTone(event.type)
        const title = event.stepId ? (stepNames.get(event.stepId) ?? event.stepId) : humanizeEventType(event.type)
        return (
          <li key={event.id} className="sw-timeline__item" data-tone={tone}>
            <span className="sw-timeline__marker" aria-hidden="true">
              <Icon />
            </span>
            <div className="sw-timeline__content">
              <strong className="sw-timeline__title">{title}</strong>
              <time className="sw-timeline__time" dateTime={event.createdAt}>
                {formatDateTime(event.createdAt)}
              </time>
              {event.message && <span className="sw-timeline__message">{event.message}</span>}
            </div>
          </li>
        )
      })}
    </ol>
  )
}
