import { useState } from 'react'
import { Button, Field, HStack, Input, Stack, Text } from '@chakra-ui/react'
import { Navigate, useLocation } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import { credentialGuidance } from './credentialGuidance'
import { CredentialGuidanceNote } from './CredentialGuidanceNote'
import { IntegrationConnectionFields } from './IntegrationConnectionFields'
import { IntegrationSettingsPanel } from './IntegrationSettingsPanel'
import { isMaasProvisioner, providerLabel, roleLabel, withIntegrationSetting } from './integrationSettings'
import { ProvisionerHttpProxySection } from './ProvisionerHttpProxySection'
import { ProvisionerSubnetDnsSection } from './ProvisionerSubnetDnsSection'
import { useIntegrationSettingsContext } from './useIntegrationSettings'


/**
 * The Save row shared by the General and Connection tabs: a status that says in words whether the
 * tab holds unsaved changes (they survive switching tabs, not leaving the page), and the button
 * that submits the tab's form.
 */
function SaveRow({ dirty, saving, canSave }: { dirty: boolean; saving: boolean; canSave: boolean }) {
  return (
    <HStack justify="space-between" gap="3" wrap="wrap">
      <Text role="status" fontSize="sm" color={dirty ? 'fg' : 'fg.muted'}>
        {dirty ? 'Unsaved changes. They are kept while you switch tabs, not when you leave this page.' : 'All changes saved.'}
      </Text>
      <Button type="submit" size="sm" colorPalette="brand" loading={saving} disabled={!canSave}>
        Save changes
      </Button>
    </HStack>
  )
}

/**
 * General tab: the Integration's identity and switch. Site, role, and provider are fixed at
 * registration and shown read-only; name, endpoint, and Enabled are edited and saved together. The
 * write sends the saved connection settings unchanged, so it never carries the Connection tab's
 * unsaved draft.
 */
export function IntegrationGeneralTab() {
  const { integration, siteName, generalDraft, setGeneralDraft, onSaved } = useIntegrationSettingsContext()
  const { sites } = useApp()
  const { showToast } = useToast()
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const draft = generalDraft ?? { name: integration.name, endpoint: integration.endpoint, enabled: integration.enabled }
  const dirty = draft.name !== integration.name || draft.endpoint !== integration.endpoint || draft.enabled !== integration.enabled
  const valid = Boolean(draft.name.trim() && draft.endpoint.trim())
  const update = (patch: Partial<typeof draft>) => setGeneralDraft({ ...draft, ...patch })

  const save = async () => {
    if (!dirty || !valid || saving) return
    setSaving(true)
    setError('')
    try {
      const saved = await sites.updateIntegration(integration.id, {
        name: draft.name.trim(),
        endpoint: draft.endpoint.trim(),
        enabled: draft.enabled,
        settings: integration.settings,
      })
      setGeneralDraft(null)
      onSaved(saved)
      showToast({ tone: 'success', title: 'Integration updated' })
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The Integration could not be saved.')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault()
        void save()
      }}
    >
      <IntegrationSettingsPanel
        id="integration-general"
        title="General"
        description="How this Integration is named and reached. Site, role, and provider are fixed when it is created."
      >
        {error && (
          <Alert status="error" title="Integration could not be saved">
            {error}
          </Alert>
        )}
        <DescriptionList
          items={[
            { label: 'Site', value: siteName },
            { label: 'Role', value: roleLabel(integration.kind) },
            { label: 'Provider', value: providerLabel(integration.providerKind) },
            { label: 'Integration ID', value: <span className="sw-mono">{integration.id}</span> },
          ]}
        />
        <div className="sw-resource-form-grid">
          <Field.Root required invalid={!draft.name.trim()}>
            <Field.Label>
              Name <Field.RequiredIndicator />
            </Field.Label>
            <Input value={draft.name} disabled={saving} onChange={(event) => update({ name: event.target.value })} />
          </Field.Root>
          <Field.Root required invalid={!draft.endpoint.trim()}>
            <Field.Label>
              Endpoint <Field.RequiredIndicator />
            </Field.Label>
            <Input type="url" value={draft.endpoint} disabled={saving} onChange={(event) => update({ endpoint: event.target.value })} />
          </Field.Root>
        </div>
        <Field.Root>
          <Checkbox id="integration-enabled" checked={draft.enabled} disabled={saving} onCheckedChange={(checked) => update({ enabled: checked })}>
            Enabled
          </Checkbox>
          <Field.HelperText>A paused Integration keeps its data, but swallow stops syncing and using it.</Field.HelperText>
        </Field.Root>
        <SaveRow dirty={dirty} saving={saving} canSave={dirty && valid && !saving} />
      </IntegrationSettingsPanel>
    </form>
  )
}

/**
 * Connection tab: the Integration `settings` map (timeout, TLS, automatic inspection, and
 * provider-specific keys), saved as a whole. Unknown keys the Dashboard does not edit are carried
 * through unchanged. The write sends the saved name, endpoint, and Enabled, so it never carries the
 * General tab's unsaved draft.
 */
