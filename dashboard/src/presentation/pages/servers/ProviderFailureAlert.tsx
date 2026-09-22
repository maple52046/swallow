import { useState } from 'react'
import { Box, Button, List, Stack, Text } from '@chakra-ui/react'
import type { ProvisioningAxis } from '@/domain/server/types'
import { Alert } from '@/presentation/components/ui/alert'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useServerActiveOperations } from './useServerActiveOperations'

/**
 * Presents a provider-lifecycle failure (`failed` / `broken` / `rescue`) on the Server detail page
 * with everything an operator needs to act, rather than a coarse state label:
 *
 * - the provisioner's own machine-level reason (e.g. "Failed to erase disks."), which is otherwise
 *   only buried in the provider event log;
 * - state- and reason-specific guidance (a failed disk erase is recovered without wiping);
 * - the durable Operations still active on this Server, which block a new recovery with a 409 —
 *   the trap where a parked failed Release keeps the operator from recovering; and
 * - a single Recover action that cancels those blocking Operations first when present, so the
 *   operator is not left to discover the busy state and cancel by hand.
 */
export function ProviderFailureAlert({
  serverId,
  provisioning,
  onRecoverStarted,
}: {
  serverId: string
  provisioning: ProvisioningAxis
  /** Called after a recovery is accepted so the detail page can follow the Server in place. */
  onRecoverStarted: () => void
}) {
  const { provisioning: repo } = useApp()
  const { showToast } = useToast()
  const { activeOperations, cancelAll, refresh } = useServerActiveOperations([serverId])
  const [busy, setBusy] = useState(false)

  const blocking = activeOperations.length > 0
  const { status, title, guidance } = describeFailure(provisioning)
  const reason = provisioning.errorDescription?.trim()
  const locked = provisioning.locked

  const recover = async () => {
    setBusy(true)
    try {
      // A parked Operation (for example a failed Release) keeps this Server "busy", so the backend
      // rejects a new recovery with 409. Cancel those first so the one-click recovery actually runs.
      if (blocking) await cancelAll()
      await repo.createRecoverOperation({ serverIds: [serverId] })
      showToast({ tone: 'success', title: 'Recover Operation created', description: 'Swallow will return the Server to the ready pool.' })
      onRecoverStarted()
    } catch (error) {
      showToast({ tone: 'error', title: 'Recovery could not start', description: error instanceof Error ? error.message : 'Try again in a moment.' })
      void refresh()
    } finally {
      setBusy(false)
    }
  }

  return (
    <Alert status={status} title={title}>
      <Stack gap="2">
        {reason && (
          <Text>
            <strong>Provider reason:</strong> {reason}
          </Text>
        )}
        <Text>{guidance}</Text>
        {blocking && (
          <Box>
            <Text>
              {activeOperations.length === 1
                ? 'One Operation is still active on this Server and blocks recovery:'
                : `${activeOperations.length} Operations are still active on this Server and block recovery:`}
            </Text>
            <List.Root aria-label="Operations blocking recovery">
              {activeOperations.map((operation) => (
                <List.Item key={operation.id}>
                  {operation.intent} — {operation.status}
                </List.Item>
              ))}
            </List.Root>
          </Box>
        )}
        {locked && <Text>The Server is locked; unlock it before recovering.</Text>}
        <Box>
          <Button colorPalette="brand" size="sm" loading={busy} disabled={locked} onClick={recover}>
            {blocking ? 'Cancel blocking Operations and Recover' : 'Recover (Return to Ready)'}
          </Button>
        </Box>
      </Stack>
    </Alert>
  )
}

/**
 * Chooses the alert tone, title, and guidance for a provider-lifecycle failure. A failed disk
 * erase gets specific guidance because Recover returns the Server to Ready without wiping (and
 * escalates to Mark fixed when a plain release cannot complete), which is exactly the escape an
 * operator cannot infer from "Failed to erase disks." alone.
 */
function describeFailure(provisioning: ProvisioningAxis): {
  status: 'error' | 'warning' | 'info'
  title: string
  guidance: string
} {
  if (provisioning.state === 'rescue') {
    return {
      status: 'info',
      title: 'Server is in rescue mode',
      guidance:
        'Rescue mode is a diagnostic environment. Exiting rescue restores the previous state; Recover returns the Server to Ready.',
    }
  }
  if (provisioning.state === 'broken') {
    return {
      status: 'warning',
      title: 'Provider marked this Server broken',
      guidance:
        'Recover returns the Server to Ready, which clears the broken flag; Release also returns it to the ready pool.',
    }
  }
  const eraseFailure = /eras/i.test(provisioning.errorDescription ?? '') || /eras/i.test(provisioning.providerState)
  return {
    status: 'error',
    title: 'Provider lifecycle failed',
    guidance: eraseFailure
      ? 'A disk erase could not complete. Recover returns the Server to Ready without erasing its disks — if a plain release cannot complete either, recovery escalates to Mark fixed, which returns it to Ready without a wipe. Mark fixed alone does not apply to a failed Server.'
      : 'Recover returns the Server to Ready. Mark fixed does not apply to a failed Server — it only clears a Broken one.',
  }
}
