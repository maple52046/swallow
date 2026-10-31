import { useMemo, useState } from 'react'
import { Button, HStack, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import {
  GPU_ALLOCATION_MODE_LABELS,
  GPU_RESOURCE_NAMING_LABELS,
  gpuOperatorStateLabel,
  gpuOperatorStatus,
  hasAMDComputeGPU,
  isGPUOperatorInProgress,
} from '@/domain/platform/gpuOperator'
import type { Platform } from '@/domain/platform/types'
import type { Server } from '@/domain/server/types'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { InProgressSpinner } from '@/presentation/components/InProgressSpinner'
import { SectionHeader } from '@/presentation/components/OperatorPrimitives'
import { StatusBadge } from '@/presentation/components/StatusBadge'
import { Alert } from '@/presentation/components/ui/alert'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import { Modal } from '@/presentation/components/ui/modal'
import { useToast } from '@/presentation/components/toast/toastContext'
import { GPUOperatorOptionsFields } from './GPUOperatorOptionsFields'
import {
  gpuOperatorFormFromPlatform,
  gpuOperatorFormProblem,
  gpuOperatorFormSettings,
  type GPUOperatorFormValue,
} from './gpuOperatorForm'
import { useGPUOperatorReleases } from './useGPUOperatorReleases'

interface PlatformGPUOperatorSectionProps {
  platform: Platform
  /** The Platform's Servers (members plus deployment controllers), for their GPU inventory. */
  members: readonly Server[]
  /** Reloads the Platform after an accepted change so the new state and Workflow appear. */
  onChanged: () => void
  onOpenWorkflow: (workflowId: string) => void
}

/**
 * The Swallow-managed AMD GPU Operator of a Kubernetes Platform (decision 062), on the Platform
 * Overview: its GPU Operator State, recorded settings, and the Install, Configure, and Uninstall
 * actions.
 *
 * Changes are allowed only on an active Swallow-deployed Platform with no change running, which
 * mirrors the backend preconditions; the reason is shown as text when they are not. A change opens
 * the shared GPUOperatorOptionsFields in a dialog, and the request's API error (for example a busy
 * target) stays in the dialog. Uninstall asks for confirmation and names the consequence. A failed
 * state warns that the cluster may still run the previous settings. The page polls while a change
 * is in progress (usePlatformDetail).
 */
export function PlatformGPUOperatorSection({ platform, members, onChanged, onOpenWorkflow }: PlatformGPUOperatorSectionProps) {
  const { platforms } = useApp()
  const { showToast } = useToast()
  const [dialog, setDialog] = useState<'configure' | 'uninstall' | null>(null)
  const [form, setForm] = useState<GPUOperatorFormValue | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const { state: releases, reload: reloadReleases } = useGPUOperatorReleases(dialog === 'configure')

  const operator = platform.gpuOperator
  const managed = operator !== null && operator.state !== 'absent'
  const inProgress = operator !== null && isGPUOperatorInProgress(operator.state)
  const serversById = useMemo(() => new Map(members.map((server) => [server.id, server])), [members])
  // The operator can be installed when a workload assignment's inventory has an AMD compute GPU,
  // the backend's rule. Which nodes it manages is decided in the cluster, not here.
  const hasAMDGPUWorkload = useMemo(() => (platform.deployment?.roleAssignments ?? [])
    .filter((assignment) => assignment.role === 'worker' || assignment.runWorkloads)
    .some((assignment) => {
      const server = serversById.get(assignment.serverId)
      return Boolean(server && hasAMDComputeGPU(server.status.gpus))
    }), [platform.deployment, serversById])

  const blockedReason = platform.origin !== 'deployed'
    ? 'Only a Platform Swallow deployed can have a Swallow-managed GPU Operator.'
    : platform.lifecycleState !== 'active'
      ? 'GPU Operator changes are available once the Platform is active.'
      : inProgress
        ? 'A GPU Operator change is running.'
        : null
  const catalog = releases.status === 'ready' ? releases.releases : null
  const problem = form ? gpuOperatorFormProblem(form, hasAMDGPUWorkload, catalog, null) : null

  const openConfigure = () => {
    setForm(gpuOperatorFormFromPlatform(managed ? operator : null))
    setError('')
    setDialog('configure')
  }
  const close = () => {
    if (submitting) return
    setDialog(null)
    setError('')
  }
  const submit = async (settings: Parameters<typeof platforms.configureGPUOperator>[1], title: string) => {
    setSubmitting(true)
    setError('')
    try {
      await platforms.configureGPUOperator(platform.id, settings)
      showToast({ tone: 'success', title, description: 'The Platform shows the GPU Operator state while the workflow runs.' })
      setDialog(null)
      onChanged()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Could not start the GPU Operator change.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <section className="sw-section" aria-label="AMD GPU Operator">
      <SectionHeader
        title="AMD GPU Operator"
        description="Schedules workloads on this Platform's AMD GPUs."
        actions={
          <HStack gap="2">
            {managed ? (
              <>
                <Button variant="outline" size="sm" disabled={Boolean(blockedReason)} onClick={openConfigure}>Configure</Button>
                <Button variant="outline" size="sm" colorPalette="red" disabled={Boolean(blockedReason)} onClick={() => { setError(''); setDialog('uninstall') }}>
                  Uninstall
                </Button>
              </>
            ) : (
              <Button variant="outline" size="sm" disabled={Boolean(blockedReason) || !hasAMDGPUWorkload} onClick={openConfigure}>
                Install
              </Button>
            )}
          </HStack>
        }
      />
      <Stack gap="3" className="sw-section-content sw-gpu-operator-summary">
        <HStack gap="2">
          <StatusBadge status={gpuOperatorStatus(operator?.state ?? 'absent')} label={gpuOperatorStateLabel(operator?.state ?? 'absent')} />
          {inProgress && <InProgressSpinner color="blue.fg" />}
          {operator?.lastWorkflowId && (
            <Button variant="plain" size="sm" px="0" h="auto" colorPalette="brand" onClick={() => onOpenWorkflow(operator.lastWorkflowId!)}>
              View workflow
            </Button>
          )}
        </HStack>
        {operator?.state === 'failed' && (
          <Alert status="error" title="The last GPU Operator change did not finish">
            The cluster may still run the previous settings. Open the workflow for the cause: retry it once the cause is fixed, or cancel it before applying other settings.
          </Alert>
        )}
        {managed ? (
          <DescriptionList
            items={[
              { label: 'Version', value: operator.version },
              { label: 'Allocation mode', value: GPU_ALLOCATION_MODE_LABELS[operator.allocationMode] },
              ...(operator.allocationMode === 'dra'
                ? [{ label: 'DRA driver', value: operator.draDriverVersion ?? '-' }]
                : [{ label: 'Resource naming', value: GPU_RESOURCE_NAMING_LABELS[operator.resourceNaming] }]),
              { label: 'Node Labeller', value: operator.nodeLabeller ? 'On' : 'Off' },
              { label: 'Device Metrics Exporter', value: operator.metricsExporter ? 'On' : 'Off' },
            ]}
          />
        ) : (
          <Text color="fg.muted" fontSize="sm">
            {!hasAMDGPUWorkload
              ? 'Not installed. No workload node of this Platform has an AMD compute GPU.'
              : 'Not installed. Install it to schedule workloads on the AMD GPUs of this Platform.'}
          </Text>
        )}
        {blockedReason && managed && <Text color="fg.muted" fontSize="sm">{blockedReason}</Text>}
      </Stack>

      {dialog === 'configure' && form && (
        <Modal
          open
          onClose={close}
          size="lg"
          closeOnInteractOutside={!submitting}
          title={managed ? 'Configure AMD GPU Operator' : 'Install AMD GPU Operator'}
          description={managed
            ? 'Changing the allocation mode is refused while a Pod still uses an AMD GPU.'
            : 'Swallow installs the operator with the GPU driver already in the OS image.'}
          onSubmit={(event) => {
            event.preventDefault()
            if (!problem) void submit(gpuOperatorFormSettings(form), managed ? 'GPU Operator change started' : 'GPU Operator installation started')
          }}
          footer={
            <>
              <Button variant="ghost" onClick={close} disabled={submitting}>Cancel</Button>
              <Button type="submit" colorPalette="brand" loading={submitting} disabled={submitting || Boolean(problem)}>
                {managed ? 'Apply changes' : 'Install GPU Operator'}
              </Button>
            </>
          }
        >
          {error && (
            <Alert status="error" title="The change could not be started">{error}</Alert>
          )}
          <GPUOperatorOptionsFields
            value={form}
            onChange={setForm}
            hasAMDGPUWorkload={hasAMDGPUWorkload}
            releases={releases}
            onRetryReleases={reloadReleases}
            k0sVersion={null}
            showInstallToggle={false}
            idPrefix="platform-gpu-operator"
          />
        </Modal>
      )}

      <ConfirmDialog
        open={dialog === 'uninstall'}
        title="Uninstall AMD GPU Operator"
        confirmLabel="Uninstall GPU Operator"
        busy={submitting}
        onCancel={close}
        onConfirm={() => void submit({ enabled: false }, 'GPU Operator uninstall started')}
      >
        <Text>
          Swallow removes the AMD GPU Operator, its DeviceConfig, and its node labels from {platform.name}. Pods can no longer
          request AMD GPUs; the Platform itself keeps running. The uninstall is refused while a Pod still uses an AMD GPU.
        </Text>
        {error && <Alert status="error" title="The uninstall could not be started">{error}</Alert>}
      </ConfirmDialog>
    </section>
  )
}