export function IntegrationConnectionTab() {
  const { integration, connectionDraft, setConnectionDraft, onSaved } = useIntegrationSettingsContext()
  const { sites } = useApp()
  const { showToast } = useToast()
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const settings = connectionDraft ?? integration.settings
  const dirty = JSON.stringify(sortedEntries(settings)) !== JSON.stringify(sortedEntries(integration.settings))

  const save = async () => {
    if (!dirty || saving) return
    setSaving(true)
    setError('')
    try {
      const saved = await sites.updateIntegration(integration.id, {
        name: integration.name,
        endpoint: integration.endpoint,
        enabled: integration.enabled,
        settings,
      })
      setConnectionDraft(null)
      onSaved(saved)
      showToast({ tone: 'success', title: 'Connection settings saved' })
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The connection settings could not be saved.')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault()
        void save()
      }}
    >
      <IntegrationSettingsPanel
        id="integration-connection"
        title="Connection"
        description="How swallow talks to the provider and what it does with newly enrolled Servers. Saved with this Integration."
      >
        {error && (
          <Alert status="error" title="Connection settings could not be saved">
            {error}
          </Alert>
        )}
        <IntegrationConnectionFields
          kind={integration.kind}
          providerKind={integration.providerKind}
          settings={settings}
          disabled={saving}
          onSettingChange={(key, value) => setConnectionDraft(withIntegrationSetting(settings, key, value))}
        />
        <SaveRow dirty={dirty} saving={saving} canSave={dirty && !saving} />
      </IntegrationSettingsPanel>
    </form>
  )
}

/** The settings map as sorted entries, so two maps with the same keys and values compare equal. */
function sortedEntries(settings: Record<string, string>): Array<[string, string]> {
  return Object.entries(settings).sort(([left], [right]) => left.localeCompare(right))
}

/**
 * Credential tab: sets or replaces the Integration's write-only secret (contract
 * sites-integrations.md, `PUT /integrations/{id}/credential`). The wording says whether one exists
 * ("Set" vs "Replace") so an operator knows which they are doing. The value lives only in this
 * tab's state, is cleared on success and when the tab unmounts, and is never read back; the copy
 * warns that it cannot be viewed again.
 */
export function IntegrationCredentialTab() {
  const { integration, reload } = useIntegrationSettingsContext()
  const { sites } = useApp()
  const { showToast } = useToast()
  const [credential, setCredential] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const replacing = integration.hasCredential
  const term = credentialGuidance(integration.providerKind).term
  const action = replacing ? 'Replace' : 'Set'

  const save = async () => {
    if (!credential || saving) return
    setSaving(true)
    setError('')
    try {
      await sites.replaceIntegrationCredential(integration.id, credential)
      setCredential('')
      // The write returns no Integration, so read it again for its credential state.
      reload()
      showToast({ tone: 'success', title: replacing ? 'Credential replaced' : 'Credential set' })
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : `The credential could not be ${replacing ? 'replaced' : 'set'}.`)
    } finally {
      setSaving(false)
    }
  }

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault()
        void save()
      }}
    >
      <IntegrationSettingsPanel
        id="integration-credential"
        title="Credential"
        description={
          replacing
            ? `A ${term} is stored. swallow keeps it encrypted and never shows it; you can replace it here.`
            : `No ${term} is stored yet. swallow needs one to reach the provider.`
        }
      >
        {error && (
          <Alert status="error" title={`Credential could not be ${replacing ? 'replaced' : 'set'}`}>
            {error}
          </Alert>
        )}
        <CredentialGuidanceNote providerKind={integration.providerKind} />
        <Field.Root required>
          <Field.Label>
            {replacing ? `New ${term}` : term} <Field.RequiredIndicator />
          </Field.Label>
          <Input
            type="password"
            value={credential}
            disabled={saving}
            onChange={(event) => setCredential(event.target.value)}
            autoComplete="new-password"
          />
          <Field.HelperText>
            {replacing
              ? 'Replaces the stored value. It cannot be viewed again after saving.'
              : 'Stored encrypted. It cannot be viewed again after saving.'}
          </Field.HelperText>
        </Field.Root>
        <Stack align="flex-end">
          <Button type="submit" size="sm" colorPalette="brand" loading={saving} disabled={!credential || saving}>
            {action} credential
          </Button>
        </Stack>
      </IntegrationSettingsPanel>
    </form>
  )
}

/**
 * Sends a non-MAAS Integration that was deep-linked to a MAAS-only tab back to General, keeping the
 * query string (the Site scope) so the address bar stays shareable.
 */
function GeneralRedirect() {
  const location = useLocation()
  return <Navigate to={{ pathname: '../general', search: location.search }} relative="path" replace />
}

/** HTTP proxy tab: the provider-owned proxy of a MAAS provisioner (decision 056). */
export function IntegrationHttpProxyTab() {
  const { integration } = useIntegrationSettingsContext()
  if (!isMaasProvisioner(integration)) return <GeneralRedirect />
  return <ProvisionerHttpProxySection integration={integration} />
}

/** Subnet DNS tab: the provider-owned DNS servers of a MAAS provisioner's subnets (decision 058). */
export function IntegrationSubnetDnsTab() {
  const { integration } = useIntegrationSettingsContext()
  if (!isMaasProvisioner(integration)) return <GeneralRedirect />
  return <ProvisionerSubnetDnsSection integration={integration} />
}
