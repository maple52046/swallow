import { useMemo, useState } from 'react'
import { Button, Field, Heading, Input, Stack } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { Integration, IntegrationKind, Site } from '@/domain/site/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { credentialGuidance } from './credentialGuidance'
import { CredentialGuidanceNote } from './CredentialGuidanceNote'

interface IntegrationDialogProps {
  integration?: Integration
  sites: Site[]
  defaultSiteId?: string
  onClose: () => void
  onSaved: (integration: Integration) => void
}

const PROVIDERS: Record<IntegrationKind, Array<{ value: string; label: string }>> = {
  provisioner: [{ value: 'maas', label: 'MAAS' }],
  metrics: [{ value: 'prometheus', label: 'Prometheus' }],
  platform: [
    { value: 'kubernetes', label: 'Kubernetes' },
    { value: 'slurm', label: 'Slurm' },
  ],
}

/** Every integration role, with labels, for display. */
const ROLE_OPTIONS: Array<{ value: IntegrationKind; label: string }> = [
  { value: 'provisioner', label: 'Provisioner' },
  { value: 'metrics', label: 'Metrics' },
  { value: 'platform', label: 'Platform' },
]

/**
 * Roles an operator may create. `platform` is intentionally excluded: a platform Integration is
 * created only by deploying a platform (decision 032), so it can be edited but never registered
 * here. Editing keeps the full list so an existing platform Integration still displays its role.
 */
const CREATABLE_ROLE_OPTIONS = ROLE_OPTIONS.filter((option) => option.value !== 'platform')

function replaceSetting(settings: Record<string, string>, key: string, value: string): Record<string, string> {
  const next = { ...settings }
  if (value) next[key] = value
  else delete next[key]
  return next
}

/**
 * Creates or edits one provider connection without allowing Site or provider ownership
 * to change after registration. Unknown adapter settings are preserved during edits, and
 * the write-only credential (create flow only) is cleared from state on success.
 */
