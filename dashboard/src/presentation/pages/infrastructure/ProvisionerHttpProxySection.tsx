import { useCallback, useState } from 'react'
import { Button, Field, HStack, Input, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import {
  httpProxyModeNeedsUrl,
  PROVISIONER_HTTP_PROXY_MODES,
  type ProvisionerHttpProxy,
  type ProvisionerHttpProxyConfig,
  type ProvisionerHttpProxyMode,
} from '@/domain/provisioning/types'
import type { Integration } from '@/domain/site/types'
import { AsyncSection } from '@/presentation/components/AsyncSection'
import { ConvergenceStatus } from '@/presentation/components/convergence/ConvergenceStatus'
import { describeConvergence, isSettling } from '@/presentation/components/convergence/convergencePresentation'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { Select } from '@/presentation/components/ui/select'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { useSettlingRefresh } from '@/presentation/hooks/useSettlingRefresh'
import { HTTP_PROXY_MODE_TEXT, httpProxyUrlProblem } from './httpProxyForm'
import { ProvisionerLiveSettingSection } from './ProvisionerLiveSettingSection'

const MODE_OPTIONS = PROVISIONER_HTTP_PROXY_MODES.map((mode) => ({ value: mode, label: HTTP_PROXY_MODE_TEXT[mode].label }))

interface ProvisionerHttpProxySectionProps {
  /** The stored MAAS provisioner Integration whose settings page shows this tab. */
  integration: Pick<Integration, 'id' | 'hasCredential'>
}

/**
 * The HTTP proxy tab of a MAAS provisioner's Integration settings page (contract
 * provisioner-http-proxy.md, decisions 056 and 071): the proxy MAAS hands to the machines it deploys.
 *
 * swallow keeps the value as desired state: the tab's own **Save proxy** button stores it, never the
 * General or Connection Save, because it is not an Integration setting, and the backend then applies
 * it to MAAS and puts back changes made in MAAS. Without a stored credential nothing is requested:
 * the API could not reach MAAS, so the section says what is missing instead of showing an error.
 * Loading, the API's error, and a vanished Integration use the shared AsyncSection states; the rest
 * of the page stays usable whatever this tab shows.
 */
export function ProvisionerHttpProxySection({ integration }: ProvisionerHttpProxySectionProps) {
  return (
    <ProvisionerLiveSettingSection
      integration={integration}
      id="integration-http-proxy"
      title="HTTP proxy"
      description="The proxy MAAS gives the machines it deploys for packages. swallow keeps it and applies it to MAAS; it is not saved with this Integration's connection."
      credentialNeededFor="reads and applies the proxy"
    >
      <HttpProxySetting integrationId={integration.id} />
    </ProvisionerLiveSettingSection>
  )
}

/**
 * Reads the setting once per mount (re-read when the Integration changes) and hands it to the form.
 * A 404 means the Integration was deleted meanwhile and renders as unavailable, not as a fault.
 */
function HttpProxySetting({ integrationId }: { integrationId: string }) {
  const { provisioning } = useApp()
  const proxy = useAsyncData(() => provisioning.getProvisionerHttpProxy(integrationId), [provisioning, integrationId])
  return (
    <AsyncSection
      state={proxy}
      loadingRows={2}
      unavailableTitle="HTTP proxy settings are unavailable"
      unavailableHint="Return to the Integrations list and refresh it."
    >
      {(current) => <HttpProxyForm integrationId={integrationId} current={current} />}
    </AsyncSection>
  )
}

/**
 * The editable form over one setting. `setting` is the stored setting (the read, then each save and
 * re-read), so "Not saved yet" compares the operator's choice with the spec rather than with the
 * opening state, and the status line says whether MAAS holds the spec and what MAAS reports. While a
 * saved change is still being applied, the setting is re-read quietly until it settles. A typed URL
 * is kept while another mode is chosen, so switching back does not lose it, but only modes that
 * route through it send it.
 */
function HttpProxyForm({ integrationId, current }: { integrationId: string; current: ProvisionerHttpProxy }) {
  const { provisioning } = useApp()
  const { showToast } = useToast()
  const [setting, setSetting] = useState(current)
  const [mode, setMode] = useState<ProvisionerHttpProxyMode>(current.spec.mode)
  const [url, setUrl] = useState(current.spec.httpProxy)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const settling = isSettling(describeConvergence(setting.status.conditions, setting.generation).state)
  const refresh = useCallback(async () => {
    setSetting(await provisioning.getProvisionerHttpProxy(integrationId))
  }, [provisioning, integrationId])
  useSettlingRefresh(settling, refresh)

  // A mode this console does not know (added to the API later) has no text; the select then shows
  // its placeholder and nothing can be saved until the operator chooses a known mode.
  const modeText: { label: string; help: string } | undefined = HTTP_PROXY_MODE_TEXT[mode]
  const needsUrl = httpProxyModeNeedsUrl(mode)
  const urlProblem = needsUrl ? httpProxyUrlProblem(url) : null
  const showUrlProblem = Boolean(urlProblem && url.trim())
  const requested = { mode, httpProxy: needsUrl ? url.trim() : '' }
  const dirty = requested.mode !== setting.spec.mode || requested.httpProxy !== setting.spec.httpProxy
  const canSave = Boolean(modeText) && dirty && !urlProblem && !saving

  const save = async () => {
    if (!canSave) return
    setSaving(true)
    setError('')
    try {
      const result = await provisioning.setProvisionerHttpProxy(integrationId, requested)
      setSetting(result)
      setMode(result.spec.mode)
      setUrl(result.spec.httpProxy)
      showToast({
        tone: 'success',
        title: 'HTTP proxy saved',
        description: 'swallow applies it to MAAS shortly; deployments MAAS starts afterwards use it.',
      })
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The HTTP proxy could not be saved.')
    } finally {
      setSaving(false)
    }
  }

  const observed = setting.status.observed

  return (
    <Stack gap="4">
      {error && (
        <Alert status="error" title="HTTP proxy could not be saved">
          {error}
        </Alert>
      )}
      <div className="sw-resource-form-grid">
        <Field.Root>
          <Field.Label>Proxy mode</Field.Label>
          <Select
            id="integration-http-proxy-mode"
            aria-label="Proxy mode"
            value={mode}
            placeholder="Choose a mode"
            // Every option is a known mode, so the value the select emits is one of them.
            onChange={(value) => setMode(value as ProvisionerHttpProxyMode)}
            options={MODE_OPTIONS}
            disabled={saving}
          />
          <Field.HelperText>{modeText?.help ?? 'Choose how deployed machines reach package repositories.'}</Field.HelperText>
        </Field.Root>
        {needsUrl && (
          <Field.Root required invalid={showUrlProblem}>
            <Field.Label>
              Proxy URL <Field.RequiredIndicator />
            </Field.Label>
            <Input
              type="url"
              value={url}
              placeholder="http://proxy.example.com:3128"
              disabled={saving}
              onChange={(event) => setUrl(event.target.value)}
              onKeyDown={(event) => {
                // Enter saves the proxy, which is what an operator typing a proxy URL means; the
                // default is suppressed so no enclosing form ever submits instead.
                if (event.key !== 'Enter') return
                event.preventDefault()
                void save()
              }}
            />
            {showUrlProblem ? (
              <Field.ErrorText>{urlProblem}</Field.ErrorText>
            ) : (
              <Field.HelperText>Scheme, host, and port, such as http://10.0.0.20:3128.</Field.HelperText>
            )}
          </Field.Root>
        )}
      </div>
      <HStack justify="space-between" align="flex-start" gap="3" wrap="wrap">
        <Stack gap="1" role="status">
          {dirty ? (
            <Text fontSize="sm">Not saved yet.</Text>
          ) : (
            <ConvergenceStatus conditions={setting.status.conditions} generation={setting.generation} />
          )}
          <Text fontSize="sm" color="fg.muted">
            MAAS reports: {observed ? httpProxySummary(observed) : 'not read yet'}.
          </Text>
        </Stack>
        <Button type="button" size="sm" colorPalette="brand" variant="outline" loading={saving} disabled={!canSave} onClick={() => void save()}>
          Save proxy
        </Button>
      </HStack>
    </Stack>
  )
}

/** One configuration in words, naming a mode this console does not know by its value. */
function httpProxySummary(config: ProvisionerHttpProxyConfig): string {
  const text: { label: string } | undefined = HTTP_PROXY_MODE_TEXT[config.mode]
  const label = text ? text.label : `an unrecognised mode (${config.mode})`
  return `${label}${config.httpProxy ? ` (${config.httpProxy})` : ''}`
}
