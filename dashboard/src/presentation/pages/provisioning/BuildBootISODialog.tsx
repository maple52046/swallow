import { useState } from 'react'
import { Box, Button, Field, HStack, Input, Stack, Text } from '@chakra-ui/react'
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
 * A single provisioner is summarized read-only; only a multi-provisioner scope needs a selector.
 * The selected Integration suggests the MAAS rack host and an advanced ISO name, while either field
 * remains operator-editable and is never overwritten after editing. The iPXE script is swallow's
 * fixed template (DHCP from the Site network, then chain the rack's `ipxe.cfg`), so there is no
 * script field and the dialog previews the final chain URL instead. The API builds synchronously in
 * seconds; while it runs, dismissal is blocked so the request cannot be abandoned, and a refusal
 * (name taken, builder unavailable, packaging failure) is shown inline with the API's message so the
 * operator can correct it and retry.
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
  const selectedIntegration = integrations.find((item) => item.id === draft.integrationId)
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
      description="Build the optional iPXE path for Servers that obtain DHCP outside the provisioner network. The BMC mounts the ISO, then iPXE chains to the selected MAAS rack."
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
            Build Boot ISO
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
        {single ? (
          <div className="sw-boot-iso-integration-summary" aria-label="Provisioner Integration">
            <Text color="fg.muted" fontSize="xs" fontWeight="medium">
              Provisioner Integration
            </Text>
            <Text fontWeight="semibold">{single.name}</Text>
            <Text className="sw-mono" color="fg.muted" fontSize="sm">
              {single.endpoint}
            </Text>
          </div>
        ) : (
          <Field.Root required>
            <Field.Label htmlFor="boot-iso-integration">
              Provisioner Integration <Field.RequiredIndicator />
            </Field.Label>
            <Select
              id="boot-iso-integration"
              value={draft.integrationId}
              aria-label="Provisioner Integration"
              placeholder="Select a provisioner"
              disabled={busy}
              onChange={chooseIntegration}
              options={integrations.map((integration) => ({ value: integration.id, label: integration.name }))}
            />
            <Field.HelperText>Only Servers connected to this Integration can use the ISO.</Field.HelperText>
          </Field.Root>
        )}
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
            Suggested from {selectedIntegration ? `${selectedIntegration.name}'s` : 'the selected Integration'} endpoint.
            Use the hostname or IPv4 address that Servers can reach from the Site DHCP network.
          </Field.HelperText>
        </Field.Root>
        {chainURL && (
          <Box className="sw-boot-iso-chain-preview" aria-live="polite">
            <Text color="fg.muted" fontSize="xs" fontWeight="medium">
              Boot path preview
            </Text>
            <Text fontSize="sm">
              Site DHCP →{' '}
              <Text as="span" className="sw-mono">
                {chainURL}
              </Text>
            </Text>
          </Box>
        )}
        <Alert status="warning" title="Secure Boot must be off">
          The generated iPXE image is unsigned. Disable Secure Boot on Servers that mount this ISO.
        </Alert>
        <Box as="details" className="sw-boot-iso-advanced">
          <Box as="summary">Advanced settings</Box>
          <Field.Root required>
            <Field.Label>
              Boot ISO name <Field.RequiredIndicator />
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
            <Field.HelperText>
              Generated from the Integration name and unique within that Integration.
            </Field.HelperText>
          </Field.Root>
        </Box>
      </Stack>
    </Modal>
  )
}
