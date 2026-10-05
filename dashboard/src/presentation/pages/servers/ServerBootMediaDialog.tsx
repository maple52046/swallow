import { useRef, useState } from 'react'
import { Button, Field, HStack, Link, List, Stack, Text } from '@chakra-ui/react'
import { Link as RouterLink } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { BootISO } from '@/domain/provisioning/types'
import { serverDisplayName, type Server, type ServerBootMedia } from '@/domain/server/types'
import { BootMediaApplyProgress } from '@/presentation/components/serverSummary/BootMediaApplyProgress'
import { bootOverrideLabel } from '@/presentation/components/serverSummary/bootMediaLabels'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useAsyncData, type AsyncData } from '@/presentation/hooks/useAsyncData'

/**
 * What the dialog does:
 * - `enable` turns Boot Media on with a chosen Boot ISO;
 * - `change` switches an enabled Server to another Boot ISO;
 * - `reapply` mounts the current Boot ISO again (after a BMC lost it);
 * - `disable` stops Boot Media and asks the BMC to reset.
 *
 * The first three are the same API preflight with an `isoId`.
 */
export type ServerBootMediaDialogMode = 'enable' | 'change' | 'reapply' | 'disable'

interface ServerBootMediaDialogProps {
  server: Server
  bootMedia: ServerBootMedia
  mode: ServerBootMediaDialogMode
  onClose: () => void
  /** Called after the setting changed, so the caller re-reads Boot Media. */
  onChanged: () => void
  /**
   * Called when an enable preflight is sent and when its request ends (either way), so the caller
   * polls Boot Media for the preflight's progress in between and passes it back as `bootMedia`.
   */
  onApplyStarted?: () => void
  onApplySettled?: () => void
}

const TITLES: Record<ServerBootMediaDialogMode, string> = {
  enable: 'Enable Boot Media',
  change: 'Change Boot ISO',
  reapply: 'Re-apply Boot Media',
  disable: 'Disable Boot Media',
}

const SUBMIT_LABELS: Record<ServerBootMediaDialogMode, string> = {
  enable: 'Enable',
  change: 'Switch ISO',
  reapply: 'Re-apply',
  disable: 'Disable',
}

/**
 * Enables, switches, re-applies, or disables Boot Media on one Server from the Server Summary
 * (decisions 047 and 049).
 *
 * Applying is a preflight run by the API against the real BMC: it probes Redfish, mounts the
 * chosen Boot ISO, waits three minutes for the mount to settle, directs the next boots at it,
 * reads both back, and saves only when that worked — about five minutes. While it runs the
 * dialog shows its real progress (`bootMedia.apply`, which the caller polls) and may be closed:
 * the request continues, the Boot media block keeps showing the progress, and the outcome still
 * arrives as a toast, a failure included. A refusal while the dialog is open is shown inline with
 * the BMC's own explanation. Nothing reboots the Server: the next OS deployment powers it on
 * through the provisioner, after re-applying Boot Media.
 *
 * The Boot ISO choice offers only ISOs built for this Server's own provisioner (the API refuses
 * others), read when the dialog opens; with none, the dialog links to the Boot ISOs tab instead of
 * offering an empty list. Re-apply needs no choice unless the current ISO is gone or unavailable,
 * in which case it asks for one like Change does. Disabling always saves; whether the BMC could
 * also eject the ISO and clear the override is reported in a toast. A locked Server explains why
 * and disables every write, matching the API.
 */
