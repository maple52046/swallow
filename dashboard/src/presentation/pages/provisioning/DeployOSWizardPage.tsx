import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import {
  Alert,
  AlertVariant,
  Button,
  Card,
  CardBody,
  CardTitle,
  Checkbox,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  Form,
  FormGroup,
  FormSelect,
  FormSelectOption,
  Label,
  TextArea,
  TextInput,
  ToggleGroup,
  ToggleGroupItem,
  Title,
  Wizard,
  WizardFooter,
  WizardStep,
} from '@patternfly/react-core'
import { SyncAltIcon } from '@patternfly/react-icons'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type {
  DeploymentTemplate,
  DeploymentTargetIssue,
  DeploymentNetworkMode,
  NetworkInspectionResult,
  DeploymentUserDataMode,
  DeployServersResult,
  StoredDeploymentResult,
} from '@/domain/provisioning/types'
import { serverDisplayName, type Server } from '@/domain/server/types'
import type { Integration, OSImage } from '@/domain/site/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { SingleSelect } from '@/presentation/components/SingleSelect'
import { SectionHeader, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { formatSubnetOptionLabel } from '@/presentation/utils/network'
import { PageHeader } from '@/presentation/components/PageHeader'
import { ProvisioningBadge } from '@/presentation/components/AxisBadge'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useServerWorkingSet } from '@/presentation/pages/servers/useServerWorkingSet'
import { ProvisioningTabs } from './ProvisioningTabs'

const RESULT_STORAGE_KEY = 'swallow.provisioning.last-result'
const MAX_TARGETS = 100

/**
 * Restores only an acceptance report for the exact URL-owned target set.
 * Cloud-init is absent from StoredDeploymentResult and is never read from browser storage.
 */
