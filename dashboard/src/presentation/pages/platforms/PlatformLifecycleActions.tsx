import { useRef, useState } from 'react'
import { Button, Field, Input, Stack, Text } from '@chakra-ui/react'
import { Redo } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { isOrchestrationOperation, type Operation } from '@/domain/operation/types'
import { platformPrimaryRemovalAction, platformUninstallAvailability } from '@/domain/platform/lifecycle'
import type { Platform } from '@/domain/platform/types'
import { emptyReleaseOptions, type ReleaseOptionsValue } from '@/presentation/components/releaseOptions'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useServerActiveOperations } from '@/presentation/hooks/useServerActiveOperations'
import { useTargetLockProtection } from '@/presentation/hooks/useTargetLockProtection'
import { PlatformRemovalSplitButton } from './PlatformRemovalSplitButton'
import { PlatformUninstallChoices } from './PlatformUninstallChoices'
import {
  platformRecordDeleteConsequence,
  platformUninstallAcceptedToast,
  platformUninstallConfirmLabel,
  platformUninstallConsequence,
  platformUninstallDeleteHint,
  type PlatformRecordDeleteContext,
} from './platformRemovalCopy'
import { platformUninstallOptions } from './platformUninstallRequest'

type RemovalDialog = 'uninstall' | 'delete'

/**
 * Whether a repair error is an HTTP 409 conflict. For a Step retry this means the durable
 * workflow execution is gone and can no longer accept the signal, which is the cue to recover
 * by rerunning instead. Duck-typed on the API error's status so this presentation code stays
 * decoupled from the infrastructure error class (the layering rule forbids importing
 * `@/infrastructure/**` here).
 */
function isConflict(error: unknown): boolean {
  return typeof error === 'object' && error !== null && 'status' in error && (error as { status?: unknown }).status === 409
}

interface PlatformLifecycleActionsProps {
  platform: Platform
  operation?: Operation
  targetServerIds?: readonly string[]
  onRepairStarted: () => void
}

/**
 * Exposes Platform-scoped repair and the red Platform removal control.
 *
 * Schema-v3 repair signals each retryable failed Step on the existing durable Operation,
 * preserving successful provider effects and protected inputs. Legacy deployment history keeps
 * the compatibility retry that creates a new Operation. The parent callback refreshes lifecycle
 * state without redirecting operators into the automation debugger.
 *
 * Removal (decision 010, 2026-10-29 amendment): while the Platform can still be uninstalled the
 * control is a split button whose primary part opens the Uninstall dialog and whose chevron menu
 * offers "Delete only…" (record-only Delete), so host cleanup and record removal are never
 * presented as peers. The Uninstall dialog's submit is the same kind of split button: its primary
 * part uninstalls and keeps the record, and "… and delete record" in its menu also sends
 * `deleteRecord`, which the backend honors only after the uninstall succeeds. A running uninstall or
 * a locked target disables only the header's Uninstall part, with its reason; record-only Delete
 * never mutates hosts, so it stays available. Once the Platform is `uninstalled`, or was never
 * deployed by Swallow, the control is a single "Delete platform" button.
 *
 * The Uninstall dialog lists the unfinished Workflows still holding the deployment targets (a
 * failed deployment parked for attention, or one still running) and requires the operator to
 * confirm that the backend cancels them (`cancelActiveWorkflows`, decision 061); the request
 * returns at once and the uninstall starts by itself after they end, so the dialog never waits.
 */
