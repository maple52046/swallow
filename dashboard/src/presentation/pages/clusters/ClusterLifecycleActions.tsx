import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  Dropdown,
  DropdownItem,
  DropdownList,
  FormGroup,
  MenuToggle,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  TextInput,
  Tooltip,
} from '@patternfly/react-core'
import { RedoIcon } from '@patternfly/react-icons'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { clusterUninstallDisabledReason } from '@/domain/cluster/lifecycle'
import type { Cluster } from '@/domain/cluster/types'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'

type ConfirmationAction = 'uninstall' | 'delete'

interface ClusterLifecycleActionsProps {
  cluster: Cluster
  targetServerIds?: readonly string[]
  onRepairStarted: () => void
}

/**
 * Exposes Cluster-scoped repair and the separate uninstall/delete lifecycle actions.
 *
 * Repair deliberately delegates to the durable Operation Retry contract so original
 * deployment inputs and secret variables are retained. The parent callback refreshes
 * Cluster lifecycle state without redirecting operators into the automation debugger.
 */
export function ClusterLifecycleActions({
  cluster,
  targetServerIds,
  onRepairStarted,
}: ClusterLifecycleActionsProps) {
  const { clusters, operations, servers } = useApp()
  const { showToast } = useToast()
  const { scopedHref } = useSiteScope()
  const navigate = useNavigate()
  const [menuOpen, setMenuOpen] = useState(false)
  const [action, setAction] = useState<ConfirmationAction | null>(null)
  const [confirmation, setConfirmation] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [repairOpen, setRepairOpen] = useState(false)
  const [repairError, setRepairError] = useState('')
  const [repairing, setRepairing] = useState(false)
  const [targetProtection, setTargetProtection] = useState<{
    targetKey: string
    lockedNames: string[]
    error?: string
  }>({ targetKey: '', lockedNames: [] })
  const targetKey = useMemo(
    () => [...(targetServerIds ?? [])].sort().join(','),
    [targetServerIds],
  )
  const targetCount = targetServerIds?.length

  useEffect(() => {
    const ids = targetKey ? targetKey.split(',') : []
    let cancelled = false
    Promise.all(ids.map((id) => servers.getServer(id)))
      .then((targets) => {
        if (cancelled) return
        setTargetProtection({
          targetKey,
          lockedNames: targets.flatMap((server) => (
            server?.provisioning?.locked ? [server.hostname || server.id] : []
          )),
        })
      })
      .catch(() => {
        if (!cancelled) {
          setTargetProtection({
            targetKey,
            lockedNames: [],
            error: 'Target protection could not be checked. Refresh and try again.',
          })
        }
      })
    return () => {
      cancelled = true
    }
  }, [servers, targetKey, targetServerIds])

  const lockedDisabledReason = targetProtection.targetKey !== targetKey
    ? 'Checking target protection.'
    : targetProtection.error
      ? targetProtection.error
      : targetProtection.lockedNames.length > 0
        ? `${targetProtection.lockedNames.join(', ')} ${targetProtection.lockedNames.length === 1 ? 'is' : 'are'} locked. Unlock ${targetProtection.lockedNames.length === 1 ? 'it' : 'them'} before changing this Cluster.`
        : undefined
  const uninstallDisabledReason = clusterUninstallDisabledReason(cluster) ?? lockedDisabledReason
  const repairDisabledReason = !cluster.lifecycleOperationId
    ? 'The deployment Operation is unavailable.'
    : lockedDisabledReason

  const openConfirmation = (next: ConfirmationAction) => {
    setMenuOpen(false)
    setAction(next)
    setConfirmation('')
    setError('')
  }

  const close = () => {
    if (submitting) return
    setAction(null)
    setConfirmation('')
    setError('')
  }

  const submit = async () => {
    if (!action || confirmation !== cluster.name || submitting) return
    setSubmitting(true)
    setError('')
    try {
      if (action === 'uninstall') {
        const accepted = await clusters.uninstallCluster(cluster.id)
        showToast({
          tone: 'success',
          title: 'Cluster uninstall accepted',
          description: targetCount === undefined
            ? 'The original deployment targets will be cleaned.'
            : `${targetCount} original deployment target${targetCount === 1 ? '' : 's'} will be cleaned.`,
        })
        navigate(scopedHref(`/operations/${accepted.operationId}`))
        return
      }

      await clusters.deleteCluster(cluster.id)
      showToast({
        tone: 'success',
        title: 'Cluster deleted',
        description: 'The Swallow record and owned projections were removed. Hosts were not changed.',
      })
      navigate(scopedHref('/clusters'), { replace: true })
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The cluster action failed.')
    } finally {
      setSubmitting(false)
    }
  }

  const isUninstall = action === 'uninstall'
  const targetLabel = targetCount === undefined
    ? 'the original deployment targets'
    : `${targetCount} original deployment target${targetCount === 1 ? '' : 's'}`

  const startRepair = async () => {
    if (!cluster.lifecycleOperationId || repairing) return
    setRepairing(true)
    setRepairError('')
    try {
      const created = await operations.retryOperation(cluster.lifecycleOperationId)
      setRepairOpen(false)
      showToast({
        tone: 'success',
        title: 'Cluster repair started',
        description: `Operation ${created.id} is rerunning the original deployment configuration.`,
      })
      onRepairStarted()
    } catch (caught) {
      setRepairError(caught instanceof Error ? caught.message : 'Could not start cluster repair.')
    } finally {
      setRepairing(false)
    }
  }

  return (
    <>
      {cluster.lifecycleState === 'deploy_failed' && (
        <Tooltip content={repairDisabledReason ?? 'Rerun the original deployment configuration'}>
          <span>
            <Button
              icon={<RedoIcon />}
              onClick={() => {
                setRepairError('')
                setRepairOpen(true)
              }}
              isDisabled={Boolean(repairDisabledReason)}
              aria-label={repairDisabledReason
                ? `Repair deployment: ${repairDisabledReason}`
                : 'Repair deployment'}
            >
              Repair deployment
            </Button>
          </span>
        </Tooltip>
      )}
      <Dropdown
        isOpen={menuOpen}
        onOpenChange={setMenuOpen}
        toggle={(ref) => (
          <MenuToggle
            ref={ref}
            isExpanded={menuOpen}
            onClick={() => setMenuOpen((value) => !value)}
          >
            Cluster actions
          </MenuToggle>
        )}
      >
        <DropdownList>
          <DropdownItem
            isDisabled={Boolean(uninstallDisabledReason)}
            description={uninstallDisabledReason}
            onClick={() => openConfirmation('uninstall')}
          >
            Uninstall cluster
          </DropdownItem>
          <DropdownItem isDanger onClick={() => openConfirmation('delete')}>
            Delete cluster
          </DropdownItem>
        </DropdownList>
      </Dropdown>

      <Modal
        isOpen={repairOpen}
        onClose={() => {
          if (!repairing) setRepairOpen(false)
        }}
        variant="small"
        aria-labelledby="cluster-repair-title"
      >
        <ModalHeader
          title="Repair cluster deployment"
          labelId="cluster-repair-title"
          description="Rerun the original deployment safely against its existing partial state."
        />
        <ModalBody>
          {repairError && (
            <Alert variant={AlertVariant.danger} title="Repair could not start" isInline>
              {repairError}
            </Alert>
          )}
          <Alert variant={AlertVariant.info} title="Original configuration will be reused" isInline>
            Repair creates a new Operation with the same machines, roles, network settings,
            and protected credentials. The failed Operation and its logs remain available.
          </Alert>
          <p><strong>Targets:</strong> {targetLabel}</p>
          <p>
            Completed idempotent steps are checked again. Remaining deployment work resumes
            from the hosts' current state.
          </p>
        </ModalBody>
        <ModalFooter>
          <Button
            onClick={() => void startRepair()}
            isLoading={repairing}
            isDisabled={repairing || Boolean(repairDisabledReason)}
          >
            Repair deployment
          </Button>
          <Button
            variant="link"
            onClick={() => setRepairOpen(false)}
            isDisabled={repairing}
          >
            Cancel
          </Button>
        </ModalFooter>
      </Modal>

      <Modal
        isOpen={action !== null}
        onClose={close}
        variant="small"
        aria-labelledby="cluster-lifecycle-confirmation-title"
      >
        <ModalHeader
          title={isUninstall ? 'Uninstall cluster' : 'Delete cluster'}
          labelId="cluster-lifecycle-confirmation-title"
          description={
            isUninstall
              ? `This removes k0s from ${targetLabel} and keeps the Swallow record.`
              : 'This removes only the Swallow record and owned projections.'
          }
        />
        <ModalBody>
          {error && (
            <Alert variant={AlertVariant.danger} title="Action failed" isInline>
              {error}
            </Alert>
          )}
          {isUninstall ? (
            <>
              <p>
                k0s services, state, configuration, join tokens, temporary installer, and
                binary will be removed. The operating system, user data, and shared packages
                remain installed. Hosts are not rebooted.
              </p>
              <p><strong>Targets:</strong> {targetLabel}</p>
            </>
          ) : (
            <Alert variant={AlertVariant.warning} title="Hosts will not be uninstalled" isInline>
              Any cluster still running on the hosts will continue to run. Accepted operations
              will also continue after this record is deleted.
            </Alert>
          )}
          <FormGroup
            label={`Type "${cluster.name}" to confirm`}
            isRequired
            fieldId="cluster-lifecycle-confirmation"
          >
            <TextInput
              id="cluster-lifecycle-confirmation"
              value={confirmation}
              onChange={(_event, value) => setConfirmation(value)}
              autoFocus
              aria-label="Cluster name confirmation"
              onKeyDown={(event) => {
                if (event.key === 'Enter') void submit()
              }}
            />
          </FormGroup>
        </ModalBody>
        <ModalFooter>
          <Button
            variant="danger"
            onClick={() => void submit()}
            isLoading={submitting}
            isDisabled={confirmation !== cluster.name || submitting}
          >
            {isUninstall ? 'Uninstall cluster' : 'Delete cluster'}
          </Button>
          <Button variant="link" onClick={close} isDisabled={submitting}>
            Cancel
          </Button>
        </ModalFooter>
      </Modal>
    </>
  )
}
