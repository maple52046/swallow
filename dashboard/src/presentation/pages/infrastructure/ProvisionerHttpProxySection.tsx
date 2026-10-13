import { useState } from 'react'
import { Button, Field, Heading, HStack, Input, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import {
  httpProxyModeNeedsUrl,
  PROVISIONER_HTTP_PROXY_MODES,
  type ProvisionerHttpProxy,
  type ProvisionerHttpProxyMode,
} from '@/domain/provisioning/types'
import type { Integration } from '@/domain/site/types'
import { AsyncSection } from '@/presentation/components/AsyncSection'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { Select } from '@/presentation/components/ui/select'
import { useAsyncData } from '@/presentation/hooks/useAsyncData'
import { HTTP_PROXY_MODE_TEXT, httpProxyUrlProblem } from './httpProxyForm'

const MODE_OPTIONS = PROVISIONER_HTTP_PROXY_MODES.map((mode) => ({ value: mode, label: HTTP_PROXY_MODE_TEXT[mode].label }))

interface ProvisionerHttpProxySectionProps {
  /** The stored MAAS provisioner Integration being edited; unsaved dialog edits do not apply here. */
  integration: Pick<Integration, 'id' | 'hasCredential'>
}

/**
 * The HTTP Proxy section of the Edit integration dialog for a MAAS provisioner (contract
 * provisioner-http-proxy.md, decision 056): the proxy MAAS hands to the machines it deploys.
 *
 * The value is provisioner-owned and live. It is read from MAAS when the dialog opens and written
 * through by the section's own **Apply proxy** button, never by the dialog's Save, because it is not
 * an Integration setting and a MAAS write must not hide inside the Integration PATCH. Without a
 * stored credential nothing is requested: the API could not reach MAAS, so the section says what is
 * missing instead of showing an error. Loading, the API's error, and a vanished Integration use the
 * shared AsyncSection states; the dialog itself stays usable whatever this section shows.
 */
export function ProvisionerHttpProxySection({ integration }: ProvisionerHttpProxySectionProps) {
  return (
    <section className="sw-integration-settings" aria-labelledby="integration-http-proxy-title">
      <Stack gap="1">
        <Heading as="h3" size="sm" id="integration-http-proxy-title">
          HTTP proxy
        </Heading>
        <Text fontSize="sm" color="fg.muted">
          The proxy MAAS gives the machines it deploys for packages. It is read from and applied to MAAS directly, not
          saved with this Integration.
        </Text>
      </Stack>
      {integration.hasCredential ? (
        <LiveHttpProxy integrationId={integration.id} />
      ) : (
        <Alert status="info" title="No MAAS API key">
          Set the Integration&apos;s credential first; swallow reads and applies the proxy with it.
        </Alert>
      )}
    </section>
  )
}

/**
 * Reads the live value once per mount (re-read when the Integration changes) and hands it to the
 * form. A 404 means the Integration was deleted meanwhile and renders as unavailable, not as a fault.
 */
function LiveHttpProxy({ integrationId }: { integrationId: string }) {
  const { provisioning } = useApp()
  const proxy = useAsyncData(() => provisioning.getProvisionerHttpProxy(integrationId), [provisioning, integrationId])
  return (
    <AsyncSection
      state={proxy}
      loadingRows={2}
      unavailableTitle="HTTP proxy settings are unavailable"
      unavailableHint="Close the dialog and refresh the Integrations list."
    >
      {(current) => <HttpProxyForm integrationId={integrationId} current={current} />}
    </AsyncSection>
  )
}

/**
 * The editable form over one read value. `applied` is what MAAS last reported (the read, then each
 * read-back), so "Not applied" compares the operator's choice with MAAS rather than with the opening
 * state. A typed URL is kept while another mode is chosen, so switching back does not lose it, but
 * only modes that route through it send it.
 */
function HttpProxyForm({ integrationId, current }: { integrationId: string; current: ProvisionerHttpProxy }) {
  const { provisioning } = useApp()
  const { showToast } = useToast()
  const [applied, setApplied] = useState(current)
  const [mode, setMode] = useState<ProvisionerHttpProxyMode>(current.mode)
  const [url, setUrl] = useState(current.httpProxy)
  const [applying, setApplying] = useState(false)
  const [error, setError] = useState('')

  // A mode this console does not know (added to the API later) has no text; the select then shows
  // its placeholder and nothing can be applied until the operator chooses a known mode.
  const modeText: { label: string; help: string } | undefined = HTTP_PROXY_MODE_TEXT[mode]
  const appliedText: { label: string; help: string } | undefined = HTTP_PROXY_MODE_TEXT[applied.mode]
  const needsUrl = httpProxyModeNeedsUrl(mode)
  const urlProblem = needsUrl ? httpProxyUrlProblem(url) : null
  const showUrlProblem = Boolean(urlProblem && url.trim())
  const requested = { mode, httpProxy: needsUrl ? url.trim() : '' }
  const dirty = requested.mode !== applied.mode || requested.httpProxy !== applied.httpProxy
  const canApply = Boolean(modeText) && dirty && !urlProblem && !applying

  const apply = async () => {
    if (!canApply) return
    setApplying(true)
    setError('')
    try {
      const result = await provisioning.setProvisionerHttpProxy(integrationId, requested)
      setApplied(result)
      setMode(result.mode)
      setUrl(result.httpProxy)
      showToast({ tone: 'success', title: 'HTTP proxy applied', description: 'MAAS uses it for deployments it starts from now on.' })
    } catch (caught) {
      // MAAS applies its settings one at a time, so a failure may follow a partial write; the
      // message says what failed and reopening the dialog shows what MAAS now holds.
      setError(caught instanceof Error ? caught.message : 'The HTTP proxy could not be applied.')
    } finally {
      setApplying(false)
    }
  }

  const appliedSummary = appliedText
    ? `${appliedText.label}${applied.httpProxy ? ` (${applied.httpProxy})` : ''}`
    : `an unrecognised mode (${applied.mode})`

  return (
    <Stack gap="4">
      {error && (
        <Alert status="error" title="HTTP proxy could not be applied">
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
            disabled={applying}
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
              disabled={applying}
              onChange={(event) => setUrl(event.target.value)}
              onKeyDown={(event) => {
                // The dialog is a form whose Enter saves the Integration; Enter here applies the
                // proxy instead, which is what an operator typing a proxy URL means.
                if (event.key !== 'Enter') return
                event.preventDefault()
                void apply()
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
      <HStack justify="space-between" gap="3" wrap="wrap">
        <Text role="status" fontSize="sm" color={dirty ? 'fg' : 'fg.muted'}>
          {dirty ? 'Not applied yet. Save changes does not apply the proxy.' : `MAAS uses: ${appliedSummary}.`}
        </Text>
        <Button type="button" size="sm" colorPalette="brand" variant="outline" loading={applying} disabled={!canApply} onClick={() => void apply()}>
          Apply proxy
        </Button>
      </HStack>
    </Stack>
  )
}