function readStoredResult(serverIds: string[]): StoredDeploymentResult | null {
  if (serverIds.length === 0) return null
  try {
    const raw = sessionStorage.getItem(RESULT_STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as StoredDeploymentResult
    if (!Array.isArray(parsed.serverIds)) return null
    const expected = [...serverIds].sort().join(',')
    return [...parsed.serverIds].sort().join(',') === expected ? parsed : null
  } catch {
    return null
  }
}

/**
 * Persists non-secret dispatch outcomes for reload recovery in this browser tab.
 * A storage failure is non-fatal because the active render retains the same result.
 */
function storeResult(result: StoredDeploymentResult): void {
  try {
    sessionStorage.setItem(RESULT_STORAGE_KEY, JSON.stringify(result))
  } catch {
    // Results remain visible for the active render when session storage is unavailable.
  }
}

/** Rewrites repeated serverId parameters while preserving unrelated URL filters. */
function updateTargetParams(
  params: URLSearchParams,
  serverIds: readonly string[],
  integrationId?: string,
): URLSearchParams {
  const next = new URLSearchParams(params)
  next.delete('serverId')
  serverIds.forEach((id) => next.append('serverId', id))
  if (integrationId) next.set('integrationId', integrationId)
  else next.delete('integrationId')
  return next
}

function serverIsDeployable(server: Server): boolean {
  return !server.absent && server.provisioning?.state === 'ready'
}
function validIPv4(value: string): boolean {
  const octets = value.trim().split('.')
  return octets.length === 4 && octets.every((octet) => /^(0|[1-9]\d{0,2})$/.test(octet) && Number(octet) <= 255)
}


function targetIssueName(issue: DeploymentTargetIssue, servers: Server[]): string {
  const server = servers.find((item) => item.id === issue.serverId)
  return server ? serverDisplayName(server) : issue.serverId
}

/**
 * PatternFly OS provisioning workflow for one to one hundred Servers.
 *
 * Targets remain deep-linkable in the URL, cloud-init remains memory-only, and the
 * Results step stores only non-secret acceptance data in sessionStorage. The Targets
 * step performs provider-owned readiness inspection before configuration, while final
 * deployment repeats backend preflight to protect against changed provider state.
 * The image selector reads the chosen provisioner live on integration changes and
 * explicit refresh, retaining no browser-owned catalog.
 */
export function DeployOSWizardPage() {
  const { provisioning, sites: siteRepository, servers: serverRepository } = useApp()
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
  const [networkMode, setNetworkMode] = useState<DeploymentNetworkMode>('dhcp')
  const [networkSubnetId, setNetworkSubnetId] = useState('')
  const [defaultGateway, setDefaultGateway] = useState(false)
  const [networkAssignments, setNetworkAssignments] = useState<Record<string, { interfaceId: string; subnetId: string; ipAddress: string }>>({})
  const [result, setResult] = useState<StoredDeploymentResult | null>(() => readStoredResult(initialTargetIds))
  const [liveServers, setLiveServers] = useState<Record<string, Server>>({})
  const previousSite = useRef<{ initialized: boolean; value?: string }>({
    initialized: false,
    value: siteId,
  })

  // Site-scoped integrations and templates load together; cleanup prevents an older
  // request from overwriting state after a scope change or unmount.
  useEffect(() => {
    let cancelled = false
    setResourcesLoading(true)
    Promise.all([
      siteRepository.listIntegrations({ siteId, kind: 'provisioner' }),
      provisioning.listTemplates({ siteId }),
    ]).then(([nextIntegrations, nextTemplates]) => {
      if (cancelled) return
      setIntegrations(nextIntegrations)
      setTemplates(nextTemplates)
    }).catch((error: Error) => {
      if (!cancelled) {
        showToast({ tone: 'error', title: 'Provisioning resources unavailable', description: error.message })
      }
    }).finally(() => {
      if (!cancelled) setResourcesLoading(false)
    })
    return () => {
      cancelled = true
    }
  }, [provisioning, showToast, siteId, siteRepository])

  // The image catalog is provider-owned live data. A changed integration cancels
  // presentation updates from the previous provider request; the nonce repeats the
  // same read only when the operator explicitly requests current provider state.
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
    provisioning.listOSImages(integrationId)
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
    setNetworkMode('dhcp')
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
    setResult(null)
    try {
      sessionStorage.removeItem(RESULT_STORAGE_KEY)
    } catch {
      // The active workflow still resets without browser storage.
    }
    const next = new URLSearchParams(searchParams)
    ;['serverId', 'integrationId', 'templateId', 'imageId'].forEach((key) => next.delete(key))
    setSearchParams(next, { replace: true })
    showToast({
      tone: 'warning',
      title: 'Provisioning draft cleared',
      description: 'Targets and configuration were cleared because the Site scope changed.',
    })
  }, [searchParams, setSearchParams, showToast, siteId, siteScopeLoading])

  const servers = useMemo(
    () => workingSet.state.status === 'ready' ? workingSet.state.data.servers : [],
    [workingSet.state],
  )
  const selectedServers = useMemo(
    () => servers.filter((server) => selected.has(server.id)),
    [selected, servers],
  )

  useEffect(() => {
    if (workingSet.state.status !== 'ready' || initialTargetIds.length === 0) return
    const compatible = initialTargetIds
      .map((id) => servers.find((server) => server.id === id))
      .filter((server): server is Server => Boolean(server && serverIsDeployable(server)))
    const derivedIntegration = compatible[0]?.source.integrationId ?? ''
    const sameIntegration = compatible.filter(
      (server) => server.source.integrationId === derivedIntegration,
    )
    if (sameIntegration.length !== initialTargetIds.length) {
      setSelected(new Set(sameIntegration.map((server) => server.id)))
      setTargetIssues([])
      setIntegrationId(derivedIntegration)
      setSearchParams(
        updateTargetParams(searchParams, sameIntegration.map((server) => server.id), derivedIntegration),
        { replace: true },
      )
      showToast({
        tone: 'warning',
        title: 'Incompatible targets removed',
        description: 'Only ready, present Servers from one provisioner can be deployed together.',
      })
    } else if (!integrationId && derivedIntegration) {
      setIntegrationId(derivedIntegration)
      setSearchParams(updateTargetParams(searchParams, initialTargetIds, derivedIntegration), { replace: true })
    }
  }, [
    initialTargetIds,
    integrationId,
    searchParams,
    servers,
    setSearchParams,
    showToast,
    workingSet.state.status,
  ])

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
    setNetworkMode(template.network?.mode ?? 'dhcp')
    setNetworkSubnetId(template.network?.subnetId ?? '')
    setDefaultGateway(template.network?.defaultGateway ?? false)
    setUserDataMode('inherit')
    setCustomized(false)
  }, [showToast, templateId, templates])

  // Accepted deployments are projections rather than durable jobs, so poll each Server
  // every five seconds only while at least one remains deploying. Unmount and result
  // replacement clear the timer and suppress stale responses.
  useEffect(() => {
    if (!result || result.accepted.length === 0) return
    let cancelled = false
    let failureReported = false
    const refresh = async () => {
      try {
        const entries = await Promise.all(result.accepted.map(async (accepted) => {
          await serverRepository.refreshServer(accepted.serverId)
          const server = await serverRepository.getServer(accepted.serverId)
          return [accepted.serverId, server] as const
        }))
        if (cancelled) return
        const next: Record<string, Server> = {}
        let stillRunning = false
        for (const [id, server] of entries) {
          if (!server) continue
          next[id] = server
          if (server.provisioning?.state === 'deploying') stillRunning = true
        }
        failureReported = false
        setLiveServers(next)
        if (stillRunning) timer = window.setTimeout(() => void refresh(), 5000)
      } catch (error) {
        if (cancelled) return
        if (!failureReported) {
          showToast({
            tone: 'warning',
            title: 'Deployment status refresh failed',
            description: error instanceof Error ? error.message : 'Unknown error',
          })
        }
        failureReported = true
        timer = window.setTimeout(() => void refresh(), 5000)
      }
    }
    let timer = window.setTimeout(() => void refresh(), 5000)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [result, serverRepository, showToast])

  const availableServers = useMemo(
    () => servers.filter((server) => (
      serverIsDeployable(server) &&
      (!integrationId || server.source.integrationId === integrationId)
    )),
    [integrationId, servers],
  )
  const selectedTemplate = templates.find((item) => item.id === templateId)
  const effectiveImageId = selectedTemplate && !customized ? selectedTemplate.imageId : imageId
  const effectiveEphemeral = selectedTemplate && !customized ? selectedTemplate.ephemeral : ephemeral
  const effectiveNetworkMode = selectedTemplate && !customized ? selectedTemplate.network?.mode ?? 'dhcp' : networkMode
  const effectiveNetworkSubnetId = selectedTemplate && !customized ? selectedTemplate.network?.subnetId ?? '' : networkSubnetId
  const effectiveDefaultGateway = selectedTemplate && !customized ? selectedTemplate.network?.defaultGateway ?? false : defaultGateway
  const assignedStaticIPs = selectedServers
    .map((server) => networkAssignments[server.id]?.ipAddress.trim() ?? '')
    .filter(Boolean)
  const networkAssignmentsValid = Boolean(networkInspection) && selectedServers.every((server) => {
    const assignment = networkAssignments[server.id]
    const target = networkInspection?.targets.find((item) => item.serverId === server.id)
    const iface = target?.network.interfaces.find((item) => item.id === assignment?.interfaceId)
    if (!assignment?.interfaceId || !assignment.subnetId || !iface?.availableSubnets.some((subnet) => subnet.id === assignment.subnetId)) return false
    return effectiveNetworkMode !== 'static' || validIPv4(assignment.ipAddress)
  }) && (effectiveNetworkMode !== 'static' || new Set(assignedStaticIPs).size === assignedStaticIPs.length)
  const assignedSubnetIds = [...new Set(selectedServers
    .map((server) => networkAssignments[server.id]?.subnetId)
    .filter((value): value is string => Boolean(value)))]
  const reusableSubnetId = effectiveNetworkSubnetId || (assignedSubnetIds.length === 1 ? assignedSubnetIds[0] : '')
  const reusableNetworkValid = effectiveNetworkMode === 'dhcp' || Boolean(reusableSubnetId)
  const targetsValid = selected.size > 0 && selected.size <= MAX_TARGETS &&
    selectedServers.length === selected.size &&
    selectedServers.every((server) => (
      serverIsDeployable(server) && server.source.integrationId === integrationId
    ))
  const configurationValid = Boolean(
    integrationId &&
    effectiveImageId &&
    !catalogError &&
    !catalogLoading &&
    (userDataMode !== 'replace' || userData) &&
    networkAssignmentsValid,
  )
  const inheritedSecretCannotBeSaved = Boolean(
    saveTemplate &&
    selectedTemplate?.hasUserData &&
    customized &&
    userDataMode === 'inherit',
  )
  const reviewValid = targetsValid && configurationValid &&
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
      const suggestedMode: DeploymentNetworkMode = inspection.targets.some(
        (target) => target.suggestion.mode === 'static',
      ) ? 'static' : 'dhcp'
      if (appliesInspectionDefaults) {
        setNetworkMode(suggestedMode)
        setDefaultGateway(
          suggestedMode === 'static' &&
          inspection.targets.some((target) => target.suggestion.mode === 'static' && target.suggestion.defaultGateway),
        )
      }
      setNetworkInspection(inspection)
      const nextAssignments: Record<string, { interfaceId: string; subnetId: string; ipAddress: string }> = {}
      for (const target of inspection.targets) {
        const previous = networkAssignments[target.serverId]
        nextAssignments[target.serverId] = {
          interfaceId: target.suggestion.interfaceId,
          subnetId: effectiveNetworkSubnetId || target.suggestion.subnetId,
          ipAddress: previous?.ipAddress ??
            (target.suggestion.mode === 'static' ? target.suggestion.ipAddress : ''),
        }
      }
      setNetworkAssignments(nextAssignments)
      onNext()
    } catch (error) {
      showToast({
        tone: 'error',
        title: 'Could not check deployment readiness',
        description: error instanceof Error ? error.message : 'Unknown error',
      })
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
    setNetworkMode('dhcp')
    setNetworkSubnetId('')
    setDefaultGateway(false)
    setTemplateId('')
    setTargetIssues([])
    setCustomized(false)
    setImageId('')
    setEphemeral(false)
    setUserDataMode('omit')
    setUserData('')
    setResult(null)
    setSearchParams(updateTargetParams(searchParams, [], nextIntegrationId), { replace: true })
  }

  const toggleServer = (serverId: string) => {
    setTargetIssues([])
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
      setNetworkMode('dhcp')
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
    setNetworkMode(template.network?.mode ?? 'dhcp')
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
      const response: DeployServersResult = await provisioning.deployServers({
        serverIds: [...selected],
        templateId: selectedTemplate?.id,
        settings: !selectedTemplate || customized
          ? { imageId: effectiveImageId, ephemeral: effectiveEphemeral }
          : undefined,
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
        tone: response.failed.length ? 'warning' : 'success',
        title: response.failed.length ? 'Deployment partially accepted' : 'Deployment accepted',
        description: `${response.accepted.length} accepted, ${response.failed.length} failed.`,
      })
      if (response.failed.length === 0) {
        try {
          sessionStorage.removeItem(RESULT_STORAGE_KEY)
        } catch {
          // Navigation does not depend on browser storage being available.
        }
        navigate(scopedHref('/servers'), { replace: true })
        return
      }
      const stored: StoredDeploymentResult = {
        ...response,
        serverIds: [...selected],
        integrationId,
        savedAt: new Date().toISOString(),
      }
      storeResult(stored)
      setResult(stored)
    } catch (error) {
      showToast({
        tone: 'error',
        title: 'Deployment preflight failed',
        description: error instanceof Error ? error.message : 'Unknown error',
      })
    } finally {
      setSubmitting(false)
    }
  }

  const retryDeployment = () => {
    setResult(null)
    setLiveServers({})
    setTargetIssues([])
    try {
      sessionStorage.removeItem(RESULT_STORAGE_KEY)
    } catch {
      // The active workflow still restarts when session storage is unavailable.
    }
  }


  const cancel = () => navigate(scopedHref('/servers'))
  const startIndex = result ? 4 : 1

  return <div className="operator-page">
    <PageHeader
      title="Deploy OS"
      subtitle="Apply one operating system configuration to as many as 100 ready Servers."
      breadcrumbs={[{ label: 'Provisioning' }, { label: 'Deploy OS' }]}
    />
    <ProvisioningTabs />
    {workingSet.state.status === 'loading' || resourcesLoading ? <LoadingState rows={7} /> : null}
    {workingSet.state.status === 'error' && <ErrorState message={workingSet.state.message} onRetry={workingSet.reload} />}
    {workingSet.state.status === 'ready' && !resourcesLoading && (
      <Wizard
        key={result ? 'provisioning-result' : 'provisioning-form'}
        aria-label="Deploy operating system"
        className="sw-deploy-wizard"
        height="min(700px, calc(100vh - 220px))"
        startIndex={startIndex}
        isVisitRequired
        shouldFocusContent
        onClose={cancel}
        onSave={cancel}
        footer={(activeStep, onNext, onBack, onClose) => (
          <WizardFooter
            activeStep={activeStep}
            onNext={
              activeStep.id === 'os-targets' ? (event) => void checkTargets(() => onNext(event)) :
              activeStep.id === 'os-review' ? () => void deploy() : onNext
            }
            onBack={onBack}
            onClose={onClose}
            nextButtonText={
              activeStep.id === 'os-review' ? 'Deploy OS' :
              activeStep.id === 'os-results' ? 'Done' :
              activeStep.id === 'os-targets' && targetIssues.length > 0 ? 'Check again' : undefined
            }
            isNextDisabled={
              activeStep.id === 'os-targets' ? !targetsValid || checkingTargets :
              activeStep.id === 'os-configuration' ? !configurationValid :
              activeStep.id === 'os-review' ? !reviewValid || submitting :
              false
            }
            isBackHidden={activeStep.id === 'os-results'}
            nextButtonProps={activeStep.id === 'os-targets'
              ? { isLoading: checkingTargets }
              : activeStep.id === 'os-review' ? { isLoading: submitting } : undefined}
          />
        )}
      >
        <WizardStep name="Targets" id="os-targets" status={targetIssues.length ? 'warning' : targetsValid ? 'success' : 'default'}>
          <WizardSection title="Deployment targets">
            <FormGroup label="Provisioner integration" isRequired fieldId="deploy-integration">
              <SingleSelect
                id="deploy-integration"
                ariaLabel="Provisioner integration"
                value={integrationId}
                placeholder="Select an integration"
                options={integrations.map((integration) => ({ value: integration.id, label: integration.name }))}
                isRequired
                onChange={selectIntegration}
              />
            </FormGroup>
            <div className="sw-target-summary">
              <strong>{selected.size} of {MAX_TARGETS} selected</strong>
              {selected.size > 0 && <Button variant="link" isInline onClick={() => {
                setTargetIssues([])
                setSelected(new Set())
                setSearchParams(updateTargetParams(searchParams, [], integrationId), { replace: true })
              }}>Clear</Button>}
            </div>
            {targetIssues.length > 0 && <div className="sw-target-issues" aria-live="polite">
              {targetIssues.map((issue) => <Alert key={`${issue.serverId}:${issue.code}`} variant={AlertVariant.warning} title={`${targetIssueName(issue, servers)}: Deployment blocked`} isInline>
                <div>{issue.message}</div>
                <Button variant="link" isInline onClick={() => navigate(scopedHref(`/servers/${issue.serverId}/network`))}>Review Server Network</Button>
              </Alert>)}
            </div>}
            {!integrationId && <EmptyState title="Select a provisioner" message="Targets in a batch must come from the same integration." />}
            {integrationId && availableServers.length === 0 && <EmptyState title="No ready Servers" message="This provisioner has no present Server in the ready state." />}
            {integrationId && availableServers.length > 0 && (
              <StickyTableFrame>
                <Table aria-label="Deployment targets" variant="compact">
                  <Thead><Tr><Th /><Th>Server</Th><Th>Address</Th><Th>Power</Th><Th>State</Th></Tr></Thead>
                  <Tbody>{availableServers.map((server) => (
                    <Tr key={server.id}>
                      <Td className="sw-cell-center">
                        <Checkbox id={`deploy-target-${server.id}`} aria-label={`Select ${serverDisplayName(server)}`} isChecked={selected.has(server.id)} onChange={() => toggleServer(server.id)} />
                      </Td>
                      <Td dataLabel="Server"><strong>{serverDisplayName(server)}</strong></Td>
                      <Td dataLabel="Address" className="sw-mono">{server.addresses[0] ?? '-'}</Td>
                      <Td dataLabel="Power">{server.provisioning?.powerState ?? '-'}</Td>
                      <Td dataLabel="State"><ProvisioningBadge axis={server.provisioning} /></Td>
                    </Tr>
                  ))}</Tbody>
                </Table>
              </StickyTableFrame>
            )}
          </WizardSection>
        </WizardStep>

        <WizardStep name="Configuration" id="os-configuration" status={configurationValid ? 'success' : 'default'}>
          <WizardSection title="Operating system configuration">
            <Form className="sw-form-grid">
              <FormGroup label="Configuration source" isRequired fieldId="deploy-template">
                <FormSelect id="deploy-template" value={templateId} onChange={(_event, value) => selectTemplate(value)}>
                  <FormSelectOption value="" label="Custom configuration" />
                  {templates.filter((template) => template.integrationId === integrationId).map((template) => (
                    <FormSelectOption key={template.id} value={template.id} label={template.name} />
                  ))}
                </FormSelect>
              </FormGroup>
              {selectedTemplate && <FormGroup fieldId="customize-template">
                <Button variant={customized ? 'secondary' : 'link'} onClick={() => setCustomized((value) => !value)}>
                  {customized ? 'Use template defaults' : 'Customize'}
                </Button>
              </FormGroup>}
              <FormGroup
                label="OS image"
                isRequired
                fieldId="deploy-image"
                labelInfo={
                  <Button
                    variant="link"
                    isInline
                    icon={<SyncAltIcon />}
                    isLoading={catalogLoading}
                    isDisabled={!integrationId || catalogLoading}
                    onClick={() => setCatalogRefreshNonce((value) => value + 1)}
                  >
                    Refresh
                  </Button>
                }
              >
                <SingleSelect
                  id="deploy-image"
                  ariaLabel="OS image"
                  value={effectiveImageId}
                  placeholder={catalogLoading ? 'Loading images...' : images.length === 0 ? 'No deployable images available' : 'Select an image'}
                  options={[
                    ...(effectiveImageId && !images.some((image) => image.id === effectiveImageId) ? [{ value: effectiveImageId, label: effectiveImageId }] : []),
                    ...images.map((image) => ({ value: image.id, label: `${image.name} (${image.architecture})` })),
                  ]}
                  isDisabled={Boolean(selectedTemplate && !customized) || Boolean(catalogError) || catalogLoading}
                  isRequired
                  onChange={setImageId}
                />
                {!catalogLoading && !catalogError && <small className="sw-field-note" aria-live="polite">{images.length} deployable image{images.length === 1 ? '' : 's'} returned by the provider.</small>}
              </FormGroup>
              <FormGroup fieldId="deploy-ephemeral">
                <Checkbox id="deploy-ephemeral" label="Ephemeral deployment" isChecked={effectiveEphemeral} isDisabled={Boolean(selectedTemplate && !customized)} onChange={(_event, checked) => setEphemeral(checked)} />
              </FormGroup>
              <FormGroup label="Cloud-init" isRequired fieldId="deploy-user-data-mode">
                <FormSelect id="deploy-user-data-mode" value={userDataMode} onChange={(_event, value) => { setUserDataMode(value as DeploymentUserDataMode); if (value !== 'replace') setUserData('') }}>
                  {selectedTemplate && <FormSelectOption value="inherit" label={selectedTemplate.hasUserData ? 'Inherit template cloud-init' : 'Inherit (template has none)'} />}
                  <FormSelectOption value="replace" label="Replace for this deployment" />
                  <FormSelectOption value="omit" label="Omit cloud-init" />
                </FormSelect>
              </FormGroup>
              {userDataMode === 'replace' && <FormGroup label="Cloud-init" isRequired fieldId="deploy-user-data">
                <TextArea id="deploy-user-data" value={userData} onChange={(_event, value) => setUserData(value)} rows={10} autoComplete="off" />
              </FormGroup>}
            </Form>
            <section className="sw-section">
              <SectionHeader
                title="Network configuration"
                description="Swallow applies an explicit DHCP or Static intent before deployment. Existing provider-managed modes are never reused implicitly."
              />
              <div className="sw-section-body">
                <Form className="sw-form-grid">
                  <FormGroup label="Addressing mode" isRequired fieldId="deploy-network-mode">
                    <ToggleGroup aria-label="Deployment network mode">
                      <ToggleGroupItem text="DHCP" buttonId="deploy-network-dhcp" isSelected={effectiveNetworkMode === 'dhcp'} isDisabled={Boolean(selectedTemplate && !customized)} onChange={() => { setNetworkMode('dhcp'); setDefaultGateway(false) }} />
                      <ToggleGroupItem text="Static" buttonId="deploy-network-static" isSelected={effectiveNetworkMode === 'static'} isDisabled={Boolean(selectedTemplate && !customized)} onChange={() => setNetworkMode('static')} />
                    </ToggleGroup>
                  </FormGroup>
                  {effectiveNetworkMode === 'static' && <FormGroup fieldId="deploy-network-default-gateway">
                    <Checkbox
                      id="deploy-network-default-gateway"
                      label="Use the selected subnet for the default route"
                      description="For each target, the provider makes this Static link the IPv4 default route using the gateway address configured on its selected subnet."
                      isChecked={effectiveDefaultGateway}
                      isDisabled={Boolean(selectedTemplate && !customized)}
                      onChange={(_event, checked) => setDefaultGateway(checked)}
                    />
                  </FormGroup>}
                </Form>
                {!networkInspection && <Alert variant={AlertVariant.warning} title="Network inspection is required" isInline>Return to Targets and run the readiness check again.</Alert>}
                {networkInspection && !networkAssignmentsValid && <Alert variant={AlertVariant.warning} title="Complete every network assignment" isInline>Select a NIC and subnet for each Server. Static mode also requires a unique IPv4 address per target.</Alert>}
              </div>
              {networkInspection && <StickyTableFrame>
                <Table aria-label="Deployment network assignments" variant="compact" className="sw-network-assignment-table">
                  <Thead><Tr>
                    <Th className="sw-network-server-column">Server</Th>
                    <Th className="sw-network-interface-column">Interface</Th>
                    <Th className="sw-network-subnet-column">Subnet</Th>
                    {effectiveNetworkMode === 'static' && <Th className="sw-network-address-column">Static IPv4 address</Th>}
                    <Th className="sw-network-current-mode-column"><span title="Current provider mode">Current mode</span></Th>
                  </Tr></Thead>
                  <Tbody>{networkInspection.targets.map((target) => {
                    const server = selectedServers.find((item) => item.id === target.serverId)
                    const assignment = networkAssignments[target.serverId] ?? { interfaceId: '', subnetId: '', ipAddress: '' }
                    const iface = target.network.interfaces.find((item) => item.id === assignment.interfaceId)
                    const currentProviderMode = iface?.rawProviderMode === 'AUTO' ? 'Provider-managed (MAAS AUTO)' : iface?.rawProviderMode || '-'
                    return <Tr key={target.serverId}>
                      <Td dataLabel="Server" className="sw-network-server-column"><strong>{server ? serverDisplayName(server) : target.serverId}</strong></Td>
                      <Td dataLabel="Interface" className="sw-network-interface-column">
                        <FormSelect aria-label={`Interface for ${server ? serverDisplayName(server) : target.serverId}`} value={assignment.interfaceId} onChange={(_event, value) => {
                          const nextInterface = target.network.interfaces.find((item) => item.id === value)
                          const compatible = nextInterface?.availableSubnets.some((subnet) => subnet.id === assignment.subnetId)
                          const suggestedSubnet = compatible ? assignment.subnetId : nextInterface?.availableSubnets.length === 1 ? nextInterface.availableSubnets[0].id : ''
                          setNetworkAssignments((current) => ({ ...current, [target.serverId]: { ...assignment, interfaceId: value, subnetId: suggestedSubnet } }))
                        }}>
                          <FormSelectOption value="" label="Select an interface" isDisabled isPlaceholder />
                          {target.network.interfaces.map((item) => <FormSelectOption key={item.id} value={item.id} label={`${item.name} - ${item.macAddress}${item.boot ? ' (boot NIC)' : ''}`} />)}
                        </FormSelect>
                      </Td>
                      <Td dataLabel="Subnet" className="sw-network-subnet-column">
                        <FormSelect aria-label={`Subnet for ${server ? serverDisplayName(server) : target.serverId}`} value={assignment.subnetId} onChange={(_event, value) => setNetworkAssignments((current) => ({ ...current, [target.serverId]: { ...assignment, subnetId: value } }))}>
                          <FormSelectOption value="" label="Select a subnet" isDisabled isPlaceholder />
                          {iface?.availableSubnets.map((subnet) => <FormSelectOption key={subnet.id} value={subnet.id} label={formatSubnetOptionLabel(subnet)} />)}
                        </FormSelect>
                      </Td>
                      {effectiveNetworkMode === 'static' && <Td dataLabel="Static IPv4 address" className="sw-network-address-column"><TextInput aria-label={`Static IPv4 address for ${server ? serverDisplayName(server) : target.serverId}`} value={assignment.ipAddress} onChange={(_event, value) => setNetworkAssignments((current) => ({ ...current, [target.serverId]: { ...assignment, ipAddress: value } }))} placeholder="192.0.2.10" /></Td>}
                      <Td dataLabel="Current mode" className="sw-network-current-mode-column" title={currentProviderMode}>{currentProviderMode}</Td>
                    </Tr>
                  })}</Tbody>
                </Table>
              </StickyTableFrame>}
            </section>
            {catalogError && <Alert variant={AlertVariant.warning} title="Image catalog unavailable" isInline>{catalogError}</Alert>}
            {selectedTemplate && !customized && <Alert variant={AlertVariant.info} title="Template settings are locked" isInline>Choose Customize to override the image or ephemeral setting.</Alert>}
          </WizardSection>
        </WizardStep>

        <WizardStep name="Review" id="os-review" status={reviewValid ? 'success' : 'warning'}>
          <WizardSection title="Review deployment">
            <Card isCompact>
              <CardTitle>{selected.size} Server{selected.size === 1 ? '' : 's'}</CardTitle>
              <CardBody>
                <DescriptionList isHorizontal isCompact>
                  <ReviewItem label="Integration" value={integrations.find((item) => item.id === integrationId)?.name ?? integrationId} />
                  <ReviewItem label="Configuration" value={selectedTemplate ? `${selectedTemplate.name}${customized ? ' (customized)' : ''}` : 'Custom'} />
                  <ReviewItem label="Image" value={effectiveImageId} />
                  <ReviewItem label="Ephemeral" value={effectiveEphemeral ? 'Yes' : 'No'} />
                  <ReviewItem label="Cloud-init" value={userDataMode === 'inherit' ? 'Inherit from template' : userDataMode === 'replace' ? 'Replace for this deployment' : 'Omit'} />
                  <ReviewItem label="Network mode" value={effectiveNetworkMode === 'dhcp' ? 'DHCP' : 'Static'} />
                  <ReviewItem label="Default gateway" value={effectiveDefaultGateway ? 'Selected links' : 'Provider routing'} />
                </DescriptionList>
              </CardBody>
            </Card>
            <StickyTableFrame>
              <Table aria-label="Deployment review targets" variant="compact">
                <Thead><Tr><Th>Server</Th><Th>Provider machine ID</Th><Th>Interface</Th><Th>Subnet</Th><Th>IP address</Th><Th>Power</Th></Tr></Thead>
                <Tbody>{selectedServers.map((server) => <Tr key={server.id}>
                  <Td>{serverDisplayName(server)}</Td>
                  <Td className="sw-mono">{server.source.providerMachineId}</Td>
                  <Td>{networkInspection?.targets.find((target) => target.serverId === server.id)?.network.interfaces.find((iface) => iface.id === networkAssignments[server.id]?.interfaceId)?.name || '-'}</Td>
                  <Td>{networkInspection?.targets.find((target) => target.serverId === server.id)?.network.interfaces.flatMap((iface) => iface.availableSubnets).find((subnet) => subnet.id === networkAssignments[server.id]?.subnetId)?.cidr || '-'}</Td>
                  <Td className="sw-mono">{effectiveNetworkMode === 'static' ? networkAssignments[server.id]?.ipAddress || '-' : 'DHCP'}</Td>
                  <Td>{server.provisioning?.powerState ?? '-'}</Td>
                </Tr>)}</Tbody>
              </Table>
            </StickyTableFrame>
            {(!selectedTemplate || customized) && <Card isCompact>
              <CardTitle>Reuse this configuration</CardTitle>
              <CardBody className="sw-template-editor">
                <Checkbox id="save-deployment-template" label="Save as a deployment template" isChecked={saveTemplate} onChange={(_event, checked) => setSaveTemplate(checked)} />
                {saveTemplate && <TextInput aria-label="New deployment template name" value={templateName} onChange={(_event, value) => setTemplateName(value)} placeholder="Template name" />}
                {inheritedSecretCannotBeSaved && <Alert variant={AlertVariant.warning} title="Inherited cloud-init cannot be copied" isInline>Select Replace or Omit before saving a customized template.</Alert>}
                {saveTemplate && !reusableNetworkValid && <Alert variant={AlertVariant.warning} title="Static templates require one shared subnet" isInline>Choose the same subnet for every target before saving this configuration as a template.</Alert>}
              </CardBody>
            </Card>}
            <Alert variant={AlertVariant.warning} title="Deployment starts immediately after preflight" isInline>Accepted provider requests cannot be rolled back as a batch.</Alert>
          </WizardSection>
        </WizardStep>

        <WizardStep name="Results" id="os-results" isDisabled={!result} status={result?.failed.length ? 'warning' : result ? 'success' : 'default'}>
          <WizardSection title="Deployment results">
            {result && <>
              <div className="sw-result-summary">
                <Label color="green">{result.accepted.length} accepted</Label>
                <Label color={result.failed.length ? 'red' : 'grey'}>{result.failed.length} failed</Label>
                <Button variant="secondary" onClick={retryDeployment}>Configure and try again</Button>
              </div>
              {result.accepted.length > 0 && <StickyTableFrame>
                <Table aria-label="Accepted deployments" variant="compact">
                  <Thead><Tr><Th>Server</Th><Th>Current state</Th><Th>Observed</Th></Tr></Thead>
                  <Tbody>{result.accepted.map((accepted) => {
                    const live = liveServers[accepted.serverId]
                    return <Tr key={accepted.serverId}>
                      <Td><Button variant="link" isInline onClick={() => navigate(scopedHref(`/servers/${accepted.serverId}/summary`))}>{live ? serverDisplayName(live) : accepted.serverId}</Button></Td>
                      <Td><ProvisioningBadge axis={live?.provisioning ?? { ...accepted, integrationId: result.integrationId }} /></Td>
                      <Td>{live?.provisioning?.observedAt ?? accepted.observedAt}</Td>
                    </Tr>
                  })}</Tbody>
                </Table>
              </StickyTableFrame>}
              {result.failed.map((failure) => <Alert key={failure.serverId} variant={AlertVariant.danger} title={`${failure.serverId}: ${failure.stage.replaceAll('_', ' ')}`} isInline>{failure.message}</Alert>)}
            </>}
          </WizardSection>
        </WizardStep>
      </Wizard>
    )}
  </div>
}

function WizardSection({ title, children }: { title: string; children: ReactNode }) {
  return <section className="sw-wizard-section"><Title headingLevel="h2" size="lg">{title}</Title>{children}</section>
}

function ReviewItem({ label, value }: { label: string; value: ReactNode }) {
  return <DescriptionListGroup><DescriptionListTerm>{label}</DescriptionListTerm><DescriptionListDescription>{value || '-'}</DescriptionListDescription></DescriptionListGroup>
}
