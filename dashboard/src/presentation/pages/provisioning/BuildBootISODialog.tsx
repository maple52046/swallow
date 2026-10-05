import { useState } from 'react'
import { Button, Field, HStack, Input, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import { bootISOChainURL, type BootISO } from '@/domain/provisioning/types'
import type { Integration } from '@/domain/site/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { rackAddressFromEndpoint, suggestedBootISOName } from './bootISOForm'

interface BuildBootISODialogProps {
  /** The provisioner Integrations of the current Site scope; the dialog offers only these. */
  integrations: readonly Integration[]
  onClose: () => void
  /** Called with the built ISO after the API stored it, so the caller can refresh and announce it. */
  onBuilt: (iso: BootISO) => void
}

/** The form as typed; values are trimmed only when sent, so the operator's input is never rewritten. */
interface BuildDraft {
  integrationId: string
  name: string
  rackAddress: string
}

/**
 * Builds a Boot ISO for one provisioner from the Provisioning page's Boot ISOs tab (decision 049).
 *
 * The operator picks the provisioner and confirms the MAAS rack address; the iPXE script is always
 * swallow's verified template (DHCP from the site network, then chain the rack's `ipxe.cfg`), so
 * there is no script field — the dialog previews the chain URL instead. Picking a provisioner
 * pre-fills the name and the rack address (its endpoint host); a field the operator already edited
 * is not overwritten. The API builds synchronously in seconds; while it runs, dismissal is blocked
 * so the request cannot be abandoned, and a refusal (name taken, builder unavailable, packaging
 * failure) is shown inline with the API's message so the operator can correct it and retry.
 */
export function BuildBootISODialog({ integrations, onClose, onBuilt }: BuildBootISODialogProps) {
  const { provisioning } = useApp()
  const single = integrations.length === 1 ? integrations[0] : undefined
  const [draft, setDraft] = useState<BuildDraft>(() =>
    single
      ? { integrationId: single.id, name: suggestedBootISOName(single.name), rackAddress: rackAddressFromEndpoint(single.endpoint) }
      : { integrationId: '', name: '', rackAddress: '' },
  )
  const [edited, setEdited] = useState({ name: false, rackAddress: false })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const chainURL = bootISOChainURL(draft.rackAddress)
  const ready = Boolean(draft.integrationId && draft.name.trim() && draft.rackAddress.trim())

  const chooseIntegration = (integrationId: string) => {
    const integration = integrations.find((item) => item.id === integrationId)
    setDraft((current) => ({
      integrationId,
      name: edited.name || !integration ? current.name : suggestedBootISOName(integration.name),
      rackAddress: edited.rackAddress || !integration ? current.rackAddress : rackAddressFromEndpoint(integration.endpoint),
    }))
  }

  const submit = async () => {
    if (busy || !ready) return
    setBusy(true)
    setError('')
    try {
      const iso = await provisioning.createBootISO({
        integrationId: draft.integrationId,
        name: draft.name.trim(),
        rackAddress: draft.rackAddress.trim(),
      })
      onBuilt(iso)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The Boot ISO could not be built.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      onClose={() => !busy && onClose()}
      closeOnInteractOutside={!busy}
      title="Build Boot ISO"
      description="An iPXE ISO that takes an address from the site network's DHCP and chains to the provisioner's MAAS rack. Servers of that provisioner can then boot it through their BMC (Boot Media)."
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
      footer={
        <HStack gap="2">
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button type="submit" colorPalette="brand" loading={busy} loadingText="Building…" disabled={busy || !ready}>
            Build ISO
          </Button>
        </HStack>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="The Boot ISO was not built">
            {error}
          </Alert>
        )}
        <Field.Root required>
          <Field.Label htmlFor="boot-iso-integration">
            Provisioner <Field.RequiredIndicator />
          </Field.Label>
          <Select
            id="boot-iso-integration"
            value={draft.integrationId}
            aria-label="Provisioner"
            placeholder="Select a provisioner"
            disabled={busy}
            onChange={chooseIntegration}
            options={integrations.map((integration) => ({ value: integration.id, label: integration.name }))}
          />
          <Field.HelperText>Only Servers of this provisioner can use the ISO.</Field.HelperText>
        </Field.Root>
        <Field.Root required>
          <Field.Label>
            Name <Field.RequiredIndicator />
          </Field.Label>
          <Input
            value={draft.name}
            maxLength={63}
            disabled={busy}
            autoComplete="off"
            onChange={(event) => {
              setEdited((current) => ({ ...current, name: true }))
              setDraft((current) => ({ ...current, name: event.target.value }))
            }}
          />
          <Field.HelperText>Unique per provisioner; shown when choosing a Server&apos;s Boot Media.</Field.HelperText>
        </Field.Root>
        <Field.Root required>
          <Field.Label>
            MAAS rack address <Field.RequiredIndicator />
          </Field.Label>
          <Input
            value={draft.rackAddress}
            className="sw-mono"
            placeholder="10.0.0.2"
            disabled={busy}
            autoComplete="off"
            spellCheck={false}
            onChange={(event) => {
              setEdited((current) => ({ ...current, rackAddress: true }))
              setDraft((current) => ({ ...current, rackAddress: event.target.value }))
            }}
          />
          <Field.HelperText>
            The rack controller&apos;s IPv4 address or hostname, optionally with <Text as="span" className="sw-mono">:port</Text>{' '}
            (default 5248). Pre-filled with the provisioner endpoint&apos;s host, which is the rack when MAAS runs region and
            rack on one machine. The Servers must reach it from the network their DHCP serves.
          </Field.HelperText>
        </Field.Root>
        {chainURL && (
          <Text fontSize="sm" aria-live="polite">
            Boots, takes a DHCP lease, then chains to{' '}
            <Text as="span" className="sw-mono">
              {chainURL}
            </Text>
            .
          </Text>
        )}
        <Text color="fg.muted" fontSize="sm">
          iPXE is unsigned: Servers booting the ISO must have Secure Boot turned off.
        </Text>
      </Stack>
    </Modal>
  )
}
