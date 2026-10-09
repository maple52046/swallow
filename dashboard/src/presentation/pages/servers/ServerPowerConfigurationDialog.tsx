import { useState } from 'react'
import { Button, Field, Input, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import { serverDisplayName, type Server, type ServerPowerConfiguration } from '@/domain/server/types'
import { powerDriverLabel, powerFamilyLabel } from '@/presentation/components/serverSummary/powerConfigurationLabels'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import {
  draftFamily,
  powerConfigurationDraftErrors,
  powerConfigurationInput,
  type PowerConfigurationDraft,
} from './serverPowerConfigurationPresentation'

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
 * The form starts from the provisioner's current driver and parameters. Without a driver, nothing is
 * pre-selected: the operator must choose, because the dashboard cannot tell a BMC from a virtual
 * machine. The chosen driver's family decides the fields (a BMC address and account, or a libvirt
 * hypervisor URI and domain). The password is write-only: it lives only in this dialog's state, is
 * never shown back, and is dropped when the dialog unmounts; leaving it empty keeps the stored one for
 * an unchanged driver, which the helper text explains. Saving writes to the provisioner, so dismissal
 * is blocked while it runs and the API's refusal (its own or the provisioner's) is shown inline. A
 * locked Server explains why and disables saving. Saving never switches power or resumes an
 * inspection; the success toast says what to do next.
 */
export function ServerPowerConfigurationDialog({ server, configuration, onClose, onChanged }: ServerPowerConfigurationDialogProps) {
  const { servers } = useApp()
  const { showToast } = useToast()
  const options = configuration.drivers
  const [draft, setDraft] = useState<PowerConfigurationDraft>(() => ({
    driver: options.some((option) => option.driver === configuration.driver) ? configuration.driver : '',
    address: configuration.address,
    powerId: configuration.powerId,
    username: configuration.username,
    password: '',
    clearPassword: false,
  }))
  const [touched, setTouched] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const name = serverDisplayName(server)
  const locked = server.provisioning?.locked ?? false
  const family = draftFamily(draft, options)
  const errors = powerConfigurationDraftErrors(draft, options)
  const valid = Object.keys(errors).length === 0
  const sameDriver = draft.driver === configuration.driver
  const canClearPassword = sameDriver && configuration.passwordSet

  const update = (patch: Partial<PowerConfigurationDraft>) => setDraft((current) => ({ ...current, ...patch }))

  const save = async () => {
    setTouched(true)
    if (!valid || busy || locked) return
    setBusy(true)
    setError('')
    try {
      const saved = await servers.setPowerConfiguration(server.id, powerConfigurationInput(draft, options))
      showToast({
        tone: 'success',
        title: 'Power configuration saved',
        description: `The provisioner now powers ${name} through ${powerDriverLabel(saved.driver)}. Check its power state; retry its hardware inspection if it waits for attention.`,
      })
      onChanged(saved)
      onClose()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The power configuration could not be saved.')
    } finally {
      setBusy(false)
    }
  }

  const passwordHelper = canClearPassword
    ? 'Leave empty to keep the stored password. swallow sends it to the provisioner and does not store it.'
    : configuration.passwordSet
      ? 'Changing the driver removes the stored password unless you enter one. swallow does not store it.'
      : 'Optional. swallow sends it to the provisioner and does not store it.'

  return (
    <Modal
      open
      onClose={() => !busy && onClose()}
      closeOnInteractOutside={!busy}
      title="Power configuration"
      description={`How the provisioner switches and reads the power of ${name}. Saved to the provisioner.`}
      onSubmit={(event) => {
        event.preventDefault()
        void save()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={busy} loadingText="Saving…" disabled={busy || locked || (touched && !valid)}>
            Save
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {locked && (
          <Alert status="warning" title="Read-only while locked">
            {name} is locked. Unlock it to change its power configuration.
          </Alert>
        )}
        {error && (
          <Alert status="error" title="The power configuration was not changed">
            {error}
          </Alert>
        )}
        <Field.Root required invalid={touched && Boolean(errors.driver)}>
          <Field.Label>
            Driver <Field.RequiredIndicator />
          </Field.Label>
          <Select
            id="power-configuration-driver"
            aria-label="Driver"
            placeholder="Select a driver"
            value={draft.driver}
            disabled={busy}
            onChange={(driver) => update({ driver, clearPassword: false })}
            options={options.map((option) => ({
              value: option.driver,
              label: `${powerDriverLabel(option.driver)} — ${powerFamilyLabel(option.family)}`,
            }))}
          />
          <Field.ErrorText>{errors.driver}</Field.ErrorText>
        </Field.Root>
        {family && (
          <>
            <Field.Root required invalid={touched && Boolean(errors.address)}>
              <Field.Label>
                {family === 'virsh' ? 'Hypervisor URI' : 'BMC address'} <Field.RequiredIndicator />
              </Field.Label>
              <Input
                value={draft.address}
                onChange={(event) => update({ address: event.target.value })}
                className="sw-mono"
                autoComplete="off"
                placeholder={family === 'virsh' ? 'qemu+ssh://maas@hypervisor/system' : '10.0.0.5'}
                disabled={busy}
              />
              <Field.HelperText>
                {family === 'virsh'
                  ? "The provisioner's rack controller connects here over SSH; put SSH options in its SSH configuration, not in the URI."
                  : 'The host, IP, or URL of the BMC.'}
              </Field.HelperText>
              <Field.ErrorText>{errors.address}</Field.ErrorText>
            </Field.Root>
            {family === 'virsh' ? (
              <Field.Root required invalid={touched && Boolean(errors.powerId)}>
                <Field.Label>
                  Domain <Field.RequiredIndicator />
                </Field.Label>
                <Input
                  value={draft.powerId}
                  onChange={(event) => update({ powerId: event.target.value })}
                  className="sw-mono"
                  autoComplete="off"
                  disabled={busy}
                />
                <Field.HelperText>The libvirt domain name or UUID on that hypervisor (virsh list --all).</Field.HelperText>
                <Field.ErrorText>{errors.powerId}</Field.ErrorText>
              </Field.Root>
            ) : (
              <Field.Root>
                <Field.Label>Account</Field.Label>
                <Input
                  value={draft.username}
                  onChange={(event) => update({ username: event.target.value })}
                  className="sw-mono"
                  autoComplete="off"
                  disabled={busy}
                />
              </Field.Root>
            )}
            <Field.Root>
              <Field.Label>Password</Field.Label>
              {/* new-password keeps browsers from offering the operator's own swallow password here. */}
              <Input
                type="password"
                value={draft.password}
                onChange={(event) => update({ password: event.target.value, clearPassword: false })}
                autoComplete="new-password"
                disabled={busy || draft.clearPassword}
              />
              <Field.HelperText>{passwordHelper}</Field.HelperText>
            </Field.Root>
            {canClearPassword && (
              <Checkbox
                checked={draft.clearPassword}
                disabled={busy}
                onCheckedChange={(clearPassword) => update({ clearPassword, password: '' })}
              >
                Remove the stored password
              </Checkbox>
            )}
            <Text color="fg.muted" fontSize="sm">
              Saving does not switch power. If hardware inspection waits for attention, check the power state, then retry it.
            </Text>
          </>
        )}
      </Stack>
    </Modal>
  )
}
