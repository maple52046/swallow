import { useState } from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  Checkbox,
  FormGroup,
  List,
  ListItem,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  TextInput,
} from '@patternfly/react-core'
import type { Platform, UninstallPlatformOptions } from '@/domain/platform/types'
import {
  emptyReleaseOptions,
  type ReleaseOptionsValue,
} from '@/presentation/components/releaseOptions'
import { ReleaseOptionsFields } from '@/presentation/components/ReleaseOptionsFields'
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
 * typing the action verb. Uninstall reuses the standalone release-servers option; delete only
 * removes the Swallow records. The dialog stays open and shows an inline error when every
 * target failed, otherwise it hands control back to the page.
 */
export function PlatformBulkActionDialog({
  action,
  targets,
  skipped,
  onClose,
  onDone,
}: PlatformBulkActionDialogProps) {
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
      isOpen
      onClose={close}
      variant={isUninstall && releaseServers ? 'medium' : 'small'}
      aria-labelledby="platform-bulk-action-title"
    >
      <ModalHeader
        title={isUninstall ? `Uninstall ${count} platform${count === 1 ? '' : 's'}` : `Delete ${count} platform${count === 1 ? '' : 's'}`}
        labelId="platform-bulk-action-title"
        description={isUninstall
          ? 'Each platform runs its own uninstall Operation; the deployment targets are cleaned.'
          : 'This removes only the Swallow records and owned projections. Hosts are not changed.'}
      />
      <ModalBody>
        {error && (
          <Alert variant={AlertVariant.danger} title="Action failed" isInline>{error}</Alert>
        )}
        {isUninstall ? (
          <p>
            Platform software (Slurm or k0s), configuration, keys, and state are removed from each
            platform&apos;s deployment targets. The operating system stays installed unless you also
            release the servers. Hosts are not rebooted.
          </p>
        ) : (
          <Alert variant={AlertVariant.warning} title="Hosts will not be uninstalled" isInline>
            Any platform still running on the hosts continues to run, and accepted Operations
            continue after these records are deleted.
          </Alert>
        )}
        <FormGroup label={`${count} platform${count === 1 ? '' : 's'} affected`} fieldId="platform-bulk-targets">
          <List isPlain id="platform-bulk-targets">
            {targets.map((platform) => (
              <ListItem key={platform.id}>{platform.name}</ListItem>
            ))}
          </List>
        </FormGroup>
        {skipped.length > 0 && (
          <Alert variant={AlertVariant.info} title={`${skipped.length} selected platform${skipped.length === 1 ? '' : 's'} skipped`} isInline>
            {skipped.map((platform) => platform.name).join(', ')} cannot be uninstalled (externally
            registered, already uninstalled, or an operation is running) and are left unchanged.
          </Alert>
        )}
        {isUninstall && (
          <>
            <Checkbox
              id="bulk-uninstall-release-servers"
              label="Also release servers back to the provider"
              isChecked={releaseServers}
              onChange={(_event, checked) => {
                setReleaseServers(checked)
                if (!checked) setReleaseOptions(emptyReleaseOptions)
              }}
            />
            {releaseServers && (
              <>
                <Alert
                  variant={AlertVariant.danger}
                  title="Servers will be wiped and returned to the provider"
                  isInline
                >
                  After the platform software is removed, every target server of these platforms is
                  released to the provider in the same operation. This removes its deployed operating
                  system; the servers leave their platform and return to the available pool.
                </Alert>
                <ReleaseOptionsFields
                  idPrefix="bulk-uninstall-release"
                  value={releaseOptions}
                  onChange={setReleaseOptions}
                />
              </>
            )}
          </>
        )}
        <FormGroup label={`Type "${verb}" to confirm`} isRequired fieldId="platform-bulk-confirmation">
          <TextInput
            id="platform-bulk-confirmation"
            value={confirmation}
            onChange={(_event, value) => setConfirmation(value)}
            autoFocus
            aria-label={`Type ${verb} to confirm`}
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
          isLoading={running}
          isDisabled={confirmation !== verb || running || count === 0}
        >
          {isUninstall ? `Uninstall ${count} platform${count === 1 ? '' : 's'}` : `Delete ${count} platform${count === 1 ? '' : 's'}`}
        </Button>
        <Button variant="link" onClick={close} isDisabled={running}>Cancel</Button>
      </ModalFooter>
    </Modal>
  )
}
