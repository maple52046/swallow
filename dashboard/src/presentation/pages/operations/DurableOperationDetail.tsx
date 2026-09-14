import { useCallback, useEffect, useMemo, useState } from 'react'
import { Badge, Button, HStack, IconButton, Stack, Table, Tabs, Text, VisuallyHidden } from '@chakra-ui/react'
import { Ban, Check, CircleDot, Redo, RefreshCw, TriangleAlert, X, type LucideIcon } from 'lucide-react'
import { useApp } from '@/di/AppProvider'
import {
  isTerminalStatus,
  operationStatus,
  type Operation,
  type OperationArtifact,
  type OperationEvents,
  type OperationStep,
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
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Step</Table.ColumnHeader>
                <Table.ColumnHeader>Targets</Table.ColumnHeader>
                <Table.ColumnHeader>Executor</Table.ColumnHeader>
                <Table.ColumnHeader>Status</Table.ColumnHeader>
                <Table.ColumnHeader>Attempt</Table.ColumnHeader>
                <Table.ColumnHeader>Duration</Table.ColumnHeader>
                <Table.ColumnHeader>Reason</Table.ColumnHeader>
                <Table.ColumnHeader><VisuallyHidden>Actions</VisuallyHidden></Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {steps.map((step) => {
                const retryable = (step.status === 'failed' || step.status === 'requires_attention') && step.error?.retryable
                return (
                  <Table.Row key={step.id} bg={selectedStep?.id === step.id ? 'bg.subtle' : undefined}>
                    <Table.Cell>
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
                <OperationLogWorkspace operationId={operation.id} stepId={selectedStep.id} />
              </div>
            </Tabs.Content>
            <Tabs.Content value="stderr">
              <div className="sw-tab-content">
                <OperationLogWorkspace operationId={operation.id} stepId={selectedStep.id} variant="stderr" />
              </div>
            </Tabs.Content>
            <Tabs.Content value="events">
              <div className="sw-tab-content">
                <StepEvents operation={operation} step={selectedStep} />
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
