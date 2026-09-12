import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Button, Card, Field, Heading, HStack, Input, SegmentGroup, Table, Text, Textarea } from '@chakra-ui/react'
import { RefreshCw } from 'lucide-react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type {
  DeploymentTemplate,
  DeploymentTargetIssue,
  DeploymentNetworkMode,
  NetworkInspectionResult,
  DeploymentUserDataMode,
} from '@/domain/provisioning/types'
import { serverDisplayName, type Server } from '@/domain/server/types'
import type { Integration, OSImage } from '@/domain/site/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { SectionHeader, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { Wizard, type WizardStepDef } from '@/presentation/components/Wizard'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import { Select } from '@/presentation/components/ui/select'
import { formatSubnetOptionLabel } from '@/presentation/utils/network'
import { PageHeader } from '@/presentation/components/PageHeader'
import { LockBadge, ProvisioningBadge, PowerBadge } from '@/presentation/components/AxisBadge'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useServerWorkingSet } from '@/presentation/pages/servers/useServerWorkingSet'
import { ProvisioningTabs } from './ProvisioningTabs'

const MAX_TARGETS = 100

/** Rewrites repeated serverId parameters while preserving unrelated URL filters. */
function updateTargetParams(params: URLSearchParams, serverIds: readonly string[], integrationId?: string): URLSearchParams {
  const next = new URLSearchParams(params)
  next.delete('serverId')
  serverIds.forEach((id) => next.append('serverId', id))
  if (integrationId) next.set('integrationId', integrationId)
  else next.delete('integrationId')
  return next
}

function serverIsReadyCandidate(server: Server): boolean {
  return !server.absent && server.provisioning?.state === 'ready'
}

function serverIsDeployable(server: Server): boolean {
  return serverIsReadyCandidate(server) && !server.provisioning?.locked
}
function validIPv4(value: string): boolean {
  const octets = value.trim().split('.')
  return octets.length === 4 && octets.every((octet) => /^(0|[1-9]\d{0,2})$/.test(octet) && Number(octet) <= 255)
}

function targetIssueName(issue: DeploymentTargetIssue, servers: Server[]): string {
  const server = servers.find((item) => item.id === issue.serverId)
  return server ? serverDisplayName(server) : issue.serverId
}

/** Keeps every wizard step on the same compact vertical rhythm. */
function WizardSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="sw-wizard-section">
      <Heading as="h2" size="md">
        {title}
      </Heading>
      {children}
    </section>
  )
}

/**
 * OS provisioning workflow for one to one hundred Servers.
 *
 * Targets remain deep-linkable in the URL, cloud-init remains memory-only, and the Targets step
 * performs provider-owned readiness inspection before configuration, while final deployment
 * repeats backend preflight to protect against changed provider state. The image selector reads
 * the chosen provisioner live on integration changes and explicit refresh, retaining no
 * browser-owned catalog.
 */
