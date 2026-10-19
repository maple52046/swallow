import { useState } from 'react'
import { Box, Button, Link, List, Stack, Text } from '@chakra-ui/react'
import { Link as RouterLink } from 'react-router-dom'
import type { ProvisioningAxis } from '@/domain/server/types'
import { Alert } from '@/presentation/components/ui/alert'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useServerActiveOperations } from './useServerActiveOperations'

/**
 * Presents a provider-lifecycle failure (`failed` / `broken` / `rescue`) on the Server detail page
 * with everything an operator needs to act, rather than a coarse state label:
 *
 * - the provisioner's own machine-level reason (e.g. "Failed to erase disks."), which is otherwise
 *   only buried in the provider event log;
 * - state- and reason-specific guidance (a failed disk erase is recovered without wiping);
 * - the durable Operations still active on this Server, which block a new recovery with a 409 —
 *   the trap where a parked failed Release keeps the operator from recovering. Each links to its
 *   Workflow, because when the blocking work is the deployment that failed here, retrying its
 *   failed Step returns this Server to Ready and redeploys it without stopping the rest of that
 *   work (for example the other Servers of a platform deployment). A Server whose layout needs a
 *   new inspection (`inspectionRequired`) is the exception: the retry ends the Step instead, so
 *   the guidance says that rather than promising a redeploy; and
 * - a single Recover action that has the backend cancel those blocking Operations when present
 *   (decision 061): the Recover is accepted at once and starts after they end, so the operator is
 *   neither left to cancel by hand nor kept waiting here. It stops the whole Operation, so it is
 *   the choice for abandoning that work, not for redeploying this Server.
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
  const { scopedHref } = useSiteScope()
  const { activeOperations, refresh } = useServerActiveOperations([serverId])
  const [busy, setBusy] = useState(false)

  const blocking = activeOperations.length > 0
  const { status, title, guidance } = describeFailure(provisioning)
  const reason = provisioning.errorDescription?.trim()
  const locked = provisioning.locked

  const recover = async () => {
    setBusy(true)
    try {
      // A parked Operation (for example a failed Release) keeps this Server "busy", so the backend
      // rejects a new recovery with 409. Asking the backend to cancel it (decision 061) accepts the
      // Recover at once; it starts after the canceled work ends, so nothing here waits for that.
      await repo.createRecoverOperation({ serverIds: [serverId], cancelActiveWorkflows: blocking || undefined })
      showToast({
        tone: 'success',
        title: 'Recover Operation created',
        description: blocking
          ? 'The blocking operations are being canceled; Swallow returns the Server to the ready pool once they end.'
          : 'Swallow will return the Server to the ready pool.',
      })
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
                  <Link asChild colorPalette="brand" fontWeight="medium">
                    <RouterLink to={scopedHref(`/workflows/${operation.id}`)}>{operation.intent}</RouterLink>
                  </Link>{' '}
                  — {operation.status}
                </List.Item>
              ))}
            </List.Root>
            <Text mt="2">
              {/* A retry of a disk deployment onto a layout that needs a new inspection is refused
                  before Recover, so promising a redeploy there would send the operator in a loop. */}
              {provisioning.inspectionRequired
                ? 'This Server must be inspected again before a disk deployment, so retrying the deployment that failed here ends its Step with that reason instead of redeploying; once no other Step waits for a retry, the Operation ends and frees the Server for Inspect. The button below cancels the whole Operation instead.'
                : 'If one of them is the deployment that failed on this Server, open it and retry the failed Step: Swallow returns this Server to Ready and redeploys it, and keeps the rest of that work. The button below cancels the whole Operation instead.'}
            </Text>
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
