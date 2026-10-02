import { useMemo, useState } from 'react'
import { Box, Button, Field, HStack, Input, Stack, Text } from '@chakra-ui/react'
import { useApp } from '@/di/AppProvider'
import type { Server } from '@/domain/server/types'
import { serverDisplayName } from '@/domain/server/types'
import type {
  InstallSoftwareTarget,
  SoftwareCatalogEntry,
  SoftwareKind,
  SoftwareRole,
} from '@/domain/software/types'
import { DockerApiRiskNotice } from '@/presentation/components/DockerApiRiskNotice'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { softwareKindLabel } from './softwarePresentation'

interface InstallSoftwareDialogProps {
  /** The installable catalog fetched by the page; drives the kind picker and its rules. */
  catalog: SoftwareCatalogEntry[]
  /** Deployed Servers eligible as install targets, already scoped to the active Site. */
  servers: Server[]
  onClose: () => void
  /** Called with the accepted Workflow id so the page can navigate to its progress view. */
  onLaunched: (operationId: string) => void
}

/**
 * Installs one software kind on one or more deployed Servers.
 *
 * The operator picks a kind, selects target Servers, assigns per-Server roles for a kind that has
 * variants (NFS), and fills the kind-specific spec (NFS export/mount, an optional runtime version,
 * and for Docker CE the `enableApi` variant — checked by default to match the contract default, with
 * the shared risk notice beside it). Submit stays disabled until the selection satisfies the kind's
 * role and spec rules, so the obvious client-side mistakes are caught before a Workflow is created;
 * the backend remains the authority on state, mutual exclusion, and Kubernetes-member refusal, and
 * its error is surfaced inline. Dismissal is blocked while the request is in flight so a double
 * submit cannot occur.
 */
