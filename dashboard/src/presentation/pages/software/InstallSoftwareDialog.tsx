import { useMemo, useState } from 'react'
import { Badge, Box, Button, Field, HStack, Input, RadioCard, Stack, Text } from '@chakra-ui/react'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { Link as RouterLink } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type { Server } from '@/domain/server/types'
import { serverDisplayName } from '@/domain/server/types'
import type {
  SoftwareAssignment,
  SoftwareCatalogEntry,
  SoftwareKind,
  SoftwareRole,
} from '@/domain/software/types'
import { DockerApiRiskNotice } from '@/presentation/components/DockerApiRiskNotice'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { Modal } from '@/presentation/components/ui/modal'
import { SearchInput } from '@/presentation/components/ui/search-input'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import {
  groupSoftwareCatalog,
  softwareCatalogPresentation,
  softwareTargetBlocker,
  softwareTargetBlockerLabel,
  softwareTargetMatches,
} from './softwareCatalogPresentation'
import {
  buildSoftwareInstallInput,
  isSoftwareInstallConfigurationValid,
  softwareInstallActionLabel,
  softwareInstallDefaults,
  softwareInstallMode,
} from './softwareInstallForm'

interface InstallSoftwareDialogProps {
  /** The complete installable catalog; one dimension may be fixed by the caller. */
  catalog: SoftwareCatalogEntry[]
  /** Site-scoped deployed Servers offered by the Software detail flow, or one fixed Server. */
  servers: Server[]
  /** Current assignments used for existing-state, mutual-exclusion, retry, and reconfigure behavior. */
  assignments: SoftwareAssignment[]
  /** Software detail fixes the kind and asks for Servers first. */
  fixedKind?: SoftwareKind
  /** Server detail fixes one target and asks for software first. */
  fixedServer?: Server
  onClose: () => void
  /** Called with the accepted Workflow id so the route can navigate to its progress view. */
  onLaunched: (operationId: string) => void
}

type InstallStep = 'software' | 'servers' | 'configuration'

/**
 * Shared two-dimensional Managed Software installation flow.
 *
 * Software detail fixes the software dimension (Servers to Configuration); Server detail fixes the
 * target dimension (Software to Configuration). When both are fixed, row-level Retry/Reconfigure
 * opens directly on the prefilled configuration. Existing assignments never join a new multi-node
 * install, so one shared spec cannot silently overwrite heterogeneous Server configuration.
 */