export function ServerBootMediaDialog({ server, bootMedia, mode, onClose, onChanged, onApplyStarted, onApplySettled }: ServerBootMediaDialogProps) {
  const { servers, provisioning } = useApp()
  const { showToast } = useToast()
  const { scopedHref } = useSiteScope()
  const current = bootMedia.image
  const choosing = mode === 'enable' || mode === 'change' || (mode === 'reapply' && !current?.available)
  const isos = useAsyncData(
    async () => (choosing ? (await provisioning.listBootISOs({ integrationId: server.source.integrationId })).items : []),
    [provisioning, server.source.integrationId, choosing],
  )
  const [chosenId, setChosenId] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [requestedAt, setRequestedAt] = useState<number | undefined>(undefined)
  // Set when the operator closes the dialog while a preflight runs, so its outcome becomes a toast.
  const closedWhileBusy = useRef(false)
  const name = serverDisplayName(server)
  const locked = server.provisioning?.locked ?? false
  const disabling = mode === 'disable'
  const options = isos.status === 'ready' ? isos.data : []
  // The selection defaults to the current Boot ISO, else the only one offered; an explicit choice wins.
  const defaultId = options.some((iso) => iso.id === current?.id) ? current?.id ?? '' : options.length === 1 ? options[0].id : ''
  const selectedId = choosing ? chosenId || defaultId : current?.id ?? ''
  const selected: Pick<BootISO, 'id' | 'name' | 'url'> | undefined = choosing
    ? options.find((iso) => iso.id === selectedId)
    : current ?? undefined
  const unchanged = mode === 'change' && selectedId === current?.id
  const canSubmit = !busy && !locked && (disabling || (Boolean(selected) && !unchanged))

  // A preflight takes minutes and reports its progress, so the dialog may be closed while it runs;
  // a disable is short and shows no progress, so it keeps the dialog until it answers.
  const closable = !busy || !disabling
  const close = () => {
    if (!closable) return
    if (busy) closedWhileBusy.current = true
    onClose()
  }

  const submit = async () => {
    if (!canSubmit) return
    setBusy(true)
    setError('')
    if (!disabling) {
      setRequestedAt(Date.now())
      onApplyStarted?.()
    }
    try {
      const result = await servers.setBootMedia(server.id, !disabling, disabling ? undefined : selected?.id)
      if (!disabling) {
        const persistence = bootOverrideLabel(result.setting?.bootOverride)
        const iso = result.image?.name || selected?.name || 'the Boot ISO'
        showToast({
          tone: 'success',
          title: mode === 'change' ? `${name} now boots ${iso}` : `Boot Media ${mode === 'reapply' ? 're-applied' : 'enabled'} on ${name}`,
          description: persistence ? `The BMC boots ${iso} first on ${persistence}.` : `The BMC boots ${iso} first.`,
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
      if (!closedWhileBusy.current) onClose()
    } catch (caught) {
      const message = caught instanceof Error ? caught.message : 'The BMC could not be reached.'
      if (closedWhileBusy.current) {
        showToast({ tone: 'error', title: `Boot Media was not applied on ${name}`, description: message })
        onChanged()
      } else {
        setError(message)
      }
    } finally {
      setBusy(false)
      if (!disabling) onApplySettled?.()
    }
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={closable}
      title={TITLES[mode]}
      description={
        disabling
          ? `Stop booting ${name} from its Boot ISO.`
          : `Make ${name}'s BMC boot an iPXE Boot ISO first, so it reaches the provisioner without the provisioner's DHCP.`
      }
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <HStack gap="2">
          <Button variant="ghost" onClick={close} disabled={!closable}>
            {busy ? 'Continue in background' : 'Cancel'}
          </Button>
          <Button
            type="submit"
            colorPalette={disabling ? 'red' : 'brand'}
            loading={busy}
            loadingText={disabling ? 'Disabling…' : 'Applying through the BMC…'}
            disabled={!canSubmit}
          >
            {SUBMIT_LABELS[mode]}
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
          <Alert status="error" title={disabling ? 'Boot Media was not disabled' : 'Boot Media was not applied'}>
            {error}
          </Alert>
        )}
        {choosing && !busy && (
          <BootISOChoice
            state={isos}
            value={selectedId}
            currentId={current?.id}
            disabled={busy || locked}
            bootISOsHref={scopedHref('/provisioning/boot-isos')}
            onChange={setChosenId}
          />
        )}
        {busy && !disabling ? (
          <Stack gap="2">
            {selected?.name && (
              <Text fontSize="sm">
                Boot ISO <strong>{selected.name}</strong>
              </Text>
            )}
            <BootMediaApplyProgress apply={bootMedia.apply} requestedAt={requestedAt} />
            <Text color="fg.muted" fontSize="xs">
              Closing this dialog does not stop it: the Boot media block keeps showing its progress, and the outcome arrives
              as a notification.
            </Text>
          </Stack>
        ) : disabling ? (
          <Text fontSize="sm">
            OS deployments stop applying Boot Media, and swallow asks the BMC to eject the ISO and stop booting it first. If the
            BMC does not answer, the setting is still disabled. The chosen Boot ISO is kept for the next time you enable it.
          </Text>
        ) : (
          <>
            <Text fontSize="sm">swallow checks this on the real BMC before saving, because support differs by hardware and firmware:</Text>
            <List.Root as="ol" fontSize="sm" ps="5" gap="1">
              <List.Item>Probe the BMC&apos;s Redfish service.</List.Item>
              {mode === 'change' && current && <List.Item>Eject the current Boot ISO.</List.Item>}
              <List.Item>
                Mount{' '}
                {selected ? (
                  <Text as="span" className="sw-mono" wordBreak="break-all">
                    {selected.url}
                  </Text>
                ) : (
                  'the chosen Boot ISO'
                )}{' '}
                as a virtual CD.
              </List.Item>
              <List.Item>Direct the next boots at that CD and read both back.</List.Item>
            </List.Root>
            <Text color="fg.muted" fontSize="sm">
              It can take a few minutes. The Server is not rebooted now. Because a BMC can lose the setting, every OS
              deployment of this Server applies it again before powering it on. Secure Boot must be off: iPXE is unsigned.
            </Text>
          </>
        )}
      </Stack>
    </Modal>
  )
}

interface BootISOChoiceProps {
  state: AsyncData<BootISO[]>
  value: string
  currentId?: string
  disabled: boolean
  bootISOsHref: string
  onChange: (id: string) => void
}

/**
 * The Boot ISO field: the Server provisioner's ISOs, or why there is nothing to choose. An ISO the
 * installation does not serve (no Boot Media base URL) is listed but not selectable, since a BMC
 * could not mount it.
 */
function BootISOChoice({ state, value, currentId, disabled, bootISOsHref, onChange }: BootISOChoiceProps) {
  if (state.status === 'loading') {
    return <Text color="fg.muted" fontSize="sm">Loading Boot ISOs…</Text>
  }
  if (state.status !== 'ready') {
    return (
      <Alert status="error" title="Boot ISOs could not be read">
        {state.message}
      </Alert>
    )
  }
  if (state.data.length === 0) {
    return (
      <Alert status="info" title="No Boot ISO for this Server's provisioner">
        Build one on the{' '}
        <Link asChild colorPalette="brand">
          <RouterLink to={bootISOsHref}>Boot ISOs tab</RouterLink>
        </Link>{' '}
        of Provisioning, then come back to choose it.
      </Alert>
    )
  }
  return (
    <Field.Root required>
      <Field.Label htmlFor="boot-media-iso">
        Boot ISO <Field.RequiredIndicator />
      </Field.Label>
      <Select
        id="boot-media-iso"
        aria-label="Boot ISO"
        placeholder="Select a Boot ISO"
        value={value}
        disabled={disabled}
        onChange={onChange}
        options={state.data.map((iso) => ({
          value: iso.id,
          label: `${iso.name} — chains to ${iso.chainUrl}${iso.id === currentId ? ' (current)' : ''}${iso.url ? '' : ' (not served)'}`,
          disabled: !iso.url,
        }))}
      />
      <Field.HelperText>
        Only ISOs built for this Server&apos;s provisioner are offered.{' '}
        <Link asChild colorPalette="brand">
          <RouterLink to={bootISOsHref}>Manage Boot ISOs</RouterLink>
        </Link>
      </Field.HelperText>
    </Field.Root>
  )
}
