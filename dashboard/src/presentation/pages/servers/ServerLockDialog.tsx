import { Button, List, Stack, Text } from '@chakra-ui/react'
import { Lock, Unlock } from 'lucide-react'
import type { Server } from '@/domain/server/types'
import { serverDisplayName } from '@/domain/server/list'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { useExperimentalFeature } from '@/presentation/contexts/ExperimentalFeaturesContext'

interface ServerLockDialogProps {
  action: 'lock' | 'unlock'
  targets: readonly Server[]
  skipped: readonly Server[]
  busy?: boolean
  onConfirm: () => void
  onClose: () => void
}

/**
 * Confirms provider-owned Server protection for both single and convergent bulk actions.
 * The reassurance about what stays readable names metrics and alerts only while the
 * console offers monitoring, so a release build does not promise a surface it hides.
 */
export function ServerLockDialog({ action, targets, skipped, busy = false, onConfirm, onClose }: ServerLockDialogProps) {
  const monitoring = useExperimentalFeature('monitoring')
  const locking = action === 'lock'
  const title = locking ? 'Lock Servers' : 'Unlock Servers'
  const readOnlyViews = monitoring
    ? 'Metrics, alerts, inventory refresh, events, and other read-only views remain available.'
    : 'Inventory refresh, events, and other read-only views remain available.'
  return (
    <Modal
      open
      onClose={onClose}
      closeOnInteractOutside={!busy}
      title={title}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button colorPalette={locking ? 'orange' : 'brand'} loading={busy} disabled={busy || targets.length === 0} onClick={onConfirm}>
            {locking ? <Lock size={16} /> : <Unlock size={16} />}
            {locking ? 'Lock' : 'Unlock'}
          </Button>
        </>
      }
    >
      <Stack gap="3">
        <Alert
          status="info"
          title={locking ? (monitoring ? 'Monitoring and diagnostics remain available' : 'Diagnostics remain available') : 'No work starts automatically'}
        >
          {locking
            ? `Lock blocks provisioning, power, network, platform, automation, and removal changes. ${readOnlyViews}`
            : 'Unlock removes protection only. It does not resume, retry, or create any work. Review the Server state and explicitly start the operation you need.'}
        </Alert>
        <Text>
          <strong>{targets.length}</strong> Server{targets.length === 1 ? '' : 's'} will be {locking ? 'locked' : 'unlocked'}.
        </Text>
        <List.Root ps="4">
          {targets.slice(0, 8).map((server) => (
            <List.Item key={server.id}>{serverDisplayName(server)}</List.Item>
          ))}
        </List.Root>
        {targets.length > 8 && <Text>and {targets.length - 8} more</Text>}
        {skipped.length > 0 && (
          <Text>
            {skipped.length} Server{skipped.length === 1 ? '' : 's'} already {locking ? 'locked' : 'unlocked'} will be skipped.
          </Text>
        )}
        {locking && <Text color="fg.muted">Lock will be refused if provider lifecycle work, an Operation, or a Provisioning Task is active.</Text>}
      </Stack>
    </Modal>
  )
}
