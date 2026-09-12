import { useRef, useState } from 'react'
import { Button, Field, Input, Menu, Portal, Stack, Text } from '@chakra-ui/react'
import { Redo } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { isOrchestrationOperation, type Operation } from '@/domain/operation/types'
import { platformUninstallDisabledReason } from '@/domain/platform/lifecycle'
import type { Platform, UninstallPlatformOptions } from '@/domain/platform/types'
import { ReleaseOptionsFields } from '@/presentation/components/ReleaseOptionsFields'
import { emptyReleaseOptions, type ReleaseOptionsValue } from '@/presentation/components/releaseOptions'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { Tooltip } from '@/presentation/components/ui/tooltip'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useTargetLockProtection } from '@/presentation/hooks/useTargetLockProtection'

type ConfirmationAction = 'uninstall' | 'delete'

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
 * Exposes Platform-scoped repair and the separate uninstall/delete lifecycle actions.
 *
 * Schema-v3 repair signals each retryable failed Step on the existing durable Operation,
 * preserving successful provider effects and protected inputs. Legacy deployment history keeps
 * the compatibility retry that creates a new Operation. The parent callback refreshes lifecycle
 * state without redirecting operators into the automation debugger.
 */
export function PlatformLifecycleActions({ platform, operation, targetServerIds, onRepairStarted }: PlatformLifecycleActionsProps) {
  const { platforms, operations } = useApp()
  const { showToast } = useToast()
  const { scopedHref } = useSiteScope()
  const navigate = useNavigate()
  const [action, setAction] = useState<ConfirmationAction | null>(null)
  const [confirmation, setConfirmation] = useState('')
  const confirmationRef = useRef<HTMLInputElement>(null)
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  // Uninstall-scoped choice to also release member servers back to the provider.
  const [releaseServers, setReleaseServers] = useState(false)
  const [releaseOptions, setReleaseOptions] = useState<ReleaseOptionsValue>(emptyReleaseOptions)
  const [repairOpen, setRepairOpen] = useState(false)
  const [repairError, setRepairError] = useState('')
  const [repairing, setRepairing] = useState(false)
  const targetProtection = useTargetLockProtection(targetServerIds)
  const targetCount = targetServerIds?.length

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
  const uninstallDisabledReason = platformUninstallDisabledReason(platform) ?? lockedDisabledReason
  const repairDisabledReason = !platform.lifecycleOperationId
    ? 'The deployment Operation is unavailable.'
    : legacyReadinessOnly
      ? 'This older readiness Step can only recheck SSH and cannot repair missing provider addresses. Release and redeploy the affected Servers.'
      : lockedDisabledReason

  const openConfirmation = (next: ConfirmationAction) => {
    setAction(next)
    setConfirmation('')
    setError('')
    setReleaseServers(false)
    setReleaseOptions(emptyReleaseOptions)
  }

  const close = () => {
    if (submitting) return
    setAction(null)
    setConfirmation('')
    setError('')
    setReleaseServers(false)
    setReleaseOptions(emptyReleaseOptions)
  }

  // The host-side software removed by uninstall depends on the platform type.
  const platformSoftware = platform.type === 'slurm' ? 'Slurm' : 'k0s'

  const submit = async () => {
    if (!action || confirmation !== platform.name || submitting) return
    setSubmitting(true)
    setError('')
    try {
      if (action === 'uninstall') {
        // Gate every erase/unbind choice on releaseServers so an unchecked release can never
        // submit stray options; also mirror the standalone Release rule that the erase modes
        // only apply when erase itself is on.
        const options: UninstallPlatformOptions = {
          releaseServers,
          releaseOptions: {
            erase: releaseServers && releaseOptions.erase,
            secureErase: releaseServers && releaseOptions.erase && releaseOptions.secureErase,
            quickErase: releaseServers && releaseOptions.erase && releaseOptions.quickErase,
            unbindStaticIps: releaseServers && releaseOptions.unbindStaticIPs,
          },
        }
        const accepted = await platforms.uninstallPlatform(platform.id, options)
        const cleaned = targetCount === undefined ? 'the original deployment targets' : `${targetCount} original deployment target${targetCount === 1 ? '' : 's'}`
        showToast({
          tone: 'success',
          title: 'Platform uninstall accepted',
          description: releaseServers ? `${platformSoftware} will be removed and ${cleaned} released to the provider.` : `${cleaned} will be cleaned.`,
        })
        navigate(scopedHref(`/workflows/${accepted.operationId}`))
        return
      }

      await platforms.deletePlatform(platform.id)
      showToast({ tone: 'success', title: 'Platform deleted', description: 'The Swallow record and owned projections were removed. Hosts were not changed.' })
      navigate(scopedHref('/platforms'), { replace: true })
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The platform action failed.')
    } finally {
      setSubmitting(false)
    }
  }

  const isUninstall = action === 'uninstall'
  const targetLabel = targetCount === undefined ? 'the original deployment targets' : `${targetCount} original deployment target${targetCount === 1 ? '' : 's'}`

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
      <Menu.Root>
        <Menu.Trigger asChild>
          <Button variant="outline">Platform actions</Button>
        </Menu.Trigger>
        <Portal>
          <Menu.Positioner>
            <Menu.Content>
              <Menu.Item value="uninstall" disabled={Boolean(uninstallDisabledReason)} onClick={() => openConfirmation('uninstall')}>
                <Text>
                  Uninstall platform
                  {uninstallDisabledReason && (
                    <Text as="span" display="block" fontSize="xs" color="fg.muted">
                      {uninstallDisabledReason}
                    </Text>
                  )}
                </Text>
              </Menu.Item>
              <Menu.Item value="delete" color="red.fg" onClick={() => openConfirmation('delete')}>
                Delete platform
              </Menu.Item>
            </Menu.Content>
          </Menu.Positioner>
        </Portal>
      </Menu.Root>

      <Modal
        open={repairOpen}
        onClose={() => !repairing && setRepairOpen(false)}
        closeOnInteractOutside={!repairing}
        title="Repair platform deployment"
        description="Rerun the original deployment safely against its existing partial state."
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
            Repair reuses the same machines, roles, network settings, and protected credentials. Existing attempts, events, and logs remain available for diagnosis.
            {failedProvisioningRecovery && ' Swallow rechecks each failed target first. A target with no MAAS address is released, returned to Ready, and redeployed; an SSH-only failure is only rechecked.'}
          </Alert>
          <Text>
            <strong>Targets:</strong> {targetLabel}
          </Text>
          <Text>Successful Steps are preserved. Only failed retryable work resumes from the hosts' current state.</Text>
        </Stack>
      </Modal>

      <Modal
        open={action !== null}
        onClose={close}
        size={releaseServers ? 'lg' : 'md'}
        closeOnInteractOutside={!submitting}
        initialFocusEl={() => confirmationRef.current}
        title={isUninstall ? 'Uninstall platform' : 'Delete platform'}
        description={
          isUninstall
            ? `This removes ${platformSoftware} from ${targetLabel} and keeps the Swallow record.`
            : 'This removes only the Swallow record and owned projections.'
        }
        onSubmit={(event) => {
          event.preventDefault()
          void submit()
        }}
        footer={
          <>
            <Button variant="ghost" onClick={close} disabled={submitting}>
              Cancel
            </Button>
            <Button type="submit" colorPalette="red" loading={submitting} disabled={confirmation !== platform.name || submitting}>
              {isUninstall ? 'Uninstall platform' : 'Delete platform'}
            </Button>
          </>
        }
      >
        <Stack gap="4">
          {error && (
            <Alert status="error" title="Action failed">
              {error}
            </Alert>
          )}
          {isUninstall ? (
            <>
              <Text>
                {platform.type === 'slurm'
                  ? 'Slurm services (slurmctld, slurmd, slurmrestd), configuration, the MUNGE and JWT keys, and controller state will be removed. The operating system and the image-supplied Slurm packages remain installed unless you also release the servers. Hosts are not rebooted.'
                  : 'k0s services, state, configuration, join tokens, temporary installer, and binary will be removed. The operating system, user data, and shared packages remain installed unless you also release the servers. Hosts are not rebooted.'}
              </Text>
              <Text>
                <strong>Targets:</strong> {targetLabel}
              </Text>
              <Checkbox
                id="uninstall-release-servers"
                checked={releaseServers}
                onCheckedChange={(checked) => {
                  setReleaseServers(checked)
                  if (!checked) setReleaseOptions(emptyReleaseOptions)
                }}
              >
                Also release servers back to the provider
              </Checkbox>
              {releaseServers && (
                <>
                  <Alert status="error" title="Servers will be wiped and returned to the provider">
                    After k0s is removed, each target server is released to the provider in the same operation. This removes its deployed operating system;
                    the servers leave this platform and return to the available pool, and host exporters are not restored.
                  </Alert>
                  <ReleaseOptionsFields idPrefix="uninstall-release" value={releaseOptions} onChange={setReleaseOptions} />
                </>
              )}
            </>
          ) : (
            <Alert status="warning" title="Hosts will not be uninstalled">
              Any platform still running on the hosts will continue to run. Accepted operations will also continue after this record is deleted.
            </Alert>
          )}
          <Field.Root required>
            <Field.Label>
              Type "{platform.name}" to confirm <Field.RequiredIndicator />
            </Field.Label>
            <Input ref={confirmationRef} value={confirmation} onChange={(event) => setConfirmation(event.target.value)} aria-label="Platform name confirmation" />
          </Field.Root>
        </Stack>
      </Modal>
    </>
  )
}