export function PlatformLifecycleActions({ platform, operation, targetServerIds, onRepairStarted }: PlatformLifecycleActionsProps) {
  const { platforms, operations } = useApp()
  const { showToast } = useToast()
  const { scopedHref } = useSiteScope()
  const navigate = useNavigate()
  const [dialog, setDialog] = useState<RemovalDialog | null>(null)
  const [confirmation, setConfirmation] = useState('')
  const confirmationRef = useRef<HTMLInputElement>(null)
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [releaseServers, setReleaseServers] = useState(false)
  const [releaseOptions, setReleaseOptions] = useState<ReleaseOptionsValue>(emptyReleaseOptions)
  const [cancelBlocking, setCancelBlocking] = useState(false)
  // Read only while the Uninstall dialog is open, so a closed page issues no Workflow lookups.
  const { activeOperations: blockingWorkflows } = useServerActiveOperations(dialog === 'uninstall' ? targetServerIds ?? [] : [])
  const [repairOpen, setRepairOpen] = useState(false)
  const [repairError, setRepairError] = useState('')
  const [repairing, setRepairing] = useState(false)
  const targetProtection = useTargetLockProtection(targetServerIds)
  const targetCount = targetServerIds?.length
  const targetLabel = targetCount === undefined ? 'the original deployment targets' : `${targetCount} original deployment target${targetCount === 1 ? '' : 's'}`

  const failedProvisioningRecovery = Boolean(
    operation &&
      isOrchestrationOperation(operation) &&
      operation.steps?.some((step) => step.kind === 'provision-os' && (step.status === 'failed' || step.status === 'requires_attention')),
  )
  const legacyReadinessOnly = Boolean(
    operation &&
      isOrchestrationOperation(operation) &&
      operation.steps?.some((step) => step.kind === 'wait-for-ssh' && (step.status === 'failed' || step.status === 'requires_attention')),
  )

  const lockedDisabledReason = targetProtection.checking
    ? 'Checking target protection.'
    : targetProtection.error
      ? targetProtection.error
      : targetProtection.lockedNames.length > 0
        ? `${targetProtection.lockedNames.join(', ')} ${targetProtection.lockedNames.length === 1 ? 'is' : 'are'} locked. Unlock ${targetProtection.lockedNames.length === 1 ? 'it' : 'them'} before changing this Platform.`
        : undefined
  const repairDisabledReason = !platform.lifecycleOperationId
    ? 'The deployment Operation is unavailable.'
    : legacyReadinessOnly
      ? 'This older readiness Step can only recheck SSH and cannot repair missing provider addresses. Release and redeploy the affected Servers.'
      : lockedDisabledReason

  const recordOnly = platformPrimaryRemovalAction(platform) === 'delete'
  // Lock state fails closed for Uninstall. A missing target list is not treated as an error: the
  // backend resolves the frozen deployment snapshot and re-checks locks live anyway.
  const uninstall = platformUninstallAvailability(platform, {
    status: targetProtection.checking ? 'checking' : targetProtection.error ? 'error' : 'ready',
    lockedNames: targetProtection.lockedNames,
  })
  const recordDeleteContext: PlatformRecordDeleteContext = !recordOnly
    ? 'installed'
    : platform.lifecycleState === 'uninstalled'
      ? 'uninstalled'
      : 'external'

  const open = (next: RemovalDialog) => {
    setDialog(next)
    setConfirmation('')
    setError('')
    setReleaseServers(false)
    setReleaseOptions(emptyReleaseOptions)
    setCancelBlocking(false)
  }

  const close = () => {
    if (!submitting) setDialog(null)
  }

  // Releasing changes what the typed name and the cancel consent agree to, so both are asked for
  // again.
  const changeReleaseServers = (checked: boolean) => {
    setReleaseServers(checked)
    if (!checked) setReleaseOptions(emptyReleaseOptions)
    setConfirmation('')
    setCancelBlocking(false)
  }

  const blocked = dialog === 'uninstall' && blockingWorkflows.length > 0
  const awaitingCancelConfirmation = blocked && !cancelBlocking
  const canSubmit = dialog !== null && confirmation === platform.name && !submitting && !awaitingCancelConfirmation

  // `deleteRecord` is chosen at submit time: the primary part of the Uninstall submit keeps the
  // record, and the variant in its menu also deletes it after the uninstall succeeds.
  const submit = async (deleteRecord = false) => {
    if (!canSubmit) return
    setSubmitting(true)
    setError('')
    try {
      if (dialog === 'uninstall') {
        const accepted = await platforms.uninstallPlatform(platform.id, platformUninstallOptions({
          releaseServers, releaseOptions, deleteRecord, cancelActiveWorkflows: blocked && cancelBlocking,
        }))
        const toast = platformUninstallAcceptedToast({ releaseServers, deleteRecord }, targetLabel)
        const waits = blocked ? ' It starts once the canceled workflows have ended.' : ''
        showToast({ tone: 'success', title: toast.title, description: toast.description + waits })
        navigate(scopedHref(`/workflows/${accepted.operationId}`))
        return
      }

      await platforms.deletePlatform(platform.id)
      showToast({
        tone: 'success',
        title: 'Platform record deleted',
        description: 'The Swallow record and owned projections were removed. Hosts were not changed. Any in-flight workflows were canceled.',
      })
      navigate(scopedHref('/platforms'), { replace: true })
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The platform action failed.')
    } finally {
      setSubmitting(false)
    }
  }

  const startRepair = async () => {
    if (!platform.lifecycleOperationId || repairing) return
    setRepairing(true)
    setRepairError('')
    try {
      const operation = await operations.getOperation(platform.lifecycleOperationId)
      if (!operation) throw new Error('The deployment Operation no longer exists.')

      let description: string
      if (isOrchestrationOperation(operation)) {
        const failedSteps = (operation.steps ?? []).filter((step) => (step.status === 'failed' || step.status === 'requires_attention') && step.error?.retryable)
        if (failedSteps.length > 0) {
          try {
            // Preferred path while the durable workflow is still alive: signal each retryable
            // failed Step so successful provider effects and protected inputs are preserved.
            for (const step of failedSteps) {
              await operations.retryStep(operation.id, step.id)
            }
            description =
              failedSteps.length === 1
                ? `${failedSteps[0].name} will retry in Operation ${operation.id}.`
                : `${failedSteps.length} failed Steps will retry in Operation ${operation.id}.`
          } catch (retryError) {
            // A conflict means the workflow execution is gone (a host restart or execution
            // timeout ended it), so it can no longer accept a Step retry. Recover by rerunning
            // the deployment as a new Operation on the same Platform instead.
            if (!isConflict(retryError)) throw retryError
            const created = await operations.rerunOperation(operation.id)
            description = `Operation ${created.id} is rerunning the deployment on the same platform.`
          }
        } else {
          // No live retryable Step (for example the execution was lost mid-run and left its
          // Steps non-terminal). Rerun the deployment on the same Platform.
          const created = await operations.rerunOperation(operation.id)
          description = `Operation ${created.id} is rerunning the deployment on the same platform.`
        }
      } else {
        const created = await operations.retryOperation(operation.id)
        description = `Operation ${created.id} is rerunning the original deployment configuration.`
      }
      setRepairOpen(false)
      showToast({ tone: 'success', title: 'Platform repair started', description })
      onRepairStarted()
    } catch (caught) {
      setRepairError(caught instanceof Error ? caught.message : 'Could not start platform repair.')
    } finally {
      setRepairing(false)
    }
  }

  const consequence = dialog === 'uninstall' ? platformUninstallConsequence(releaseServers, 'single') : platformRecordDeleteConsequence(recordDeleteContext)

  return (
    <>
      {platform.lifecycleState === 'deploy_failed' && (
        <Tooltip content={repairDisabledReason ?? 'Rerun the original deployment configuration'}>
          <span>
            <Button
              colorPalette="brand"
              onClick={() => {
                setRepairError('')
                setRepairOpen(true)
              }}
              disabled={Boolean(repairDisabledReason)}
              aria-label={repairDisabledReason ? `Repair deployment: ${repairDisabledReason}` : 'Repair deployment'}
            >
              <Redo size={16} />
              Repair deployment
            </Button>
          </span>
        </Tooltip>
      )}
      {recordOnly ? (
        <Button colorPalette="red" className="sw-removal-delete" onClick={() => open('delete')}>
          Delete platform
        </Button>
      ) : (
        <PlatformRemovalSplitButton
          primaryLabel="Uninstall platform"
          primaryDisabledReason={uninstall.reason}
          onPrimaryAction={() => open('uninstall')}
          menuItemLabel="Delete only…"
          onMenuSelect={() => open('delete')}
          placement="header"
        />
      )}

      <Modal
        open={repairOpen}
        onClose={() => !repairing && setRepairOpen(false)}
        closeOnInteractOutside={!repairing}
        title="Repair platform deployment"
        footer={
          <>
            <Button variant="ghost" onClick={() => setRepairOpen(false)} disabled={repairing}>
              Cancel
            </Button>
            <Button colorPalette="brand" onClick={() => void startRepair()} loading={repairing} disabled={repairing || Boolean(repairDisabledReason)}>
              Repair deployment
            </Button>
          </>
        }
      >
        <Stack gap="3">
          {repairError && (
            <Alert status="error" title="Repair could not start">
              {repairError}
            </Alert>
          )}
          <Alert
            status={failedProvisioningRecovery ? 'warning' : 'info'}
            title={failedProvisioningRecovery ? 'Provisioning recovery may redeploy failed Servers' : 'Original configuration will be reused'}
          >
            Repair resumes only failed retryable work against the hosts&apos; current state, using the same machines,
            roles, network settings, and protected credentials. Successful Steps stay completed, and existing
            attempts, events, and logs remain available for diagnosis.
            {failedProvisioningRecovery && ' Swallow rechecks each failed target first. A target with no MAAS address is released, returned to Ready, and redeployed; an SSH-only failure is only rechecked.'}
          </Alert>
          <Text>
            <strong>Targets:</strong> {targetLabel}
          </Text>
        </Stack>
      </Modal>

      <Modal
        open={dialog !== null}
        onClose={close}
        size={dialog === 'uninstall' ? 'lg' : 'md'}
        closeOnInteractOutside={!submitting}
        dismissDisabled={submitting}
        initialFocusEl={() => confirmationRef.current}
        title={dialog === 'uninstall' ? 'Uninstall platform' : 'Delete platform record'}
        description={dialog === 'uninstall'
          ? `Remove Swallow-managed platform software from “${platform.name}”. This acts on the original deployment targets; the Swallow record is kept unless you choose to delete it after success.`
          : `Permanently delete the Swallow record for “${platform.name}”. This never changes hosts.`}
        onSubmit={(event) => {
          event.preventDefault()
          void submit()
        }}
        footer={
          <>
            <Button variant="ghost" onClick={close} disabled={submitting}>
              Cancel
            </Button>
            {dialog === 'uninstall' ? (
              <PlatformRemovalSplitButton
                primaryType="submit"
                primaryLabel={platformUninstallConfirmLabel({ releaseServers, deleteRecord: false })}
                disabled={!canSubmit}
                loading={submitting}
                menuItemLabel={platformUninstallConfirmLabel({ releaseServers, deleteRecord: true })}
                onMenuSelect={() => void submit(true)}
                menuTriggerLabel="More uninstall options"
                placement="dialog"
              />
            ) : (
              <Button type="submit" colorPalette="red" loading={submitting} disabled={!canSubmit}>
                Delete record
              </Button>
            )}
          </>
        }
      >
        <Stack gap="4">
          {error && (
            <Alert status="error" title="Action failed">
              {error}
            </Alert>
          )}
          {/* Polite so a screen reader hears the new targets and consequence when a choice changes. */}
          <Stack gap="3" aria-live="polite">
            <Text>
              {dialog === 'uninstall' ? (
                <><strong>Targets:</strong> {targetLabel}</>
              ) : (
                <><strong>Target:</strong> Swallow record “{platform.name}”. Hosts are not targeted.</>
              )}
            </Text>
            <Alert status={consequence.status} title={consequence.title}>
              {consequence.body}
            </Alert>
          </Stack>
          {dialog === 'uninstall' && (
            <>
              <PlatformUninstallChoices
                scope="single"
                idPrefix="uninstall"
                releaseServers={releaseServers}
                onReleaseServersChange={changeReleaseServers}
                releaseOptions={releaseOptions}
                onReleaseOptionsChange={setReleaseOptions}
                blockingWorkflows={blockingWorkflows}
                cancelBlocking={cancelBlocking}
                onCancelBlockingChange={setCancelBlocking}
                disabled={submitting}
              />
              <Text fontSize="sm" color="fg.muted">{platformUninstallDeleteHint(releaseServers, 'single')}</Text>
            </>
          )}
          <Field.Root required>
            <Field.Label>
              Type “{platform.name}” to confirm <Field.RequiredIndicator />
            </Field.Label>
            <Input
              ref={confirmationRef}
              value={confirmation}
              disabled={submitting}
              onChange={(event) => setConfirmation(event.target.value)}
              aria-label="Platform name confirmation"
            />
          </Field.Root>
        </Stack>
      </Modal>
    </>
  )
}