export function IntegrationDialog({
  integration,
  sites: availableSites,
  defaultSiteId,
  onClose,
  onSaved,
}: IntegrationDialogProps) {
  const { sites } = useApp()
  const [siteId, setSiteId] = useState(integration?.siteId ?? defaultSiteId ?? '')
  const [kind, setKind] = useState<IntegrationKind>(integration?.kind ?? 'provisioner')
  const [providerKind, setProviderKind] = useState(integration?.providerKind ?? 'maas')
  const [name, setName] = useState(integration?.name ?? '')
  const [endpoint, setEndpoint] = useState(integration?.endpoint ?? '')
  const [credential, setCredential] = useState('')
  const [settings, setSettings] = useState<Record<string, string>>(integration?.settings ?? {})
  const [enabled, setEnabled] = useState(integration?.enabled ?? true)
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const editing = Boolean(integration)
  const providerOptions = PROVIDERS[kind]
  const credentialRequired = !editing && kind !== 'metrics'
  const valid = Boolean(siteId && name.trim() && endpoint.trim() && (!credentialRequired || credential))

  const providerLabel = useMemo(
    () => providerOptions.find((provider) => provider.value === providerKind)?.label ?? providerKind,
    [providerKind, providerOptions],
  )

  const close = () => {
    if (!submitting) onClose()
  }

  const changeKind = (nextKind: IntegrationKind) => {
    setKind(nextKind)
    setProviderKind(PROVIDERS[nextKind][0].value)
    setSettings({})
  }

  const setSetting = (key: string, value: string) => {
    setSettings((current) => replaceSetting(current, key, value))
  }

  const submit = async () => {
    if (!valid || submitting) return
    setSubmitting(true)
    setError('')
    try {
      const saved = integration
        ? await sites.updateIntegration(integration.id, { name: name.trim(), endpoint: endpoint.trim(), settings, enabled })
        : await sites.createIntegration({
            siteId,
            kind,
            providerKind,
            name: name.trim(),
            endpoint: endpoint.trim(),
            credential,
            settings,
            enabled,
          })
      setCredential('')
      onSaved(saved)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Integration could not be saved.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      size="xl"
      closeOnInteractOutside={!submitting}
      title={editing ? 'Edit integration' : 'Create integration'}
      description={
        editing
          ? `${providerLabel} remains attached to its original Site and provider role.`
          : 'An Integration connects exactly one Site to one external provider.'
      }
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={submitting} disabled={!valid || submitting}>
            {editing ? 'Save changes' : 'Create integration'}
          </Button>
        </>
      }
    >
      <Stack gap="4" className="sw-resource-form">
        {error && (
          <Alert status="error" title="Integration could not be saved">
            {error}
          </Alert>
        )}
        <div className="sw-resource-form-grid">
          <Field.Root required>
            <Field.Label>
              Site <Field.RequiredIndicator />
            </Field.Label>
            <Select
              id="integration-site"
              value={siteId}
              disabled={editing}
              aria-label="Site"
              placeholder="Select a Site"
              onChange={setSiteId}
              options={availableSites.map((site) => ({ value: site.id, label: site.name }))}
            />
          </Field.Root>
          <Field.Root required>
            <Field.Label>
              Role <Field.RequiredIndicator />
            </Field.Label>
            <Select
              id="integration-kind"
              value={kind}
              disabled={editing}
              aria-label="Role"
              onChange={(value) => changeKind(value as IntegrationKind)}
              options={editing ? ROLE_OPTIONS : CREATABLE_ROLE_OPTIONS}
            />
          </Field.Root>
          <Field.Root required>
            <Field.Label>
              Provider <Field.RequiredIndicator />
            </Field.Label>
            <Select
              id="integration-provider"
              value={providerKind}
              disabled={editing}
              aria-label="Provider"
              onChange={(value) => {
                setProviderKind(value)
                setSettings({})
              }}
              options={providerOptions.map((provider) => ({ value: provider.value, label: provider.label }))}
            />
          </Field.Root>
          <Field.Root required>
            <Field.Label>
              Name <Field.RequiredIndicator />
            </Field.Label>
            <Input value={name} onChange={(event) => setName(event.target.value)} autoFocus />
          </Field.Root>
        </div>
        <Field.Root required>
          <Field.Label>
            Endpoint <Field.RequiredIndicator />
          </Field.Label>
          <Input type="url" value={endpoint} onChange={(event) => setEndpoint(event.target.value)} />
        </Field.Root>
        {!editing && (
          <>
            <CredentialGuidanceNote providerKind={providerKind} />
            <Field.Root required={credentialRequired}>
              <Field.Label>
                {credentialGuidance(providerKind).term} {credentialRequired && <Field.RequiredIndicator />}
              </Field.Label>
              <Input
                type="password"
                value={credential}
                onChange={(event) => setCredential(event.target.value)}
                autoComplete="new-password"
              />
              <Field.HelperText>
                Write-only. Swallow stores it encrypted and never returns it.
                {credentialRequired ? '' : ' Optional for this provider — leave empty for anonymous access.'}
              </Field.HelperText>
            </Field.Root>
          </>
        )}
        <section className="sw-integration-settings" aria-labelledby="integration-settings-title">
          <Heading as="h3" size="sm" id="integration-settings-title">
            Connection settings
          </Heading>
          <div className="sw-resource-form-grid">
            <Field.Root>
              <Field.Label>Request timeout</Field.Label>
              <Input value={settings.timeout ?? ''} placeholder="30s" onChange={(event) => setSetting('timeout', event.target.value)} />
              <Field.HelperText>Go duration such as 30s or 2m.</Field.HelperText>
            </Field.Root>
            <Field.Root>
              <Field.Label>TLS verification</Field.Label>
              <Checkbox
                id="integration-insecure"
                checked={settings.insecureSkipVerify === 'true'}
                onCheckedChange={(checked) => setSetting('insecureSkipVerify', checked ? 'true' : '')}
              >
                Skip certificate verification
              </Checkbox>
            </Field.Root>
            {providerKind === 'prometheus' && (
              <>
                <Field.Root>
                  <Field.Label>Alertmanager URL</Field.Label>
                  <Input type="url" value={settings.alertmanagerUrl ?? ''} onChange={(event) => setSetting('alertmanagerUrl', event.target.value)} />
                </Field.Root>
                <Field.Root>
                  <Field.Label>Grafana URL</Field.Label>
                  <Input type="url" value={settings.grafanaUrl ?? ''} onChange={(event) => setSetting('grafanaUrl', event.target.value)} />
                </Field.Root>
              </>
            )}
            {providerKind === 'slurm' && (
              <Field.Root>
                <Field.Label>Slurm API version</Field.Label>
                <Input value={settings.slurmApiVersion ?? ''} placeholder="v0.0.40" onChange={(event) => setSetting('slurmApiVersion', event.target.value)} />
              </Field.Root>
            )}
            {providerKind === 'kubernetes' && (
              <Field.Root>
                <Field.Label>Controller discovery</Field.Label>
                <Checkbox
                  id="integration-controller-leases"
                  checked={settings.controllerLeaseDiscovery === 'true'}
                  onCheckedChange={(checked) => setSetting('controllerLeaseDiscovery', checked ? 'true' : '')}
                >
                  Discover dedicated k0s controllers from leases
                </Checkbox>
              </Field.Root>
            )}
          </div>
        </section>
        <Checkbox id="integration-enabled" checked={enabled} onCheckedChange={setEnabled}>
          Enabled
        </Checkbox>
      </Stack>
    </Modal>
  )
}
