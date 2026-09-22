import { useState } from 'react'
import { Button, List, Stack, Text } from '@chakra-ui/react'
import { PowerOff } from 'lucide-react'
import type { Server } from '@/domain/server/types'
import { serverDisplayName } from '@/domain/server/list'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'

/**
 * Confirms a destructive power off when one or more targets run a RAM (ephemeral) deployment.
 *
 * Shared by the Server list (bulk, row, and power-tile dialog) and the Server detail action menu
 * so the warning never drifts between surfaces. A RAM (ephemeral) deployment runs the OS from
 * memory and leaves the disks untouched, so anything written to the Server is lost on power off or
 * reboot; the OS itself is unaffected because provisioning re-provides the same OS on the next
 * boot. Because that data loss is irreversible, this dialog is only raised when at least one target
 * is RAM-deployed; a disk deployment keeps its data, so a disk-only power off stays one click.
 *
 * The double-check is an explicit acknowledgement checkbox that gates the confirm button, so the
 * irreversible data loss cannot be triggered by a single stray click. Danger is carried by the
 * text and the acknowledgement, never by colour alone.
 */
export function ServerPowerOffWarningDialog({
  ramTargets,
  totalTargets,
  busy = false,
  onConfirm,
  onClose,
}: {
  /** The RAM-deployed Servers among the power-off targets; anything written to these is lost. */
  ramTargets: readonly Server[]
  /** Total Servers in the power-off request, so the copy can distinguish "all" from "some". */
  totalTargets: number
  busy?: boolean
  onConfirm: () => void
  onClose: () => void
}) {
  const [acknowledged, setAcknowledged] = useState(false)
  const ramCount = ramTargets.length
  const mixed = ramCount < totalTargets

  return (
    <Modal
      open
      onClose={onClose}
      role="alertdialog"
      closeOnInteractOutside={!busy}
      title="Power off RAM-deployed Servers?"
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button
            colorPalette="red"
            loading={busy}
            disabled={busy || !acknowledged}
            onClick={onConfirm}
          >
            <PowerOff size={16} />
            Power off
          </Button>
        </>
      }
    >
      <Stack gap="4">
        <Alert status="warning" title="Data on this Server will be lost">
          A RAM (ephemeral) deployment keeps no persistent disk: the OS runs from memory and the
          disks are left untouched. Anything written to the Server is lost on power off or reboot.
          Provisioning re-provides the same OS on the next boot, so the OS is unaffected — only the
          data written to it is lost, and it cannot be recovered. A disk deployment, by contrast,
          keeps its data across a power cycle.
        </Alert>
        <Stack gap="1">
          <Text>
            <strong>{ramCount}</strong> RAM-deployed Server{ramCount === 1 ? '' : 's'} will be powered off
            {mixed ? ` (of ${totalTargets} selected)` : ''}:
          </Text>
          <List.Root ps="4">
            {ramTargets.slice(0, 8).map((server) => (
              <List.Item key={server.id}>{serverDisplayName(server)}</List.Item>
            ))}
          </List.Root>
          {ramCount > 8 && <Text color="fg.muted">and {ramCount - 8} more</Text>}
        </Stack>
        <Checkbox checked={acknowledged} onCheckedChange={setAcknowledged}>
          I understand any data written to {ramCount === 1 ? 'this Server' : 'these Servers'} will be lost.
        </Checkbox>
      </Stack>
    </Modal>
  )
}
