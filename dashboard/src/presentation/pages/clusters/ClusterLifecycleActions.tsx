import { useState } from 'react'
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
} from '@patternfly/react-core'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { clusterUninstallDisabledReason } from '@/domain/cluster/lifecycle'
import type { Cluster } from '@/domain/cluster/types'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'

type ConfirmationAction = 'uninstall' | 'delete'

interface ClusterLifecycleActionsProps {
  cluster: Cluster
  targetCount?: number
}

/** Typed confirmations for the separate host-side uninstall and record-only delete actions. */
export function ClusterLifecycleActions({
  cluster,
  targetCount,
}: ClusterLifecycleActionsProps) {
  const { clusters } = useApp()
  const { showToast } = useToast()
  const { scopedHref } = useSiteScope()
  const navigate = useNavigate()
  const [menuOpen, setMenuOpen] = useState(false)
  const [action, setAction] = useState<ConfirmationAction | null>(null)
  const [confirmation, setConfirmation] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const uninstallDisabledReason = clusterUninstallDisabledReason(cluster)

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

  return (
    <>
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