export function InstallSoftwareDialog({ catalog, servers, onClose, onLaunched }: InstallSoftwareDialogProps) {
  const { software } = useApp()
  const [kind, setKind] = useState<SoftwareKind | ''>(catalog[0]?.kind ?? '')
  // Selected targets keyed by serverId; the value is the chosen roles (empty for a role-less kind).
  const [selected, setSelected] = useState<Record<string, SoftwareRole[]>>({})
  const [spec, setSpec] = useState<Record<string, string>>({})
  // Docker CE's enableApi is the only boolean spec field; it starts checked because the contract
  // records an omitted value as true, so the form shows what will actually be applied.
  const [enableApi, setEnableApi] = useState(true)
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const entry = useMemo(() => catalog.find((item) => item.kind === kind), [catalog, kind])
  const hasRoles = (entry?.roles.length ?? 0) > 0

  const selectedIds = Object.keys(selected)
  const specValue = (field: string) => spec[field] ?? ''
  const roleFor = (serverId: string): SoftwareRole[] => selected[serverId] ?? []

  const toggleServer = (serverId: string, checked: boolean) => {
    setSelected((current) => {
      const next = { ...current }
      if (checked) {
        next[serverId] = current[serverId] ?? []
      } else {
        delete next[serverId]
      }
      return next
    })
  }

  const toggleRole = (serverId: string, role: SoftwareRole, checked: boolean) => {
    setSelected((current) => {
      const roles = new Set(current[serverId] ?? [])
      if (checked) roles.add(role)
      else roles.delete(role)
      return { ...current, [serverId]: [...roles] }
    })
  }

  // Client-side validity mirrors the contract's required fields so the operator gets immediate
  // feedback; the backend still enforces the authoritative rules.
  const valid = useMemo(() => {
    if (!entry || selectedIds.length === 0) return false
    if (hasRoles) {
      const everyTargetHasRole = selectedIds.every((serverId) => roleFor(serverId).length > 0)
      if (!everyTargetHasRole) return false
      const anyServer = selectedIds.some((serverId) => roleFor(serverId).includes('server'))
      const anyClient = selectedIds.some((serverId) => roleFor(serverId).includes('client'))
      if (anyServer && specValue('exportPath').trim() === '') return false
      if (anyClient && (specValue('source').trim() === '' || specValue('mountPath').trim() === '')) return false
    }
    return true
    // roleFor/specValue read the same state the deps cover.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [entry, hasRoles, selected, spec])

  const buildSpec = (): Record<string, unknown> | undefined => {
    if (!entry) return undefined
    const result: Record<string, unknown> = {}
    for (const field of entry.specFields) {
      if (field === 'enableApi') {
        // Sent explicitly (never omitted) so an unchecked box cannot fall back to the default.
        result.enableApi = enableApi
        continue
      }
      const value = specValue(field).trim()
      if (value !== '') result[field] = value
    }
    return Object.keys(result).length > 0 ? result : undefined
  }

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (!valid || !entry || submitting) return
    setSubmitting(true)
    setError('')
    try {
      const assignments: InstallSoftwareTarget[] = selectedIds.map((serverId) => ({
        serverId,
        roles: roleFor(serverId),
      }))
      const result = await software.installSoftware({ kind: entry.kind, assignments, spec: buildSpec() })
      onLaunched(result.operationId)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The software could not be installed.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      size="lg"
      title="Install software"
      description="Install a single piece of host software on one or more deployed Servers. Server and client are variants of the same software, not separate platforms."
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
            Install
          </Button>
        </>
      }
    >
      <Stack gap="4">
        {error && (
          <Alert status="error" title="The software could not be installed">
            {error}
          </Alert>
        )}

        <Field.Root required>
          <Field.Label>
            Software <Field.RequiredIndicator />
          </Field.Label>
          <Select
            id="software-kind"
            aria-label="Software"
            value={kind}
            onChange={(value) => {
              setKind(value as SoftwareKind)
              setSelected({})
              setSpec({})
              setEnableApi(true)
            }}
            options={catalog.map((item) => ({ value: item.kind, label: softwareKindLabel(item.kind, item.label) }))}
          />
          {entry?.refusedForKubernetesMembers && (
            <Field.HelperText>Cannot be installed on a Kubernetes platform member.</Field.HelperText>
          )}
        </Field.Root>

        {/* A labelled group, not a Field.Root: Field.Root associates its id/label with a single
            control, so wrapping many Checkboxes in one made every label point at the first
            checkbox (only the first was selectable). A role="group" with an aria-labelledby label
            keeps each Checkbox's own generated id intact so all rows toggle independently. */}
        <Box as="section" role="group" aria-labelledby="software-targets-label">
          <Text id="software-targets-label" fontWeight="medium" mb="1">
            Target Servers{' '}
            <Text as="span" color="red.fg" aria-hidden>
              *
            </Text>
          </Text>
          {servers.length === 0 ? (
            <Text color="fg.muted" fontSize="sm">
              No deployed Servers are available in this Site.
            </Text>
          ) : (
            <Stack as="ul" gap="2" listStyleType="none" maxH="56" overflowY="auto" width="full">
              {servers.map((server) => {
                const checked = server.id in selected
                return (
                  <Box as="li" key={server.id}>
                    <HStack justify="space-between" gap="3" wrap="wrap">
                      <Checkbox
                        checked={checked}
                        onCheckedChange={(next) => toggleServer(server.id, next)}
                      >
                        {serverDisplayName(server)}
                      </Checkbox>
                      {checked && hasRoles && (
                        <HStack gap="3" aria-label={`Roles for ${serverDisplayName(server)}`}>
                          {entry?.roles.map((role) => (
                            <Checkbox
                              key={role}
                              checked={roleFor(server.id).includes(role)}
                              onCheckedChange={(next) => toggleRole(server.id, role, next)}
                            >
                              {role}
                            </Checkbox>
                          ))}
                        </HStack>
                      )}
                    </HStack>
                  </Box>
                )
              })}
            </Stack>
          )}
        </Box>

        {entry && renderSpecFields(entry, specValue, (field, value) => setSpec((current) => ({ ...current, [field]: value })))}

        {entry?.specFields.includes('enableApi') && (
          <Stack gap="2">
            <Checkbox checked={enableApi} onCheckedChange={setEnableApi}>
              Enable the Docker Engine API (required for the Server&apos;s Containers tab)
            </Checkbox>
            <DockerApiRiskNotice context="option" />
          </Stack>
        )}
      </Stack>
    </Modal>
  )
}

/**
 * Renders the kind-specific spec inputs. NFS gets export/mount fields; a container runtime gets an
 * optional pinned version. Fields are omitted from the payload when left blank, and required NFS
 * fields are enforced by the dialog's validity check.
 */
function renderSpecFields(
  entry: SoftwareCatalogEntry,
  value: (field: string) => string,
  onChange: (field: string, value: string) => void,
) {
  if (entry.kind === 'nfs') {
    return (
      <Stack gap="4">
        <Field.Root>
          <Field.Label>Export path (server)</Field.Label>
          <Input
            value={value('exportPath')}
            onChange={(event) => onChange('exportPath', event.target.value)}
            placeholder="/export/data"
          />
          <Field.HelperText>Required when a Server takes the server role.</Field.HelperText>
        </Field.Root>
        <Field.Root>
          <Field.Label>Export options (server)</Field.Label>
          <Input
            value={value('exportOptions')}
            onChange={(event) => onChange('exportOptions', event.target.value)}
            placeholder="rw,sync,no_subtree_check,root_squash"
          />
        </Field.Root>
        <Field.Root>
          <Field.Label>Source (client)</Field.Label>
          <Input
            value={value('source')}
            onChange={(event) => onChange('source', event.target.value)}
            placeholder="10.0.0.9:/export/data"
          />
          <Field.HelperText>Required when a Server takes the client role (host:/path).</Field.HelperText>
        </Field.Root>
        <Field.Root>
          <Field.Label>Mount path (client)</Field.Label>
          <Input
            value={value('mountPath')}
            onChange={(event) => onChange('mountPath', event.target.value)}
            placeholder="/shared"
          />
        </Field.Root>
        <Field.Root>
          <Field.Label>Mount options (client)</Field.Label>
          <Input
            value={value('mountOptions')}
            onChange={(event) => onChange('mountOptions', event.target.value)}
            placeholder="rw,_netdev"
          />
        </Field.Root>
      </Stack>
    )
  }
  if (entry.specFields.includes('version')) {
    return (
      <Field.Root>
        <Field.Label>Version</Field.Label>
        <Input
          value={value('version')}
          onChange={(event) => onChange('version', event.target.value)}
          placeholder="Leave blank for the latest available"
        />
      </Field.Root>
    )
  }
  return null
}
