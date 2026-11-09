import { Button } from '@chakra-ui/react'
import { serverDisplayName, type Server, type ServerPowerConfiguration } from '@/domain/server/types'
import { Modal } from '@/presentation/components/ui/modal'
import { PowerConfigurationFields } from './PowerConfigurationFields'
import { usePowerConfigurationForm } from './usePowerConfigurationForm'

interface ServerPowerConfigurationDialogProps {
  server: Server
  /** The configuration read when the dialog opened; its driver and parameters pre-fill the form. */
  configuration: ServerPowerConfiguration
  onClose: () => void
  /** Called with the configuration read back after a save, so the caller can show it at once. */
  onChanged: (configuration: ServerPowerConfiguration) => void
}

/**
 * Replaces one Server's Power Configuration (decision 054) from the Server Summary.
 *
 * The edit itself (draft, validation, the write-only password, the save and its toast) is the
 * shared `usePowerConfigurationForm` with `PowerConfigurationFields`, which the Server list's power
 * dialog uses too. This dialog adds the modal: saving writes to the provisioner, so dismissal is
 * blocked while it runs, and a successful save closes it after `onChanged`.
 */
export function ServerPowerConfigurationDialog({ server, configuration, onClose, onChanged }: ServerPowerConfigurationDialogProps) {
  const form = usePowerConfigurationForm(server, configuration, (saved) => {
    onChanged(saved)
    onClose()
  })

  return (
    <Modal
      open
      onClose={() => !form.busy && onClose()}
      closeOnInteractOutside={!form.busy}
      title="Power configuration"
      description={`How the provisioner switches and reads the power of ${serverDisplayName(server)}. Saved to the provisioner.`}
      onSubmit={(event) => {
        event.preventDefault()
        void form.save()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={form.busy}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={form.busy} loadingText="Checking…" disabled={!form.canSubmit}>
            Save
          </Button>
        </>
      }
    >
      <PowerConfigurationFields server={server} form={form} />
    </Modal>
  )
}
