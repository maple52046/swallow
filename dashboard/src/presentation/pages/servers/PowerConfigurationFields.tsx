import { Field, Input, Stack, Text } from '@chakra-ui/react'
import { serverDisplayName, type Server } from '@/domain/server/types'
import { powerDriverLabel, powerFamilyLabel } from '@/presentation/components/serverSummary/powerConfigurationLabels'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { PasswordInput } from '@/presentation/components/ui/password-input'
import { Select } from '@/presentation/components/ui/select'
import type { PowerConfigurationForm } from './usePowerConfigurationForm'

interface PowerConfigurationFieldsProps {
  /** The Server being configured; its name is used in the read-only notice while it is locked. */
  server: Server
  /** The edit, from `usePowerConfigurationForm`; the host dialog submits it with `form.save()`. */
  form: PowerConfigurationForm
}

/**
 * The fields of a Power Configuration edit (decision 054), shared by the Server Summary's
 * `ServerPowerConfigurationDialog` and the Server list's `ServerPowerDialog`. The host owns the
 * modal, its submit, and its buttons.
 *
 * The chosen driver's family decides the fields: a BMC address and account, or a libvirt
 * hypervisor URI and domain. Field errors appear only after a save was attempted. A locked Server
 * explains why it is read-only, and the API's refusal of a save is shown inline. The password
 * field uses `autoComplete="new-password"` so a browser never offers the user's own swallow
 * password, can be revealed so the user can check what they typed (it is write-only and never
 * shown back once saved), and its helper says what an empty value does.
 */
export function PowerConfigurationFields({ server, form }: PowerConfigurationFieldsProps) {
  const { draft, errors, family, touched, busy } = form
  return (
    <Stack gap="4">
      {form.locked && (
        <Alert status="warning" title="Read-only while locked">
          {serverDisplayName(server)} is locked. Unlock it to change its power configuration.
        </Alert>
      )}
      {form.error && (
        <Alert status="error" title="The power configuration was not changed">
          <FailureMessage failure={form.error} />
        </Alert>
      )}
      <Field.Root required invalid={touched && Boolean(errors.driver)}>
        <Field.Label>
          Driver <Field.RequiredIndicator />
        </Field.Label>
        <Select
          id="power-configuration-driver"
          aria-label="Driver"
          placeholder={form.reading ? 'Reading the drivers…' : 'Select a driver'}
          value={draft.driver}
          disabled={busy || form.reading}
          onChange={(driver) => form.update({ driver, clearPassword: false })}
          options={form.options.map((option) => ({
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
              onChange={(event) => form.update({ address: event.target.value })}
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
                onChange={(event) => form.update({ powerId: event.target.value })}
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
                onChange={(event) => form.update({ username: event.target.value })}
                className="sw-mono"
                autoComplete="off"
                disabled={busy}
              />
            </Field.Root>
          )}
          <Field.Root>
            <Field.Label>Password</Field.Label>
            {/* new-password keeps browsers from offering the user's own swallow password here. */}
            <PasswordInput
              value={draft.password}
              className="sw-mono"
              onChange={(event) => form.update({ password: event.target.value, clearPassword: false })}
              autoComplete="new-password"
              disabled={busy || draft.clearPassword}
            />
            <Field.HelperText>{form.passwordHelper}</Field.HelperText>
          </Field.Root>
          {form.canClearPassword && (
            <Checkbox
              checked={draft.clearPassword}
              disabled={busy}
              onCheckedChange={(clearPassword) => form.update({ clearPassword, password: '' })}
            >
              Remove the stored password
            </Checkbox>
          )}
          <Text color="fg.muted" fontSize="sm">
            Saving first checks that the provisioner can read the power with these settings, which can take up to 20
            seconds; nothing is saved if it cannot. Saving never switches power.
          </Text>
        </>
      )}
    </Stack>
  )
}
