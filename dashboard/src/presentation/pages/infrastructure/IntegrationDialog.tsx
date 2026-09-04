import { useMemo, useState } from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  Checkbox,
  Form,
  FormGroup,
  FormHelperText,
  FormSelect,
  FormSelectOption,
  HelperText,
  HelperTextItem,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  TextInput,
} from '@patternfly/react-core'
import { useApp } from '@/di/AppProvider'
import type { Integration, IntegrationKind, Site } from '@/domain/site/types'

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

function replaceSetting(settings: Record<string, string>, key: string, value: string): Record<string, string> {
  const next = { ...settings }
  if (value) next[key] = value
  else delete next[key]
  return next
}

/**
 * Creates or edits one provider connection without allowing Site or provider ownership
 * to change after registration. Unknown adapter settings are preserved during edits.
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
        ? await sites.updateIntegration(integration.id, {
          name: name.trim(),
          endpoint: endpoint.trim(),
          settings,
          enabled,
        })
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
    <Modal isOpen onClose={close} variant="medium" aria-labelledby="integration-editor-title">
      <ModalHeader
        title={editing ? 'Edit integration' : 'Create integration'}
        labelId="integration-editor-title"
        description={editing
          ? `${providerLabel} remains attached to its original Site and provider role.`
          : 'An Integration connects exactly one Site to one external provider.'}
      />
      <ModalBody>
        <Form className="sw-resource-form">
          {error && <Alert variant={AlertVariant.danger} title="Integration could not be saved" isInline>{error}</Alert>}
          <div className="sw-resource-form-grid">
            <FormGroup label="Site" isRequired fieldId="integration-site">
              <FormSelect
                id="integration-site"
                value={siteId}
                isDisabled={editing}
                onChange={(_event, value) => setSiteId(value)}
              >
                <FormSelectOption value="" label="Select a Site" isDisabled isPlaceholder />
                {availableSites.map((site) => <FormSelectOption key={site.id} value={site.id} label={site.name} />)}
              </FormSelect>
            </FormGroup>
            <FormGroup label="Role" isRequired fieldId="integration-kind">
              <FormSelect
                id="integration-kind"
                value={kind}
                isDisabled={editing}
                onChange={(_event, value) => changeKind(value as IntegrationKind)}
              >
                <FormSelectOption value="provisioner" label="Provisioner" />
                <FormSelectOption value="metrics" label="Metrics" />
                <FormSelectOption value="platform" label="Platform" />
              </FormSelect>
            </FormGroup>
            <FormGroup label="Provider" isRequired fieldId="integration-provider">
              <FormSelect
                id="integration-provider"
                value={providerKind}
                isDisabled={editing}
                onChange={(_event, value) => {
                  setProviderKind(value)
                  setSettings({})
                }}
              >
                {providerOptions.map((provider) => (
                  <FormSelectOption key={provider.value} value={provider.value} label={provider.label} />
                ))}
              </FormSelect>
            </FormGroup>
            <FormGroup label="Name" isRequired fieldId="integration-name">
              <TextInput id="integration-name" value={name} onChange={(_event, value) => setName(value)} autoFocus />
            </FormGroup>
          </div>
          <FormGroup label="Endpoint" isRequired fieldId="integration-endpoint">
            <TextInput id="integration-endpoint" type="url" value={endpoint} onChange={(_event, value) => setEndpoint(value)} />
          </FormGroup>
          {!editing && (
            <FormGroup label="Credential" isRequired={credentialRequired} fieldId="integration-credential">
              <TextInput
                id="integration-credential"
                type="password"
                value={credential}
                onChange={(_event, value) => setCredential(value)}
                autoComplete="new-password"
              />
              <FormHelperText>
                <HelperText><HelperTextItem>Write-only. Swallow will never return this value.</HelperTextItem></HelperText>
              </FormHelperText>
            </FormGroup>
          )}
          <section className="sw-integration-settings" aria-labelledby="integration-settings-title">
            <h3 id="integration-settings-title">Connection settings</h3>
            <div className="sw-resource-form-grid">
              <FormGroup label="Request timeout" fieldId="integration-timeout">
                <TextInput
                  id="integration-timeout"
                  value={settings.timeout ?? ''}
                  placeholder="30s"
                  onChange={(_event, value) => setSetting('timeout', value)}
                />
                <FormHelperText>
                  <HelperText><HelperTextItem>Go duration such as 30s or 2m.</HelperTextItem></HelperText>
                </FormHelperText>
              </FormGroup>
              <FormGroup fieldId="integration-insecure" label="TLS verification">
                <Checkbox
                  id="integration-insecure"
                  label="Skip certificate verification"
                  isChecked={settings.insecureSkipVerify === 'true'}
                  onChange={(_event, checked) => setSetting('insecureSkipVerify', checked ? 'true' : '')}
                />
              </FormGroup>
              {providerKind === 'prometheus' && (
                <>
                  <FormGroup label="Alertmanager URL" fieldId="integration-alertmanager-url">
                    <TextInput id="integration-alertmanager-url" type="url" value={settings.alertmanagerUrl ?? ''} onChange={(_event, value) => setSetting('alertmanagerUrl', value)} />
                  </FormGroup>
                  <FormGroup label="Grafana URL" fieldId="integration-grafana-url">
                    <TextInput id="integration-grafana-url" type="url" value={settings.grafanaUrl ?? ''} onChange={(_event, value) => setSetting('grafanaUrl', value)} />
                  </FormGroup>
                </>
              )}
              {providerKind === 'slurm' && (
                <FormGroup label="Slurm API version" fieldId="integration-slurm-api-version">
                  <TextInput id="integration-slurm-api-version" value={settings.slurmApiVersion ?? ''} placeholder="v0.0.40" onChange={(_event, value) => setSetting('slurmApiVersion', value)} />
                </FormGroup>
              )}
              {providerKind === 'kubernetes' && (
                <FormGroup fieldId="integration-controller-leases" label="Controller discovery">
                  <Checkbox
                    id="integration-controller-leases"
                    label="Discover dedicated k0s controllers from leases"
                    isChecked={settings.controllerLeaseDiscovery === 'true'}
                    onChange={(_event, checked) => setSetting('controllerLeaseDiscovery', checked ? 'true' : '')}
                  />
                </FormGroup>
              )}
            </div>
          </section>
          <Checkbox id="integration-enabled" label="Enabled" isChecked={enabled} onChange={(_event, checked) => setEnabled(checked)} />
        </Form>
      </ModalBody>
      <ModalFooter>
        <Button onClick={() => void submit()} isLoading={submitting} isDisabled={!valid || submitting}>
          {editing ? 'Save changes' : 'Create integration'}
        </Button>
        <Button variant="link" onClick={close} isDisabled={submitting}>Cancel</Button>
      </ModalFooter>
    </Modal>
  )
}