export function DeployOSWizardPage() {
  const { provisioning, sites: siteRepository } = useApp()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const { siteId, loading: siteScopeLoading, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const workingSet = useServerWorkingSet({ siteId, includeAbsent: true })
  const initialTargetIds = useMemo(() => searchParams.getAll('serverId'), [searchParams])
  const [selected, setSelected] = useState<ReadonlySet<string>>(() => new Set(initialTargetIds))
  const [integrationId, setIntegrationId] = useState(searchParams.get('integrationId') ?? '')
  const [integrations, setIntegrations] = useState<Integration[]>([])
  const [templates, setTemplates] = useState<DeploymentTemplate[]>([])
  const [images, setImages] = useState<OSImage[]>([])
  const [catalogError, setCatalogError] = useState('')
  const [catalogLoading, setCatalogLoading] = useState(false)
  const [catalogRefreshNonce, setCatalogRefreshNonce] = useState(0)
  const [resourcesLoading, setResourcesLoading] = useState(true)
  const [templateId, setTemplateId] = useState(searchParams.get('templateId') ?? '')
  const [customized, setCustomized] = useState(false)
  const [imageId, setImageId] = useState(searchParams.get('imageId') ?? '')
  const [ephemeral, setEphemeral] = useState(false)
  const [userDataMode, setUserDataMode] = useState<DeploymentUserDataMode>('omit')
  const [userData, setUserData] = useState('')
  const [saveTemplate, setSaveTemplate] = useState(false)
  const [templateName, setTemplateName] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [checkingTargets, setCheckingTargets] = useState(false)
  const [targetIssues, setTargetIssues] = useState<DeploymentTargetIssue[]>([])
  const [networkInspection, setNetworkInspection] = useState<NetworkInspectionResult | null>(null)
  const [networkMode, setNetworkMode] = useState<DeploymentNetworkMode>('automatic')
  const [networkSubnetId, setNetworkSubnetId] = useState('')
  const [defaultGateway, setDefaultGateway] = useState(false)
  const [networkAssignments, setNetworkAssignments] = useState<Record<string, { interfaceId: string; subnetId: string; ipAddress: string }>>({})
  const normalizedTargetKey = useRef('')
  const previousSite = useRef<{ initialized: boolean; value?: string }>({ initialized: false, value: siteId })

  // Site-scoped integrations and templates load together; cleanup prevents an older request from
  // overwriting state after a scope change or unmount.
  useEffect(() => {
    let cancelled = false
    setResourcesLoading(true)
    Promise.all([siteRepository.listIntegrations({ siteId, kind: 'provisioner' }), provisioning.listTemplates({ siteId })])
      .then(([nextIntegrations, nextTemplates]) => {
        if (cancelled) return
        setIntegrations(nextIntegrations)
        setTemplates(nextTemplates)
      })
      .catch((error: Error) => {
        if (!cancelled) {
          showToast({ tone: 'error', title: 'Provisioning resources unavailable', description: error.message })
        }
      })
      .finally(() => {
        if (!cancelled) setResourcesLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [provisioning, showToast, siteId, siteRepository])

  // The image catalog is provider-owned live data. A changed integration cancels presentation
  // updates from the previous provider request; the nonce repeats the same read only when the
  // operator explicitly requests current provider state.
  useEffect(() => {
    if (!integrationId) {
      setImages([])
      setCatalogError('')
      setCatalogLoading(false)
      return
    }
    let cancelled = false
    setCatalogError('')
    setCatalogLoading(true)
    provisioning
      .listOSImages(integrationId)
      .then((items) => {
        if (!cancelled) setImages(items)
      })
      .catch((error: Error) => {
        if (!cancelled) {
          setImages([])
          setCatalogError(error.message)
        }
      })
      .finally(() => {
        if (!cancelled) setCatalogLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [catalogRefreshNonce, integrationId, provisioning])

  useEffect(() => {
    if (siteScopeLoading) return
    if (!previousSite.current.initialized) {
      previousSite.current = { initialized: true, value: siteId }
      return
    }
    if (previousSite.current.value === siteId) return
    previousSite.current = { initialized: true, value: siteId }
    setSelected(new Set())
    setTargetIssues([])
    setNetworkInspection(null)
    setNetworkMode('automatic')
    setNetworkSubnetId('')
    setDefaultGateway(false)
    setNetworkAssignments({})
    setIntegrationId('')
    setTemplateId('')
    setCustomized(false)
    setImageId('')
    setEphemeral(false)
    setUserDataMode('omit')
    setUserData('')
    setSaveTemplate(false)
    setTemplateName('')
    const next = new URLSearchParams(searchParams)
    ;['serverId', 'integrationId', 'templateId', 'imageId'].forEach((key) => next.delete(key))
    setSearchParams(next, { replace: true })
    showToast({ tone: 'warning', title: 'Provisioning draft cleared', description: 'Targets and configuration were cleared because the Site scope changed.' })
  }, [searchParams, setSearchParams, showToast, siteId, siteScopeLoading])

  const servers = useMemo(() => (workingSet.state.status === 'ready' ? workingSet.state.data.servers : []), [workingSet.state])
  const selectedServers = useMemo(() => servers.filter((server) => selected.has(server.id)), [selected, servers])

  useEffect(() => {
    const lockedTargets = initialTargetIds.map((id) => servers.find((server) => server.id === id)).filter((server) => server?.provisioning?.locked)

    if (workingSet.state.status !== 'ready' || initialTargetIds.length === 0) return
    const compatible = initialTargetIds
      .map((id) => servers.find((server) => server.id === id))
      .filter((server): server is Server => Boolean(server && serverIsDeployable(server)))
    const derivedIntegration = compatible[0]?.source.integrationId ?? ''
    const sameIntegration = compatible.filter((server) => server.source.integrationId === derivedIntegration)
    if (sameIntegration.length !== initialTargetIds.length) {
      const targetKey = initialTargetIds.join(',')
      if (normalizedTargetKey.current === targetKey) return
      normalizedTargetKey.current = targetKey
      setSelected(new Set(sameIntegration.map((server) => server.id)))
      setTargetIssues([])
      setIntegrationId(derivedIntegration)
      setSearchParams(updateTargetParams(searchParams, sameIntegration.map((server) => server.id), derivedIntegration), { replace: true })
      showToast({
        tone: 'warning',
        title: lockedTargets.length > 0 ? 'Locked targets removed' : 'Incompatible targets removed',
        description:
          lockedTargets.length > 0 ? 'Unlock the Server before deployment.' : 'Only ready, present Servers from one provisioner can be deployed together.',
      })
    } else if (!integrationId && derivedIntegration) {
      normalizedTargetKey.current = ''
      setIntegrationId(derivedIntegration)
      setSearchParams(updateTargetParams(searchParams, initialTargetIds, derivedIntegration), { replace: true })
    }
  }, [initialTargetIds, integrationId, searchParams, servers, setSearchParams, showToast, siteId, workingSet.state.status])

  useEffect(() => {
    if (!templateId || templates.length === 0) return
    const template = templates.find((item) => item.id === templateId)
    if (!template) {
      setTemplateId('')
      showToast({ tone: 'warning', title: 'Deployment template not found' })
      return
    }
    setIntegrationId(template.integrationId)
    setImageId(template.imageId)
    setEphemeral(template.ephemeral)
    setNetworkMode(template.network?.mode ?? 'automatic')
    setNetworkSubnetId(template.network?.subnetId ?? '')
    setDefaultGateway(template.network?.defaultGateway ?? false)
    setUserDataMode('inherit')
    setCustomized(false)
  }, [showToast, templateId, templates])

  const availableServers = useMemo(
    () => servers.filter((server) => serverIsReadyCandidate(server) && (!integrationId || server.source.integrationId === integrationId)),
    [integrationId, servers],
  )
  const selectedTemplate = templates.find((item) => item.id === templateId)
  const effectiveImageId = selectedTemplate && !customized ? selectedTemplate.imageId : imageId
  const effectiveEphemeral = selectedTemplate && !customized ? selectedTemplate.ephemeral : ephemeral
  const effectiveNetworkMode = selectedTemplate && !customized ? selectedTemplate.network?.mode ?? 'automatic' : networkMode
  const effectiveNetworkSubnetId = selectedTemplate && !customized ? selectedTemplate.network?.subnetId ?? '' : networkSubnetId
  const effectiveDefaultGateway = selectedTemplate && !customized ? selectedTemplate.network?.defaultGateway ?? false : defaultGateway
  const assignedStaticIPs = selectedServers.map((server) => networkAssignments[server.id]?.ipAddress.trim() ?? '').filter(Boolean)
  const networkAssignmentsValid =
    Boolean(networkInspection) &&
    selectedServers.every((server) => {
      const assignment = networkAssignments[server.id]
      const target = networkInspection?.targets.find((item) => item.serverId === server.id)
      const iface = target?.network.interfaces.find((item) => item.id === assignment?.interfaceId)
      if (!assignment?.interfaceId || !assignment.subnetId || !iface?.availableSubnets.some((subnet) => subnet.id === assignment.subnetId)) return false
      return effectiveNetworkMode !== 'static' || validIPv4(assignment.ipAddress)
    }) &&
    (effectiveNetworkMode !== 'static' || new Set(assignedStaticIPs).size === assignedStaticIPs.length)
  const assignedSubnetIds = [...new Set(selectedServers.map((server) => networkAssignments[server.id]?.subnetId).filter((value): value is string => Boolean(value)))]
  const reusableSubnetId = effectiveNetworkSubnetId || (assignedSubnetIds.length === 1 ? assignedSubnetIds[0] : '')
  const reusableNetworkValid = effectiveNetworkMode === 'automatic' || Boolean(reusableSubnetId)
  const targetsValid =
    selected.size > 0 &&
    selected.size <= MAX_TARGETS &&
    selectedServers.length === selected.size &&
    selectedServers.every((server) => serverIsDeployable(server) && server.source.integrationId === integrationId)
  const configurationValid = Boolean(
    integrationId && effectiveImageId && !catalogError && !catalogLoading && (userDataMode !== 'replace' || userData) && networkAssignmentsValid,
  )
  const inheritedSecretCannotBeSaved = Boolean(saveTemplate && selectedTemplate?.hasUserData && customized && userDataMode === 'inherit')
  const reviewValid =
    targetsValid &&
    configurationValid &&
    (!saveTemplate || Boolean(templateName.trim())) &&
    (!saveTemplate || reusableNetworkValid) &&
    !inheritedSecretCannotBeSaved

  const checkTargets = async (onNext: () => void) => {
    if (!targetsValid || checkingTargets) return
    setCheckingTargets(true)
    setTargetIssues([])
    try {
      const [preflight, inspection] = await Promise.all([
        provisioning.preflightDeploymentTargets([...selected]),
        provisioning.inspectDeploymentNetworks([...selected]),
      ])
      if (!preflight.valid) {
        setTargetIssues(preflight.issues)
        showToast({
          tone: 'warning',
          title: 'Deployment targets need attention',
          description: `${preflight.issues.length} Server${preflight.issues.length === 1 ? '' : 's'} cannot be deployed yet.`,
        })
        return
      }
      const usesLockedTemplateNetwork = Boolean(selectedTemplate && !customized)
      const appliesInspectionDefaults = networkInspection === null && !usesLockedTemplateNetwork
      const suggestedMode: DeploymentNetworkMode = inspection.targets.some((target) => target.suggestion.mode === 'static') ? 'static' : 'automatic'
      if (appliesInspectionDefaults) {
        setNetworkMode(suggestedMode)
        setDefaultGateway(suggestedMode === 'static' && inspection.targets.some((target) => target.suggestion.mode === 'static' && target.suggestion.defaultGateway))
      }
      setNetworkInspection(inspection)
      const nextAssignments: Record<string, { interfaceId: string; subnetId: string; ipAddress: string }> = {}
      for (const target of inspection.targets) {
        const previous = networkAssignments[target.serverId]
        nextAssignments[target.serverId] = {
          interfaceId: target.suggestion.interfaceId,
          subnetId: effectiveNetworkSubnetId || target.suggestion.subnetId,
          ipAddress: previous?.ipAddress ?? (target.suggestion.mode === 'static' ? target.suggestion.ipAddress : ''),
        }
      }
      setNetworkAssignments(nextAssignments)
      onNext()
    } catch (error) {
      showToast({ tone: 'error', title: 'Could not check deployment readiness', description: error instanceof Error ? error.message : 'Unknown error' })
    } finally {
      setCheckingTargets(false)
    }
  }

  const selectIntegration = (nextIntegrationId: string) => {
    setIntegrationId(nextIntegrationId)
    setImages([])
    setCatalogError('')
    setNetworkInspection(null)
    setNetworkAssignments({})
    setSelected(new Set())
    setNetworkMode('automatic')
    setNetworkSubnetId('')
    setDefaultGateway(false)
    setTemplateId('')
    setTargetIssues([])
    setCustomized(false)
    setImageId('')
    setEphemeral(false)
    setUserDataMode('omit')
    setUserData('')
    setSearchParams(updateTargetParams(searchParams, [], nextIntegrationId), { replace: true })
  }

  const toggleServer = (serverId: string) => {
    setTargetIssues([])
    const server = servers.find((item) => item.id === serverId)
    if (!server || !serverIsDeployable(server)) {
      showToast({ tone: 'warning', title: 'Server cannot be selected', description: 'Unlock the Server before deployment.' })
      return
    }
    setNetworkInspection(null)
    setNetworkAssignments({})
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(serverId)) next.delete(serverId)
      else if (next.size < MAX_TARGETS) next.add(serverId)
      setSearchParams(updateTargetParams(searchParams, [...next], integrationId), { replace: true })
      return next
    })
  }

  const selectTemplate = (id: string) => {
    setTemplateId(id)
    setCustomized(false)
    setUserData('')
    if (!id) {
      setImageId('')
      setEphemeral(false)
      setNetworkMode('automatic')
      setNetworkSubnetId('')
      setDefaultGateway(false)
      setUserDataMode('omit')
      const next = new URLSearchParams(searchParams)
      next.delete('templateId')
      setSearchParams(next, { replace: true })
      return
    }
    const template = templates.find((item) => item.id === id)
    if (!template) return
    setIntegrationId(template.integrationId)
    setImageId(template.imageId)
    setEphemeral(template.ephemeral)
    setNetworkMode(template.network?.mode ?? 'automatic')
    setNetworkSubnetId(template.network?.subnetId ?? '')
    setDefaultGateway(template.network?.defaultGateway ?? false)
    setUserDataMode('inherit')
    const next = new URLSearchParams(searchParams)
    next.set('templateId', id)
    next.set('integrationId', template.integrationId)
    setSearchParams(next, { replace: true })
  }

  const deploy = async () => {
    if (!reviewValid || submitting) return
    setSubmitting(true)
    try {
      if (saveTemplate) {
        await provisioning.createTemplate({
          integrationId,
          name: templateName.trim(),
          imageId: effectiveImageId,
          ephemeral: effectiveEphemeral,
          network: { mode: effectiveNetworkMode, subnetId: reusableSubnetId || undefined, defaultGateway: effectiveDefaultGateway },
          userData: userDataMode === 'replace' ? userData : undefined,
        })
        showToast({ tone: 'success', title: 'Deployment template saved' })
      }
      const response = await provisioning.createDeploymentOperation({
        serverIds: [...selected],
        templateId: selectedTemplate?.id,
        settings: !selectedTemplate || customized ? { imageId: effectiveImageId, ephemeral: effectiveEphemeral } : undefined,
        userData: {
          mode: selectedTemplate ? userDataMode : userDataMode === 'replace' ? 'replace' : 'omit',
          value: userDataMode === 'replace' ? userData : undefined,
        },
        network: {
          mode: effectiveNetworkMode,
          subnetId: effectiveNetworkSubnetId || undefined,
          defaultGateway: effectiveDefaultGateway,
          assignments: selectedServers.map((server) => ({
            serverId: server.id,
            interfaceId: networkAssignments[server.id].interfaceId,
            subnetId: networkAssignments[server.id].subnetId,
            ipAddress: effectiveNetworkMode === 'static' ? networkAssignments[server.id].ipAddress.trim() : undefined,
          })),
        },
      })
      setUserData('')
      showToast({
        tone: 'success',
        title: 'OS deployment started',
        description: `Operation ${response.operationId} is running in the background. Server status will update automatically.`,
        duration: 12_000,
      })
      navigate(scopedHref('/servers'), { replace: true })
    } catch (error) {
      showToast({ tone: 'error', title: 'Deployment preflight failed', description: error instanceof Error ? error.message : 'Unknown error' })
    } finally {
      setSubmitting(false)
    }
  }

  const steps: WizardStepDef[] = [
    {
      id: 'os-targets',
      name: 'Targets',
      canProceed: targetsValid && !checkingTargets,
      onNext: (proceed) => void checkTargets(proceed),
      nextLabel: targetIssues.length > 0 ? 'Check again' : undefined,
      nextLoading: checkingTargets,
      content: (
        <WizardSection title="Deployment targets">
          <Field.Root required>
            <Field.Label>Provisioner integration</Field.Label>
            <Select
              id="deploy-integration"
              aria-label="Provisioner integration"
              value={integrationId}
              placeholder="Select an integration"
              options={integrations.map((integration) => ({ value: integration.id, label: integration.name }))}
              required
              onChange={selectIntegration}
            />
          </Field.Root>
          <div className="sw-target-summary">
            <strong>
              {selected.size} of {MAX_TARGETS} selected
            </strong>
            {selected.size > 0 && (
              <Button
                variant="plain"
                size="sm"
                onClick={() => {
                  setTargetIssues([])
                  setSelected(new Set())
                  setSearchParams(updateTargetParams(searchParams, [], integrationId), { replace: true })
                }}
              >
                Clear
              </Button>
            )}
          </div>
          {targetIssues.length > 0 && (
            <div className="sw-target-issues" aria-live="polite">
              {targetIssues.map((issue) => (
                <Alert
                  key={`${issue.serverId}:${issue.code}`}
                  status="warning"
                  title={`${targetIssueName(issue, servers)}: Deployment blocked`}
                  actions={
                    <Button variant="plain" size="sm" alignSelf="center" onClick={() => navigate(scopedHref(`/servers/${issue.serverId}/network`))}>
                      Review Server Network
                    </Button>
                  }
                >
                  {issue.message}
                </Alert>
              ))}
            </div>
          )}
          {!integrationId && <EmptyState title="Select a provisioner" message="Targets in a batch must come from the same integration." />}
          {integrationId && availableServers.length === 0 && (
            <EmptyState title="No ready Servers" message="This provisioner has no present Server in the ready state." />
          )}
          {integrationId && availableServers.length > 0 && (
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Deployment targets">
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeader />
                    <Table.ColumnHeader>Server</Table.ColumnHeader>
                    <Table.ColumnHeader>Address</Table.ColumnHeader>
                    <Table.ColumnHeader>Power</Table.ColumnHeader>
                    <Table.ColumnHeader>State</Table.ColumnHeader>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {availableServers.map((server) => (
                    <Table.Row key={server.id}>
                      <Table.Cell className="sw-cell-center">
                        <Checkbox
                          id={`deploy-target-${server.id}`}
                          aria-label={`Select ${serverDisplayName(server)}`}
                          checked={selected.has(server.id)}
                          disabled={server.provisioning?.locked ?? false}
                          onCheckedChange={() => toggleServer(server.id)}
                        />
                      </Table.Cell>
                      <Table.Cell>
                        <HStack gap="2">
                          <strong>{serverDisplayName(server)}</strong>
                          <LockBadge locked={server.provisioning?.locked ?? false} />
                        </HStack>
                      </Table.Cell>
                      <Table.Cell className="sw-mono">{server.addresses[0] ?? '-'}</Table.Cell>
                      <Table.Cell>
                        <PowerBadge powerState={server.provisioning?.powerState ?? null} />
                      </Table.Cell>
                      <Table.Cell>
                        <ProvisioningBadge axis={server.provisioning} />
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          )}
        </WizardSection>
      ),
    },
    {
      id: 'os-configuration',
      name: 'Configuration',
      canProceed: configurationValid,
      content: (
        <WizardSection title="Operating system configuration">
          <div className="sw-form-grid">
            <Field.Root required>
              <Field.Label>Configuration source</Field.Label>
              <Select
                value={templateId}
                aria-label="Configuration source"
                onChange={(value) => selectTemplate(value)}
                options={[
                  { value: '', label: 'Custom configuration' },
                  ...templates
                    .filter((template) => template.integrationId === integrationId)
                    .map((template) => ({ value: template.id, label: template.name })),
                ]}
              />
            </Field.Root>
            {selectedTemplate && (
              <Field.Root>
                <Field.Label>Template</Field.Label>
                <Button variant={customized ? 'outline' : 'plain'} size="sm" alignSelf="flex-start" onClick={() => setCustomized((value) => !value)}>
                  {customized ? 'Use template defaults' : 'Customize'}
                </Button>
              </Field.Root>
            )}
            <Field.Root required>
              <Field.Label>
                <HStack justify="space-between" w="full">
                  <span>OS image</span>
                  <Button
                    variant="plain"
                    size="xs"
                    loading={catalogLoading}
                    disabled={!integrationId || catalogLoading}
                    onClick={() => setCatalogRefreshNonce((value) => value + 1)}
                  >
                    <RefreshCw size={14} />
                    Refresh
                  </Button>
                </HStack>
              </Field.Label>
              <Select
                id="deploy-image"
                aria-label="OS image"
                value={effectiveImageId}
                placeholder={catalogLoading ? 'Loading images...' : images.length === 0 ? 'No deployable images available' : 'Select an image'}
                options={[
                  ...(effectiveImageId && !images.some((image) => image.id === effectiveImageId) ? [{ value: effectiveImageId, label: effectiveImageId }] : []),
                  ...images.map((image) => ({ value: image.id, label: `${image.name} (${image.architecture})` })),
                ]}
                disabled={Boolean(selectedTemplate && !customized) || Boolean(catalogError) || catalogLoading}
                required
                onChange={setImageId}
              />
              {!catalogLoading && !catalogError && (
                <Text as="small" className="sw-field-note" aria-live="polite">
                  {images.length} deployable image{images.length === 1 ? '' : 's'} returned by the provider.
                </Text>
              )}
            </Field.Root>
            <Field.Root>
              <Checkbox id="deploy-ephemeral" checked={effectiveEphemeral} disabled={Boolean(selectedTemplate && !customized)} onCheckedChange={(checked) => setEphemeral(checked)}>
                Ephemeral deployment
              </Checkbox>
            </Field.Root>
            <Field.Root required>
              <Field.Label>Cloud-init</Field.Label>
              <Select
                value={userDataMode}
                aria-label="Cloud-init mode"
                onChange={(value) => {
                  setUserDataMode(value as DeploymentUserDataMode)
                  if (value !== 'replace') setUserData('')
                }}
                options={[
                  ...(selectedTemplate
                    ? [{ value: 'inherit', label: selectedTemplate.hasUserData ? 'Inherit template cloud-init' : 'Inherit (template has none)' }]
                    : []),
                  { value: 'replace', label: 'Replace for this deployment' },
                  { value: 'omit', label: 'Omit cloud-init' },
                ]}
              />
            </Field.Root>
            {userDataMode === 'replace' && (
              <Field.Root required>
                <Field.Label>Cloud-init</Field.Label>
                <Textarea id="deploy-user-data" value={userData} onChange={(event) => setUserData(event.target.value)} rows={10} autoComplete="off" />
              </Field.Root>
            )}
          </div>
          <section className="sw-section">
            <SectionHeader
              title="Network configuration"
              description="Choose Automatic or Static addressing; existing provider settings are not reused."
            />
            <div className="sw-section-body">
              <div className="sw-form-grid">
                <Field.Root required>
                  <Field.Label>Addressing mode</Field.Label>
                  <SegmentGroup.Root
                    value={effectiveNetworkMode}
                    disabled={Boolean(selectedTemplate && !customized)}
                    onValueChange={(details) => {
                      if (!details.value) return
                      if (details.value === 'automatic') {
                        setNetworkMode('automatic')
                        setDefaultGateway(false)
                      } else {
                        setNetworkMode('static')
                      }
                    }}
                  >
                    <SegmentGroup.Indicator />
                    <SegmentGroup.Item value="automatic">
                      <SegmentGroup.ItemText>Automatic</SegmentGroup.ItemText>
                      <SegmentGroup.ItemHiddenInput />
                    </SegmentGroup.Item>
                    <SegmentGroup.Item value="static">
                      <SegmentGroup.ItemText>Static</SegmentGroup.ItemText>
                      <SegmentGroup.ItemHiddenInput />
                    </SegmentGroup.Item>
                  </SegmentGroup.Root>
                </Field.Root>
                {effectiveNetworkMode === 'static' && (
                  <Field.Root>
                    <Checkbox
                      id="deploy-network-default-gateway"
                      checked={effectiveDefaultGateway}
                      disabled={Boolean(selectedTemplate && !customized)}
                      onCheckedChange={(checked) => setDefaultGateway(checked)}
                    >
                      Use the selected subnet for the default route
                    </Checkbox>
                    <Field.HelperText>
                      For each target, the provider makes this Static link the IPv4 default route using the gateway address configured on its selected subnet.
                    </Field.HelperText>
                  </Field.Root>
                )}
              </div>
              {!networkInspection && (
                <Alert status="warning" title="Network inspection is required">
                  Return to Targets and run the readiness check again.
                </Alert>
              )}
              {networkInspection && !networkAssignmentsValid && (
                <Alert status="warning" title="Complete every network assignment">
                  Select a NIC and subnet for each Server. Static mode also requires a unique IPv4 address per target.
                </Alert>
              )}
            </div>
            {networkInspection && (
              <StickyTableFrame>
                <Table.Root size="sm" aria-label="Deployment network assignments" className="sw-network-assignment-table">
                  <Table.Header>
                    <Table.Row>
                      <Table.ColumnHeader className="sw-network-server-column">Server</Table.ColumnHeader>
                      <Table.ColumnHeader className="sw-network-interface-column">Interface</Table.ColumnHeader>
                      <Table.ColumnHeader className="sw-network-subnet-column">Subnet</Table.ColumnHeader>
                      {effectiveNetworkMode === 'static' && <Table.ColumnHeader className="sw-network-address-column">Static IPv4 address</Table.ColumnHeader>}
                      <Table.ColumnHeader className="sw-network-current-mode-column">
                        <span title="Current provider mode">Current mode</span>
                      </Table.ColumnHeader>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {networkInspection.targets.map((target) => {
                      const server = selectedServers.find((item) => item.id === target.serverId)
                      const assignment = networkAssignments[target.serverId] ?? { interfaceId: '', subnetId: '', ipAddress: '' }
                      const iface = target.network.interfaces.find((item) => item.id === assignment.interfaceId)
                      const currentProviderMode = iface?.rawProviderMode === 'AUTO' ? 'Provider-managed (MAAS AUTO)' : iface?.rawProviderMode || '-'
                      return (
                        <Table.Row key={target.serverId}>
                          <Table.Cell className="sw-network-server-column">
                            <strong>{server ? serverDisplayName(server) : target.serverId}</strong>
                          </Table.Cell>
                          <Table.Cell className="sw-network-interface-column">
                            <Select
                              aria-label={`Interface for ${server ? serverDisplayName(server) : target.serverId}`}
                              value={assignment.interfaceId}
                              size="sm"
                              placeholder="Select an interface"
                              onChange={(value) => {
                                const nextInterface = target.network.interfaces.find((item) => item.id === value)
                                const compatible = nextInterface?.availableSubnets.some((subnet) => subnet.id === assignment.subnetId)
                                const suggestedSubnet = compatible
                                  ? assignment.subnetId
                                  : nextInterface?.availableSubnets.length === 1
                                    ? nextInterface.availableSubnets[0].id
                                    : ''
                                setNetworkAssignments((current) => ({ ...current, [target.serverId]: { ...assignment, interfaceId: value, subnetId: suggestedSubnet } }))
                              }}
                              options={target.network.interfaces.map((item) => ({
                                value: item.id,
                                label: `${item.name} - ${item.macAddress}${item.boot ? ' (boot NIC)' : ''}`,
                              }))}
                            />
                          </Table.Cell>
                          <Table.Cell className="sw-network-subnet-column">
                            <Select
                              aria-label={`Subnet for ${server ? serverDisplayName(server) : target.serverId}`}
                              value={assignment.subnetId}
                              size="sm"
                              placeholder="Select a subnet"
                              onChange={(value) => setNetworkAssignments((current) => ({ ...current, [target.serverId]: { ...assignment, subnetId: value } }))}
                              options={(iface?.availableSubnets ?? []).map((subnet) => ({ value: subnet.id, label: formatSubnetOptionLabel(subnet) }))}
                            />
                          </Table.Cell>
                          {effectiveNetworkMode === 'static' && (
                            <Table.Cell className="sw-network-address-column">
                              <Input
                                aria-label={`Static IPv4 address for ${server ? serverDisplayName(server) : target.serverId}`}
                                value={assignment.ipAddress}
                                onChange={(event) => setNetworkAssignments((current) => ({ ...current, [target.serverId]: { ...assignment, ipAddress: event.target.value } }))}
                                placeholder="192.0.2.10"
                              />
                            </Table.Cell>
                          )}
                          <Table.Cell className="sw-network-current-mode-column" title={currentProviderMode}>
                            {currentProviderMode}
                          </Table.Cell>
                        </Table.Row>
                      )
                    })}
                  </Table.Body>
                </Table.Root>
              </StickyTableFrame>
            )}
          </section>
          {catalogError && (
            <Alert status="warning" title="Image catalog unavailable">
              {catalogError}
            </Alert>
          )}
          {selectedTemplate && !customized && (
            <Alert status="info" title="Template settings are locked">
              Choose Customize to override the image or ephemeral setting.
            </Alert>
          )}
        </WizardSection>
      ),
    },
    {
      id: 'os-review',
      name: 'Review',
      canProceed: reviewValid && !submitting,
      content: (
        <WizardSection title="Review deployment">
          <Card.Root size="sm">
            <Card.Body gap="3">
              <Heading size="sm">
                {selected.size} Server{selected.size === 1 ? '' : 's'}
              </Heading>
              <DescriptionList
                emptyText="-"
                items={[
                  { label: 'Integration', value: integrations.find((item) => item.id === integrationId)?.name ?? integrationId },
                  { label: 'Configuration', value: selectedTemplate ? `${selectedTemplate.name}${customized ? ' (customized)' : ''}` : 'Custom' },
                  { label: 'Image', value: effectiveImageId },
                  { label: 'Ephemeral', value: effectiveEphemeral ? 'Yes' : 'No' },
                  {
                    label: 'Cloud-init',
                    value: userDataMode === 'inherit' ? 'Inherit from template' : userDataMode === 'replace' ? 'Replace for this deployment' : 'Omit',
                  },
                  { label: 'Network mode', value: effectiveNetworkMode === 'automatic' ? 'Automatic' : 'Static' },
                  { label: 'Default gateway', value: effectiveDefaultGateway ? 'Selected links' : 'Provider routing' },
                ]}
              />
            </Card.Body>
          </Card.Root>
          <StickyTableFrame>
            <Table.Root size="sm" aria-label="Deployment review targets">
              <Table.Header>
                <Table.Row>
                  <Table.ColumnHeader>Server</Table.ColumnHeader>
                  <Table.ColumnHeader>Provider machine ID</Table.ColumnHeader>
                  <Table.ColumnHeader>Interface</Table.ColumnHeader>
                  <Table.ColumnHeader>Subnet</Table.ColumnHeader>
                  <Table.ColumnHeader>IP address</Table.ColumnHeader>
                  <Table.ColumnHeader>Power</Table.ColumnHeader>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {selectedServers.map((server) => (
                  <Table.Row key={server.id}>
                    <Table.Cell>{serverDisplayName(server)}</Table.Cell>
                    <Table.Cell className="sw-mono">{server.source.providerMachineId}</Table.Cell>
                    <Table.Cell>
                      {networkInspection?.targets
                        .find((target) => target.serverId === server.id)
                        ?.network.interfaces.find((iface) => iface.id === networkAssignments[server.id]?.interfaceId)?.name || '-'}
                    </Table.Cell>
                    <Table.Cell>
                      {networkInspection?.targets
                        .find((target) => target.serverId === server.id)
                        ?.network.interfaces.flatMap((iface) => iface.availableSubnets)
                        .find((subnet) => subnet.id === networkAssignments[server.id]?.subnetId)?.cidr || '-'}
                    </Table.Cell>
                    <Table.Cell className="sw-mono">
                      {effectiveNetworkMode === 'static' ? networkAssignments[server.id]?.ipAddress || '-' : 'Automatic'}
                    </Table.Cell>
                    <Table.Cell>
                      <PowerBadge powerState={server.provisioning?.powerState ?? null} />
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table.Root>
          </StickyTableFrame>
          {(!selectedTemplate || customized) && (
            <Card.Root size="sm">
              <Card.Body gap="4" className="sw-template-editor">
                <Heading size="sm">Reuse this configuration</Heading>
                <Checkbox id="save-deployment-template" checked={saveTemplate} onCheckedChange={(checked) => setSaveTemplate(checked)}>
                  Save as a deployment template
                </Checkbox>
                {saveTemplate && (
                  <Input aria-label="New deployment template name" value={templateName} onChange={(event) => setTemplateName(event.target.value)} placeholder="Template name" />
                )}
                {inheritedSecretCannotBeSaved && (
                  <Alert status="warning" title="Inherited cloud-init cannot be copied">
                    Select Replace or Omit before saving a customized template.
                  </Alert>
                )}
                {saveTemplate && !reusableNetworkValid && (
                  <Alert status="warning" title="Static templates require one shared subnet">
                    Choose the same subnet for every target before saving this configuration as a template.
                  </Alert>
                )}
              </Card.Body>
            </Card.Root>
          )}
          <Alert status="warning" title="Deployment starts immediately after preflight">
            Accepted provider requests cannot be rolled back as a batch.
          </Alert>
        </WizardSection>
      ),
    },
  ]

  return (
    <div className="operator-page">
      <PageHeader title="Deploy OS" breadcrumbs={[{ label: 'Provisioning' }, { label: 'Deploy OS' }]} />
      <ProvisioningTabs />
      {(workingSet.state.status === 'loading' || resourcesLoading) && <LoadingState rows={7} />}
      {workingSet.state.status === 'error' && <ErrorState message={workingSet.state.message} onRetry={workingSet.reload} />}
      {workingSet.state.status === 'ready' && !resourcesLoading && (
        <Wizard steps={steps} onFinish={() => void deploy()} finishLabel="Deploy OS" finishing={submitting} />
      )}
    </div>
  )
}
