import { useState } from 'react'
import { Button, Field, Input, List, Stack, Text } from '@chakra-ui/react'
import type { Platform, UninstallPlatformOptions } from '@/domain/platform/types'
import { emptyReleaseOptions, type ReleaseOptionsValue } from '@/presentation/components/releaseOptions'
import { ReleaseOptionsFields } from '@/presentation/components/ReleaseOptionsFields'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { usePlatformBulkActions, type PlatformBulkAction } from './usePlatformBulkActions'

interface PlatformBulkActionDialogProps {
  action: PlatformBulkAction
  /** Platforms the action will run against. */
  targets: Platform[]
  /** Selected platforms excluded from an uninstall (e.g. registered or already uninstalled). */
  skipped: Platform[]
  onClose: () => void
  /** Called after at least one target was accepted, so the page reloads and clears selection. */
  onDone: () => void
}

/**
 * Confirms and fans out uninstall or delete across the selected platforms.
 *
 * Bulk destructive actions cannot ask an operator to type every platform name, so the gate is
 * typing the action verb. Uninstall reuses the standalone mutually exclusive server-release
 * shortcut; delete only removes the Swallow records. The dialog stays open and shows an inline
 * error when every target failed, otherwise it hands control back to the page.
 */
export function PlatformBulkActionDialog({ action, targets, skipped, onClose, onDone }: PlatformBulkActionDialogProps) {
  const { run, running } = usePlatformBulkActions()
  const [confirmation, setConfirmation] = useState('')
  const [error, setError] = useState('')
  const [releaseServers, setReleaseServers] = useState(false)
  const [releaseOptions, setReleaseOptions] = useState<ReleaseOptionsValue>(emptyReleaseOptions)

  const isUninstall = action === 'uninstall'
  const verb = isUninstall ? 'uninstall' : 'delete'
  const count = targets.length

  const close = () => {
    if (!running) onClose()
  }

  const submit = async () => {
    if (confirmation !== verb || running || count === 0) return
    setError('')
    const options: UninstallPlatformOptions | undefined = isUninstall
      ? {
          // Gate every erase/unbind choice on releaseServers so an unchecked release can never
          // submit stray options; the erase modes only apply when erase itself is on.
          releaseServers,
          releaseOptions: {
            erase: releaseServers && releaseOptions.erase,
            secureErase: releaseServers && releaseOptions.erase && releaseOptions.secureErase,
            quickErase: releaseServers && releaseOptions.erase && releaseOptions.quickErase,
            unbindStaticIps: releaseServers && releaseOptions.unbindStaticIPs,
          },
        }
      : undefined

    const outcomes = await run(action, targets, options)
    if (outcomes.every((outcome) => !outcome.accepted)) {
      setError('Every selected platform failed. Review the error and retry.')
      return
    }
    onDone()
  }

  return (
    <Modal
      open
      onClose={close}
      size={isUninstall && releaseServers ? 'lg' : 'md'}
      closeOnInteractOutside={!running}
      title={isUninstall ? `Uninstall ${count} platform${count === 1 ? '' : 's'}` : `Delete ${count} platform${count === 1 ? '' : 's'}`}
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={running}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="red" loading={running} disabled={confirmation !== verb || running || count === 0}>
            {isUninstall
              ? releaseServers
                ? 'Release all servers'
                : `Uninstall ${count} platform${count === 1 ? '' : 's'}`
              : `Delete ${count} platform${count === 1 ? '' : 's'}`}
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="Action failed">
            {error}
          </Alert>
        )}
        {isUninstall && !releaseServers ? (
          <Text>
            Platform services, configuration, credentials, and managed state are removed from each platform&apos;s deployment targets. Operating systems,
            user data, and other installed packages remain. Hosts are not rebooted, and the Swallow platform records
            are retained.
          </Text>
        ) : !isUninstall ? (
          <Alert status="warning" title="Hosts will not be uninstalled">
            Only the Swallow records and owned projections are deleted. Any platforms still running on the hosts and
            any accepted Operations will continue.
          </Alert>
        ) : null}
        <Field.Root>
          <Field.Label>{`${count} platform${count === 1 ? '' : 's'} affected`}</Field.Label>
          <List.Root id="platform-bulk-targets" listStyle="none" ps="0">
            {targets.map((platform) => (
              <List.Item key={platform.id}>{platform.name}</List.Item>
            ))}
          </List.Root>
        </Field.Root>
        {skipped.length > 0 && (
          <Alert status="info" title={`${skipped.length} selected platform${skipped.length === 1 ? '' : 's'} skipped`}>
            {skipped.map((platform) => platform.name).join(', ')} cannot be uninstalled (externally registered, already uninstalled, or an operation is
            running) and are left unchanged.
          </Alert>
        )}
        {isUninstall && (
          <>
            <Checkbox
              id="bulk-uninstall-release-servers"
              checked={releaseServers}
              onCheckedChange={(checked) => {
                setReleaseServers(checked)
                if (!checked) setReleaseOptions(emptyReleaseOptions)
              }}
            >
              Release all servers instead of uninstalling platform software
            </Checkbox>
            {releaseServers && (
              <>
                <Alert status="error" title="Platform software uninstall is skipped">
                  Every original deployment target is released directly to its provider, which removes its deployed
                  operating system. The Swallow platform records are retained.
                </Alert>
                <ReleaseOptionsFields idPrefix="bulk-uninstall-release" value={releaseOptions} onChange={setReleaseOptions} />
              </>
            )}
          </>
        )}
        <Field.Root required>
          <Field.Label>
            Type "{verb}" to confirm <Field.RequiredIndicator />
          </Field.Label>
          <Input value={confirmation} onChange={(event) => setConfirmation(event.target.value)} autoFocus aria-label={`Type ${verb} to confirm`} />
        </Field.Root>
      </Stack>
    </Modal>
  )
}
