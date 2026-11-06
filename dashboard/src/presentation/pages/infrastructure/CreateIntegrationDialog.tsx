import { useState } from 'react'
import { Button, Field, Heading, Input, Stack } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { Integration, IntegrationKind, Site } from '@/domain/site/types'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { failureOf, type DisplayedFailure } from '@/presentation/components/failure'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { credentialGuidance } from './credentialGuidance'
import { CredentialGuidanceNote } from './CredentialGuidanceNote'
import { IntegrationConnectionFields } from './IntegrationConnectionFields'
import { integrationSettingsProblem, withIntegrationSetting } from './integrationSettings'

/**
 * Roles a user may create. `platform` is intentionally absent: a platform Integration is
 * created only by deploying a platform (decision 032), and is edited on its settings page.
 */
export type CreatableIntegrationRole = Exclude<IntegrationKind, 'platform'>

interface CreateIntegrationDialogProps {
  sites: Site[]
  defaultSiteId?: string
  /**
   * The role preselected when the dialog opens, for example `repository` when the Local repository
   * page sends a Site without one here; Provisioner when omitted.
   */
  defaultKind?: CreatableIntegrationRole
  onClose: () => void
  /** Called with the Integration the API created; the caller decides where to go next. */
  onCreated: (integration: Integration) => void
}

/** Providers a user can register per role; the API accepts only these pairs (sites-integrations.md). */
const PROVIDERS: Record<CreatableIntegrationRole, Array<{ value: string; label: string }>> = {
  provisioner: [{ value: 'maas', label: 'MAAS' }],
  metrics: [{ value: 'prometheus', label: 'Prometheus' }],
  repository: [{ value: 'pulp', label: 'Pulp' }],
}

const CREATABLE_ROLE_OPTIONS: Array<{ value: CreatableIntegrationRole; label: string }> = [
  { value: 'provisioner', label: 'Provisioner' },
  { value: 'metrics', label: 'Metrics' },
  // A Site's Local Repository; the API refuses a second one in the same Site with a conflict.
  { value: 'repository', label: 'Repository' },
]

/**
 * Registers one provider connection under a Site: identity (Site, role, provider, name), endpoint,
 * the write-only credential, and its connection settings. Everything an existing Integration can
 * change — and the provider-owned settings MAAS holds, such as its HTTP proxy — lives on the
 * Integration settings page instead, so this dialog stays short.
 *
 * The credential lives only in transient state and is cleared on success. Changing the role or
 * provider resets the settings, because their keys are provider-specific.
 */
export function CreateIntegrationDialog({ sites: availableSites, defaultSiteId, defaultKind, onClose, onCreated }: CreateIntegrationDialogProps) {
  const { sites } = useApp()
  const [siteId, setSiteId] = useState(defaultSiteId ?? '')
  const [kind, setKind] = useState<CreatableIntegrationRole>(defaultKind ?? 'provisioner')
  const [providerKind, setProviderKind] = useState(PROVIDERS[defaultKind ?? 'provisioner'][0].value)
  const [name, setName] = useState('')
  const [endpoint, setEndpoint] = useState('')
  const [credential, setCredential] = useState('')
  const [settings, setSettings] = useState<Record<string, string>>({})
  const [enabled, setEnabled] = useState(true)
  const [error, setError] = useState<DisplayedFailure | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const providerOptions = PROVIDERS[kind]
  // Metrics backends may allow anonymous reads; every other role needs a credential to work (a
  // repository's is the provider's admin password).
  const credentialRequired = kind !== 'metrics'
  const valid = Boolean(siteId && name.trim() && endpoint.trim() && (!credentialRequired || credential))
    && integrationSettingsProblem(kind, settings) === null

  const close = () => {
    if (!submitting) onClose()
  }

  const changeKind = (nextKind: CreatableIntegrationRole) => {
    setKind(nextKind)
    setProviderKind(PROVIDERS[nextKind][0].value)
    setSettings({})
  }

  const submit = async () => {
    if (!valid || submitting) return
    setSubmitting(true)
    setError(null)
    try {
      const created = await sites.createIntegration({
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
      onCreated(created)
    } catch (caught) {
      setError(failureOf(caught, 'Integration could not be created.'))
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
      title="Create integration"
      description="An Integration connects exactly one Site to one external provider."
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
            Create integration
          </Button>
        </>
      }
    >
      <Stack gap="4" className="sw-resource-form">
        {error && (
          <Alert status="error" title="Integration could not be created">
            <FailureMessage failure={error} />
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
              aria-label="Role"
              // Every option is a creatable role, so the value the select emits is one of them.
              onChange={(value) => changeKind(value as CreatableIntegrationRole)}
              options={CREATABLE_ROLE_OPTIONS}
            />
          </Field.Root>
          <Field.Root required>
            <Field.Label>
              Provider <Field.RequiredIndicator />
            </Field.Label>
            <Select
              id="integration-provider"
              value={providerKind}
              aria-label="Provider"
              onChange={(value) => {
                setProviderKind(value)
                setSettings({})
              }}
              options={providerOptions}
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
        <CredentialGuidanceNote providerKind={providerKind} />
        <Field.Root required={credentialRequired}>
          <Field.Label>
            {credentialGuidance(providerKind).term} {credentialRequired && <Field.RequiredIndicator />}
          </Field.Label>
          <Input type="password" value={credential} onChange={(event) => setCredential(event.target.value)} autoComplete="new-password" />
          <Field.HelperText>
            Write-only. Swallow stores it encrypted and never returns it.
            {credentialRequired ? '' : ' Optional for this provider — leave empty for anonymous access.'}
          </Field.HelperText>
        </Field.Root>
        <section className="sw-integration-settings" aria-labelledby="integration-settings-title">
          <Heading as="h3" size="sm" id="integration-settings-title">
            Connection settings
          </Heading>
          <IntegrationConnectionFields
            kind={kind}
            providerKind={providerKind}
            settings={settings}
            onSettingChange={(key, value) => setSettings((current) => withIntegrationSetting(current, key, value))}
          />
        </section>
        <Checkbox id="integration-enabled" checked={enabled} onCheckedChange={setEnabled}>
          Enabled
        </Checkbox>
      </Stack>
    </Modal>
  )
}
