import { useState } from 'react'
import { Button, HStack, List, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import { serverDisplayName, type Server, type ServerBootMedia } from '@/domain/server/types'
import { bootOverrideLabel } from '@/presentation/components/serverSummary/bootMediaLabels'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'

interface ServerBootMediaDialogProps {
  server: Server
  bootMedia: ServerBootMedia
  /** `enable` runs the preflight; `disable` stops Boot Media and asks the BMC to reset. */
  mode: 'enable' | 'disable'
  onClose: () => void
  /** Called after the setting changed, so the caller re-reads Boot Media. */
  onChanged: () => void
}

/**
 * Enables or disables Boot Media on one Server from the Server Summary (decision 047).
 *
 * Enabling is a preflight run by the API against the real BMC: it probes Redfish, mounts the
 * installation's fixed ISO URL, directs the next boots at it, reads both back, and saves only
 * when that worked — so the request can take minutes, dismissal is blocked while it runs, and the
 * BMC's own explanation of a refusal is shown inline. Nothing reboots the Server: the next OS
 * deployment powers it on through the provisioner, after re-applying Boot Media.
 *
 * Disabling always saves; whether the BMC could also eject the ISO and clear the override is
 * reported in a toast. A locked Server explains why and disables both writes, matching the API.
 */
export function ServerBootMediaDialog({ server, bootMedia, mode, onClose, onChanged }: ServerBootMediaDialogProps) {
  const { servers } = useApp()
  const { showToast } = useToast()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const name = serverDisplayName(server)
  const locked = server.provisioning?.locked ?? false
  const enabling = mode === 'enable'

  const submit = async () => {
    if (busy || locked) return
    setBusy(true)
    setError('')
    try {
      const result = await servers.setBootMedia(server.id, enabling)
      if (enabling) {
        const persistence = bootOverrideLabel(result.setting?.bootOverride)
        showToast({
          tone: 'success',
          title: `Boot Media enabled on ${name}`,
          description: persistence
            ? `The BMC boots swallow's iPXE ISO first on ${persistence}.`
            : "The BMC boots swallow's iPXE ISO first.",
        })
      } else if (result.reverted) {
        showToast({ tone: 'success', title: `Boot Media disabled on ${name}`, description: 'The BMC ejected the ISO and stopped booting it first.' })
      } else {
        showToast({
          tone: 'warning',
          title: `Boot Media disabled on ${name}`,
          description: `OS deployments no longer apply it, but the BMC was not reset: ${result.revertError ?? 'it did not answer.'}`,
        })
      }
      onChanged()
      onClose()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The BMC could not be reached.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      onClose={() => !busy && onClose()}
      closeOnInteractOutside={!busy}
      title={enabling ? 'Enable Boot Media' : 'Disable Boot Media'}
      description={
        enabling
          ? `Make ${name}'s BMC boot swallow's iPXE ISO first, so it reaches the provisioner without the provisioner's DHCP.`
          : `Stop booting ${name} from swallow's iPXE ISO.`
      }
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <HStack gap="2">
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button
            type="submit"
            colorPalette={enabling ? 'brand' : 'red'}
            loading={busy}
            loadingText={enabling ? 'Applying through the BMC…' : 'Disabling…'}
            disabled={busy || locked}
          >
            {enabling ? 'Enable' : 'Disable'}
          </Button>
        </HStack>
      }
    >
      <Stack gap="4">
        {locked && (
          <Alert status="warning" title="Read-only while locked">
            {name} is locked. Unlock it to change its Boot Media.
          </Alert>
        )}
        {error && (
          <Alert status="error" title={enabling ? 'Boot Media was not enabled' : 'Boot Media was not disabled'}>
            {error}
          </Alert>
        )}
        {enabling ? (
          <>
            <Text fontSize="sm">swallow checks this on the real BMC before saving, because support differs by hardware and firmware:</Text>
            <List.Root as="ol" fontSize="sm" ps="5" gap="1">
              <List.Item>Probe the BMC&apos;s Redfish service.</List.Item>
              <List.Item>
                Mount <Text as="span" className="sw-mono">{bootMedia.image.url}</Text> as a virtual CD.
              </List.Item>
              <List.Item>Direct the next boots at that CD and read both back.</List.Item>
            </List.Root>
            <Text color="fg.muted" fontSize="sm">
              It can take a few minutes. The Server is not rebooted now. Because a BMC can lose the setting, every OS
              deployment of this Server applies it again before powering it on.
            </Text>
          </>
        ) : (
          <Text fontSize="sm">
            OS deployments stop applying Boot Media, and swallow asks the BMC to eject the ISO and stop booting it first. If the
            BMC does not answer, the setting is still disabled.
          </Text>
        )}
      </Stack>
    </Modal>
  )
}