export function InstallSoftwareDialog({
  catalog,
  servers,
  assignments,
  fixedKind,
  fixedServer,
  onClose,
  onLaunched,
}: InstallSoftwareDialogProps) {
  const { software } = useApp()
  const { siteId, sites, scopedHref } = useSiteScope()
  const initialEntry = fixedKind ? catalog.find((item) => item.kind === fixedKind) : undefined
  const initialAssignment = initialEntry && fixedServer
    ? assignments.find((item) => item.kind === initialEntry.kind && item.serverId === fixedServer.id)
    : undefined
  const initialDefaults = initialEntry ? softwareInstallDefaults(initialEntry, initialAssignment) : undefined
  const [step, setStep] = useState<InstallStep>(
    fixedKind && fixedServer ? 'configuration' : fixedKind ? 'servers' : 'software',
  )
  const [kind, setKind] = useState<SoftwareKind | ''>(fixedKind ?? '')
  const [selected, setSelected] = useState<Record<string, SoftwareRole[]>>(
    fixedServer ? { [fixedServer.id]: initialDefaults?.roles ?? [] } : {},
  )
  const [spec, setSpec] = useState<Record<string, string>>(initialDefaults?.spec ?? {})
  const [enableApi, setEnableApi] = useState(initialDefaults?.enableApi ?? true)
  const [targetQuery, setTargetQuery] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const entry = useMemo(() => catalog.find((item) => item.kind === kind), [catalog, kind])
  const selectedIds = Object.keys(selected)
  const existingAssignment = entry && fixedServer
    ? assignments.find((item) => item.kind === entry.kind && item.serverId === fixedServer.id)
    : undefined
  const mode = softwareInstallMode(existingAssignment)
  const hasRoles = (entry?.roles.length ?? 0) > 0
  const specValue = (field: string) => spec[field] ?? ''
  const roleFor = (serverId: string): SoftwareRole[] => selected[serverId] ?? []
  const siteName = (siteId: string) => sites.find((site) => site.id === siteId)?.name ?? siteId
  const showSite = !fixedServer && !siteId

  const visibleServers = useMemo(
    () => servers.filter((server) => softwareTargetMatches(server, targetQuery)),
    [servers, targetQuery],
  )
  const availableVisibleServers = useMemo(
    () => entry
      ? visibleServers.filter((server) => softwareTargetBlocker(server, entry, assignments) === null)
      : [],
    [assignments, entry, visibleServers],
  )

  const chooseKind = (nextKind: SoftwareKind) => {
    const nextEntry = catalog.find((item) => item.kind === nextKind)
    if (!nextEntry) return
    const assignment = fixedServer
      ? assignments.find((item) => item.kind === nextKind && item.serverId === fixedServer.id)
      : undefined
    const defaults = softwareInstallDefaults(nextEntry, assignment)
    setKind(nextKind)
    setSelected(fixedServer ? { [fixedServer.id]: defaults.roles } : {})
    setSpec(defaults.spec)
    setEnableApi(defaults.enableApi)
    setError('')
  }

  const softwareChoiceBlocker = (candidate: SoftwareCatalogEntry): string | null => {
    if (!fixedServer) return null
    const current = assignments.find((item) => item.serverId === fixedServer.id && item.kind === candidate.kind)
    if (current?.state === 'pending') return 'Installation is already in progress.'
    if (current?.state === 'uninstalling') return 'Uninstall is already in progress.'
    if (current?.state === 'installed' || current?.state === 'failed') return null
    const blocker = softwareTargetBlocker(fixedServer, candidate, assignments)
    return blocker ? softwareTargetBlockerLabel(blocker) : null
  }

  const toggleServer = (serverId: string, checked: boolean) => {
    setSelected((current) => {
      const next = { ...current }
      if (checked) next[serverId] = current[serverId] ?? []
      else delete next[serverId]
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

  const configurationValid = isSoftwareInstallConfigurationValid({
    entry,
    serverIds: selectedIds,
    roles: selected,
    spec,
  })

  const close = () => {
    if (!submitting) onClose()
  }

  const submit = async () => {
    if (!entry || !configurationValid || submitting) return
    setSubmitting(true)
    setError('')
    try {
      const result = await software.installSoftware(buildSoftwareInstallInput({
        entry,
        serverIds: selectedIds,
        roles: selected,
        spec,
        enableApi,
      }))
      onLaunched(result.operationId)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'The software could not be installed.')
    } finally {
      setSubmitting(false)
    }
  }

  const canContinue = step === 'software' ? Boolean(entry) : selectedIds.length > 0
  const hasPreviousStep = !(fixedKind && fixedServer)
  const title = mode === 'reconfigure' && entry
    ? `Reconfigure ${entry.label}`
    : mode === 'retry' && entry
      ? `Retry ${entry.label} installation`
      : 'Install software'
  const description = fixedServer
    ? `Choose and configure software for ${serverDisplayName(fixedServer)}.`
    : entry
      ? `Install ${entry.label} on one or more deployed Servers.`
      : 'Install one Managed Software kind on deployed Servers.'

  return (
    <Modal
      open
      onClose={close}
      closeOnInteractOutside={!submitting}
      dismissDisabled={submitting}
      size="xl"
      title={title}
      description={description}
      contentClassName="sw-software-install-dialog"
      onSubmit={(event) => {
        event.preventDefault()
        if (step === 'configuration') void submit()
      }}
      footer={
        <>
          <Button variant="ghost" onClick={close} disabled={submitting}>Cancel</Button>
          {step === 'configuration' ? (
            <>
              {hasPreviousStep && (
                <Button
                  variant="outline"
                  onClick={() => setStep(fixedKind ? 'servers' : 'software')}
                  disabled={submitting}
                >
                  <ChevronLeft size={16} aria-hidden />
                  Back
                </Button>
              )}
              <Button
                type="submit"
                colorPalette="brand"
                loading={submitting}
                disabled={!configurationValid || submitting}
              >
                {softwareInstallActionLabel(mode)}
              </Button>
            </>
          ) : (
            <Button
              colorPalette="brand"
              disabled={!canContinue}
              onClick={() => setStep('configuration')}
            >
              Next
              <ChevronRight size={16} aria-hidden />
            </Button>
          )}
        </>
      }
    >
      <Stack gap="5">
        {hasPreviousStep && (
          <HStack as="ol" className="sw-software-install-progress" aria-label="Installation progress">
            {(fixedKind ? ['Servers', 'Configuration'] : ['Software', 'Configuration']).map((label, index) => {
              const active = step === 'configuration' ? index === 1 : index === 0
              return (
                <HStack as="li" key={label} gap="2" data-active={active || undefined}>
                  <span>{index + 1}</span>
                  <Text>{label}</Text>
                </HStack>
              )
            })}
          </HStack>
        )}

        {error && (
          <Alert status="error" title="The software could not be installed">{error}</Alert>
        )}

        {step === 'software' && fixedServer && (
          <Stack gap="6">
            {groupSoftwareCatalog(catalog).map((group) => (
              <Box as="section" key={group.key} aria-labelledby={`software-picker-${group.key}`}>
                <Text id={`software-picker-${group.key}`} fontWeight="semibold" mb="2">{group.label}</Text>
                <RadioCard.Root
                  aria-label={group.label}
                  value={kind}
                  colorPalette="brand"
                  className="sw-software-picker-grid"
                  onValueChange={(details) => {
                    if (details.value) chooseKind(details.value as SoftwareKind)
                  }}
                >
                  {group.entries.map((candidate) => {
                    const presentation = softwareCatalogPresentation(candidate.kind)
                    const blocker = softwareChoiceBlocker(candidate)
                    const assignment = assignments.find((item) =>
                      item.serverId === fixedServer.id && item.kind === candidate.kind
                    )
                    const Icon = presentation.icon
                    return (
                      <Box key={candidate.kind} className="sw-software-picker-option">
                        <RadioCard.Item
                          value={candidate.kind}
                          disabled={Boolean(blocker)}
                          className="sw-software-picker-item"
                        >
                          <RadioCard.ItemHiddenInput />
                          <RadioCard.ItemControl className="sw-software-picker-card">
                            <RadioCard.ItemContent>
                              <HStack align="flex-start" gap="3">
                                <span className="sw-software-picker-card__icon" aria-hidden><Icon size={20} /></span>
                                <Box minW="0">
                                  <RadioCard.ItemText>{candidate.label}</RadioCard.ItemText>
                                  <RadioCard.ItemDescription>{presentation.description}</RadioCard.ItemDescription>
                                  {assignment && (
                                    <Badge mt="2" variant="subtle" colorPalette={assignment.state === 'failed' ? 'red' : assignment.state === 'installed' ? 'green' : 'yellow'}>
                                      {assignment.state === 'installed' ? 'Installed' : assignment.state === 'failed' ? 'Install failed' : assignment.state === 'pending' ? 'Installing' : 'Uninstalling'}
                                    </Badge>
                                  )}
                                  {blocker && <Text color="fg.muted" fontSize="xs" mt="2">{blocker}</Text>}
                                </Box>
                              </HStack>
                            </RadioCard.ItemContent>
                            <RadioCard.ItemIndicator />
                          </RadioCard.ItemControl>
                        </RadioCard.Item>
                        {assignment && (assignment.state === 'pending' || assignment.state === 'uninstalling') && (
                          <Button asChild variant="plain" size="xs" mt="1">
                            <RouterLink to={scopedHref(`/workflows/${assignment.lastWorkflowId}`)}>View workflow</RouterLink>
                          </Button>
                        )}
                      </Box>
                    )
                  })}
                </RadioCard.Root>
              </Box>
            ))}
          </Stack>
        )}

        {step === 'servers' && entry && (
          <Stack gap="4">
            <HStack justify="space-between" gap="3" wrap="wrap">
              <SearchInput
                value={targetQuery}
                onChange={setTargetQuery}
                placeholder="Search Servers, addresses, or tags"
                aria-label="Search installation targets"
              />
              <Text color="fg.muted" fontSize="sm" role="status">{selectedIds.length} selected</Text>
            </HStack>
            {visibleServers.length > 0 && (
              <Checkbox
                checked={
                  availableVisibleServers.length > 0 &&
                  availableVisibleServers.every((server) => server.id in selected)
                    ? true
                    : availableVisibleServers.some((server) => server.id in selected)
                      ? 'indeterminate'
                      : false
                }
                disabled={availableVisibleServers.length === 0}
                onCheckedChange={(checked) => {
                  for (const server of availableVisibleServers) toggleServer(server.id, checked)
                }}
              >
                Select all available results
              </Checkbox>
            )}
            {visibleServers.length === 0 ? (
              <Text color="fg.muted">No Servers match this search.</Text>
            ) : (
              <Stack as="ul" className="sw-software-target-list" gap="0" listStyleType="none">
                {visibleServers.map((server) => {
                  const blocker = softwareTargetBlocker(server, entry, assignments)
                  return (
                    <Box as="li" key={server.id} className="sw-software-target-row" data-disabled={Boolean(blocker) || undefined}>
                      <Checkbox
                        checked={server.id in selected}
                        disabled={Boolean(blocker)}
                        onCheckedChange={(checked) => toggleServer(server.id, checked)}
                        aria-label={`Select ${serverDisplayName(server)}`}
                      />
                      <Box minW="0" flex="1">
                        <Text fontWeight="semibold">{serverDisplayName(server)}</Text>
                        <Text color="fg.muted" fontSize="sm">
                          {[server.addresses[0], showSite ? siteName(server.source.siteId) : ''].filter(Boolean).join(' · ') || server.id}
                        </Text>
                        {blocker && <Text color="fg.muted" fontSize="xs">{softwareTargetBlockerLabel(blocker)}</Text>}
                      </Box>
                      <HStack gap="1" wrap="wrap" justify="flex-end">
                        {server.tags.slice(0, 2).map((tag) => <Badge key={tag} variant="subtle" colorPalette="gray">{tag}</Badge>)}
                      </HStack>
                    </Box>
                  )
                })}
              </Stack>
            )}
          </Stack>
        )}

        {step === 'configuration' && entry && (
          <Stack gap="5">
            <Box className="sw-software-configuration-summary">
              <Text fontWeight="semibold">
                {entry.label} · {selectedIds.length} {selectedIds.length === 1 ? 'Server' : 'Servers'}
              </Text>
              <Text color="fg.muted" fontSize="sm">
                {mode === 'reconfigure'
                  ? 'The complete configuration below replaces the recorded configuration on this Server.'
                  : mode === 'retry'
                    ? 'Retry the failed installation with the configuration below.'
                    : 'Swallow will create one Workflow for these targets.'}
              </Text>
            </Box>

            {hasRoles && (
              <Box as="section" role="group" aria-labelledby="software-roles-label">
                <Text id="software-roles-label" fontWeight="semibold" mb="2">
                  Roles <Text as="span" color="red.fg" aria-hidden>*</Text>
                </Text>
                <Stack gap="2">
                  {selectedIds.map((serverId) => {
                    const server = servers.find((candidate) => candidate.id === serverId)
                    return (
                      <HStack key={serverId} className="sw-software-role-row" justify="space-between" gap="3" wrap="wrap">
                        <Text>{server ? serverDisplayName(server) : serverId}</Text>
                        <HStack gap="3" aria-label={`Roles for ${server ? serverDisplayName(server) : serverId}`}>
                          {entry.roles.map((role) => (
                            <Checkbox
                              key={role}
                              checked={roleFor(serverId).includes(role)}
                              onCheckedChange={(checked) => toggleRole(serverId, role, checked)}
                            >
                              {role}
                            </Checkbox>
                          ))}
                        </HStack>
                      </HStack>
                    )
                  })}
                </Stack>
              </Box>
            )}

            <SoftwareSpecFields
              entry={entry}
              value={specValue}
              onChange={(field, value) => setSpec((current) => ({ ...current, [field]: value }))}
            />

            {entry.specFields.includes('enableApi') && (
              <Stack gap="2">
                <Checkbox checked={enableApi} onCheckedChange={setEnableApi}>
                  Enable the Docker Engine API (required for the Server&apos;s Containers tab)
                </Checkbox>
                <DockerApiRiskNotice context="option" />
              </Stack>
            )}
          </Stack>
        )}
      </Stack>
    </Modal>
  )
}

/** Kind-specific configuration fields shared by new installs, retry, and reconfigure. */
function SoftwareSpecFields({
  entry,
  value,
  onChange,
}: {
  entry: SoftwareCatalogEntry
  value: (field: string) => string
  onChange: (field: string, value: string) => void
}) {
  if (entry.kind === 'nfs') {
    return (
      <Stack gap="4">
        <Field.Root>
          <Field.Label>Export path (server)</Field.Label>
          <Input value={value('exportPath')} onChange={(event) => onChange('exportPath', event.target.value)} placeholder="/export/data" />
          <Field.HelperText>Required when a Server takes the server role.</Field.HelperText>
        </Field.Root>
        <Field.Root>
          <Field.Label>Export options (server)</Field.Label>
          <Input value={value('exportOptions')} onChange={(event) => onChange('exportOptions', event.target.value)} placeholder="rw,sync,no_subtree_check,root_squash" />
        </Field.Root>
        <Field.Root>
          <Field.Label>Source (client)</Field.Label>
          <Input value={value('source')} onChange={(event) => onChange('source', event.target.value)} placeholder="10.0.0.9:/export/data" />
          <Field.HelperText>Required when a Server takes the client role (host:/path).</Field.HelperText>
        </Field.Root>
        <Field.Root>
          <Field.Label>Mount path (client)</Field.Label>
          <Input value={value('mountPath')} onChange={(event) => onChange('mountPath', event.target.value)} placeholder="/shared" />
        </Field.Root>
        <Field.Root>
          <Field.Label>Mount options (client)</Field.Label>
          <Input value={value('mountOptions')} onChange={(event) => onChange('mountOptions', event.target.value)} placeholder="rw,_netdev" />
        </Field.Root>
      </Stack>
    )
  }
  if (entry.specFields.includes('version')) {
    return (
      <Field.Root>
        <Field.Label>Version</Field.Label>
        <Input value={value('version')} onChange={(event) => onChange('version', event.target.value)} placeholder="Leave blank for the latest available" />
      </Field.Root>
    )
  }
  return null
}
