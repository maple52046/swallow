import { Field, Input } from '@chakra-ui/react'
import { AUTO_INSPECT_SETTING, autoInspectEnabled, type IntegrationKind } from '@/domain/site/types'
import { Checkbox } from '@/presentation/components/ui/checkbox'

interface IntegrationConnectionFieldsProps {
  kind: IntegrationKind
  providerKind: string
  /** The Integration's `settings` string map as currently edited; unknown keys pass through. */
  settings: Record<string, string>
  /** Sets one key; an empty value removes it, which the API reads as the default. */
  onSettingChange: (key: string, value: string) => void
  disabled?: boolean
}

/**
 * The Integration `settings` an operator edits (contract sites-integrations.md): how swallow
 * reaches the provider (timeout, TLS) and swallow's own policy for it (automatic inspection), plus
 * the provider-specific keys. Shared by the Create integration dialog and the Connection tab of the
 * Integration settings page, so both offer the same keys with the same defaults.
 *
 * Every value is a string; a cleared field removes its key rather than storing "" so the backend
 * applies its default. The fields render into the caller's two-column form grid.
 */
export function IntegrationConnectionFields({ kind, providerKind, settings, onSettingChange, disabled }: IntegrationConnectionFieldsProps) {
  return (
    <div className="sw-resource-form-grid">
      <Field.Root disabled={disabled}>
        <Field.Label>Request timeout</Field.Label>
        <Input value={settings.timeout ?? ''} placeholder="30s" onChange={(event) => onSettingChange('timeout', event.target.value)} />
        <Field.HelperText>Go duration such as 30s or 2m.</Field.HelperText>
      </Field.Root>
      <Field.Root disabled={disabled}>
        <Field.Label>TLS verification</Field.Label>
        <Checkbox
          id="integration-insecure"
          checked={settings.insecureSkipVerify === 'true'}
          disabled={disabled}
          onCheckedChange={(checked) => onSettingChange('insecureSkipVerify', checked ? 'true' : '')}
        >
          Skip certificate verification
        </Checkbox>
      </Field.Root>
      {kind === 'provisioner' && (
        <Field.Root disabled={disabled}>
          <Field.Label>Hardware inspection</Field.Label>
          {/* Stored only as "false" when off; absent means on (contract sites-integrations.md). */}
          <Checkbox
            id="integration-auto-inspect"
            checked={autoInspectEnabled({ settings })}
            disabled={disabled}
            onCheckedChange={(checked) => onSettingChange(AUTO_INSPECT_SETTING, checked ? '' : 'false')}
          >
            Inspect newly enrolled Servers automatically
          </Checkbox>
          <Field.HelperText>Swallow waits until enrollment ends, then inspects the hardware so the Server becomes Ready.</Field.HelperText>
        </Field.Root>
      )}
      {providerKind === 'prometheus' && (
        <>
          <Field.Root disabled={disabled}>
            <Field.Label>Alertmanager URL</Field.Label>
            <Input type="url" value={settings.alertmanagerUrl ?? ''} onChange={(event) => onSettingChange('alertmanagerUrl', event.target.value)} />
          </Field.Root>
          <Field.Root disabled={disabled}>
            <Field.Label>Grafana URL</Field.Label>
            <Input type="url" value={settings.grafanaUrl ?? ''} onChange={(event) => onSettingChange('grafanaUrl', event.target.value)} />
          </Field.Root>
        </>
      )}
      {providerKind === 'slurm' && (
        <Field.Root disabled={disabled}>
          <Field.Label>Slurm API version</Field.Label>
          <Input value={settings.slurmApiVersion ?? ''} placeholder="v0.0.40" onChange={(event) => onSettingChange('slurmApiVersion', event.target.value)} />
        </Field.Root>
      )}
      {providerKind === 'kubernetes' && (
        <Field.Root disabled={disabled}>
          <Field.Label>Controller discovery</Field.Label>
          <Checkbox
            id="integration-controller-leases"
            checked={settings.controllerLeaseDiscovery === 'true'}
            disabled={disabled}
            onCheckedChange={(checked) => onSettingChange('controllerLeaseDiscovery', checked ? 'true' : '')}
          >
            Discover dedicated k0s controllers from leases
          </Checkbox>
        </Field.Root>
      )}
    </div>
  )
}