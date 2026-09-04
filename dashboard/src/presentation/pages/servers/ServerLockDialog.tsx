import {
  Alert,
  AlertVariant,
  Button,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
} from '@patternfly/react-core'
import { LockIcon, UnlockAltIcon } from '@patternfly/react-icons'
import type { Server } from '@/domain/server/types'
import { serverDisplayName } from '@/domain/server/list'

interface ServerLockDialogProps {
  action: 'lock' | 'unlock'
  targets: readonly Server[]
  skipped: readonly Server[]
  busy?: boolean
  onConfirm: () => void
  onClose: () => void
}

/** Confirms provider-owned Server protection for both single and convergent bulk actions. */
export function ServerLockDialog({
  action,
  targets,
  skipped,
  busy = false,
  onConfirm,
  onClose,
}: ServerLockDialogProps) {
  const locking = action === 'lock'
  const title = locking ? 'Lock Servers' : 'Unlock Servers'
  return (
    <Modal isOpen onClose={onClose} variant="medium" aria-labelledby="server-lock-title">
      <ModalHeader
        title={title}
        titleIconVariant={locking ? 'warning' : 'info'}
        labelId="server-lock-title"
        description={locking
          ? 'Lock protects the Machine from provisioning, power, network, platform, automation, and removal changes.'
          : 'Unlock removes protection only. It does not resume, retry, or create any work.'}
      />
      <ModalBody>
        <div className="sw-confirmation-content">
          <Alert
            variant={AlertVariant.info}
            isInline
            title={locking ? 'Monitoring and diagnostics remain available' : 'No work starts automatically'}
          >
            {locking
              ? 'Metrics, alerts, inventory refresh, events, and other read-only views continue while locked.'
              : 'Review the Server state and explicitly start the operation you need after unlocking.'}
          </Alert>
          <p><strong>{targets.length}</strong> Server{targets.length === 1 ? '' : 's'} will be {locking ? 'locked' : 'unlocked'}.</p>
          <ul>
            {targets.slice(0, 8).map((server) => <li key={server.id}>{serverDisplayName(server)}</li>)}
          </ul>
          {targets.length > 8 && <p>and {targets.length - 8} more</p>}
          {skipped.length > 0 && (
            <p>{skipped.length} Server{skipped.length === 1 ? '' : 's'} already {locking ? 'locked' : 'unlocked'} will be skipped.</p>
          )}
          {locking && <p>Lock will be refused if provider lifecycle work, an Operation, or a Provisioning Task is active.</p>}
        </div>
      </ModalBody>
      <ModalFooter>
        <Button
          variant={locking ? 'warning' : 'primary'}
          icon={locking ? <LockIcon /> : <UnlockAltIcon />}
          isLoading={busy}
          isDisabled={busy || targets.length === 0}
          onClick={onConfirm}
          autoFocus
        >
          {locking ? 'Lock' : 'Unlock'}
        </Button>
        <Button variant="link" onClick={onClose} isDisabled={busy}>Cancel</Button>
      </ModalFooter>
    </Modal>
  )
}
