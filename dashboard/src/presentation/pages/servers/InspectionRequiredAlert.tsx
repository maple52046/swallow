import { useState } from 'react'
import { Box, Button, Stack, Text } from '@chakra-ui/react'
import type { ProvisionerCapabilities, Server } from '@/domain/server/types'
import { Alert } from '@/presentation/components/ui/alert'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/toast/toastContext'
import { serverActionAvailability } from './serverActions'

/**
 * Tells the operator on the Server detail page that a disk deployment is refused until the Server is
 * inspected again (`provisioning.inspectionRequired`, servers-list.md), and offers that inspection in
 * place. The typical cause is a UEFI Server whose provisioner storage layout was built while it booted
 * BIOS: every disk deployment would fail while partitioning, so Swallow refuses it up front instead.
 *
 * The explanation is the Swallow-authored `inspectionRequiredReason`, never provider text. Inspect runs
 * the same action and lock gate as the Take-action menu; the backend still applies its own inspection
 * policy (for example a deployed Server must be released first) and that refusal is shown as a toast.
 * While an inspection already runs, the action is replaced by a note, because the flag clears only when
 * the inspection finishes. The action hides when the provisioner cannot inspect hardware. The warning
 * tone is backed by the title text, never colour alone.
 */
export function InspectionRequiredAlert({
  server,
  capabilities,
  onInspectStarted,
}: {
  server: Server
  /** Live provisioner capabilities; the Inspect action needs `hardwareValidation`. */
  capabilities: ProvisionerCapabilities | null
  /** Called after the inspection is accepted so the detail page can follow the Server in place. */
  onInspectStarted: () => void
}) {
  const { servers } = useApp()
  const { showToast } = useToast()
  const [busy, setBusy] = useState(false)
  const provisioning = server.status.provisioning
  if (!provisioning?.inspectionRequired) return null

  const inspecting = provisioning.state === 'inspecting'
  const canInspect = Boolean(capabilities?.hardwareValidation) && !inspecting
  const { disabledReason } = serverActionAvailability('inspect', [server])
  const reason =
    provisioning.inspectionRequiredReason?.trim() ||
    "The provisioner's storage layout for this Server cannot be used for a disk deployment."

  const inspect = async () => {
    setBusy(true)
    try {
      await servers.runServerAction(server.id, 'inspect')
      showToast({
        tone: 'success',
        title: 'Inspection started',
        description: 'The provisioner rebuilds the storage layout; disk deployment is allowed again when it finishes.',
      })
      onInspectStarted()
    } catch (error) {
      showToast({
        tone: 'error',
        title: 'Inspection could not start',
        description: error instanceof Error ? error.message : 'Try again in a moment.',
      })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Alert status="warning" title="Inspect this Server before a disk deployment">
      <Stack gap="2">
        <Text>{reason}</Text>
        <Text>
          Swallow refuses disk deployments until a new inspection rebuilds the storage layout. A RAM deployment
          does not use the disk and is not affected.
        </Text>
        {inspecting && <Text>An inspection is running; this warning clears when it finishes.</Text>}
        {canInspect && (
          <Box>
            <Button colorPalette="brand" size="sm" loading={busy} disabled={Boolean(disabledReason)} onClick={inspect}>
              Inspect hardware
            </Button>
            {disabledReason && (
              <Text mt="1" fontSize="sm" color="fg.muted">
                {disabledReason}
              </Text>
            )}
          </Box>
        )}
      </Stack>
    </Alert>
  )
}
