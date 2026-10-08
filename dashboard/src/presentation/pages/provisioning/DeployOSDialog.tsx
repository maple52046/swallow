import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import {
  Button,
  Card,
  Field,
  Heading,
  HStack,
  Input,
  Stack,
  Table,
  Text,
  Textarea,
} from '@chakra-ui/react'
import { Search } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import type {
  DeploymentNetworkMode,
  DeploymentTargetIssue,
  DeployTarget,
  NetworkInspectionResult,
  DeploymentUserDataMode,
} from '@/domain/provisioning/types'
import { DEPLOY_TARGET_LABELS } from '@/domain/provisioning/types'
import { serverDisplayName, type Server } from '@/domain/server/types'
import type { Integration, OSImage } from '@/domain/site/types'
import { ConfirmDialog } from '@/presentation/components/ConfirmDialog'
import { LoadingState } from '@/presentation/components/LoadingState'
import { StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { useToast } from '@/presentation/components/toast/toastContext'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import { Modal } from '@/presentation/components/ui/modal'
import { Select } from '@/presentation/components/ui/select'
import { useExperimentalFeature } from '@/presentation/contexts/ExperimentalFeaturesContext'
import { useServerWorkingSet } from '@/presentation/pages/servers/useServerWorkingSet'
import { AddressingModeField } from './AddressingModeField'
import { NetworkAssignmentsEditor } from './NetworkAssignmentsEditor'
import { DeployTargetField } from './DeployTargetField'
import { OSImageSelectionStep } from './OSImageSelectionStep'
import { UnsupportedDeployModeWarning } from './UnsupportedDeployModeWarning'
import {
  defaultDeployTargetForImage,
  osImageSupportsDeployTarget,
  toDeployServersInput,
  type OSDeploymentConfiguration,
  type OSDeploymentLaunchContext,
} from './osDeploymentFlow'

const MAX_TARGETS = 100

type ContextualLaunchContext = Extract<
  OSDeploymentLaunchContext,
  { kind: 'fixed-targets' | 'fixed-image' }
>

interface DeployOSDialogProps {
  context: ContextualLaunchContext
  onClose: () => void
  onLaunched: (operationId: string, targetIds: readonly string[]) => void
}

type ResourceState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; integrations: Integration[] }

type ProvisionerResolution =
  | { status: 'loading' }
  | { status: 'error'; title: string; message: string }
  | { status: 'ready'; integration: Integration }

type InspectionState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'attention'; issues: DeploymentTargetIssue[] }
  | { status: 'error'; message: string }
  | { status: 'ready'; inspection: NetworkInspectionResult }

interface NetworkAssignmentDraft {
  interfaceId: string
  subnetId: string
  ipAddress: string
}

type DialogStepId = 'os-targets' | 'os-operating-system' | 'os-installation' | 'os-networking' | 'os-review'

interface DialogStep {
  id: DialogStepId
  name: string
  content: ReactNode
  canProceed: boolean
  onNext?: (proceed: () => void) => void
  nextLabel?: string
  nextLoading?: boolean
}

function siteHref(path: string, siteId: string): string {
  const target = new URL(path, window.location.origin)
  target.searchParams.set('site', siteId)
  return `${target.pathname}${target.search}${target.hash}`
}

function serverIsSelectable(server: Server): boolean {
  return !server.absent && !server.provisioning?.locked && server.provisioning?.state === 'ready'
}

function serverMemoryLabel(memoryMiB: number): string {
  if (memoryMiB <= 0) return ''
  const gib = memoryMiB / 1024
  return `${Number.isInteger(gib) ? gib : gib.toFixed(1)} GiB RAM`
}

function serverTargetFacts(server: Server): string {
  return [
    server.addresses[0] || 'No assigned address',
    server.architecture,
    server.cpuCores > 0 ? `${server.cpuCores} CPU${server.cpuCores === 1 ? '' : 's'}` : '',
    serverMemoryLabel(server.memoryMiB),
  ].filter(Boolean).join(' · ')
}

function validIPv4(value: string): boolean {
  const octets = value.trim().split('.')
  return octets.length === 4 && octets.every(
    (octet) => /^(0|[1-9]\d{0,2})$/.test(octet) && Number(octet) <= 255,
  )
}

function issueServerName(issue: DeploymentTargetIssue, servers: readonly Server[]): string {
  const server = servers.find((candidate) => candidate.id === issue.serverId)
  return server ? serverDisplayName(server) : issue.serverId
}

/** A semantic content section within the purpose-built contextual deployment dialog. */
function DialogWizardSection({
  title,
  action,
  children,
}: {
  title: string
  action?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="sw-deploy-os-dialog__step-content">
      <div className="sw-deploy-os-dialog__step-heading">
        <Heading as="h2" size="md">{title}</Heading>
        {action}
      </div>
      {children}
    </section>
  )
}

function resolveProvisioner(
  context: ContextualLaunchContext,
  state: ResourceState,
): ProvisionerResolution {
  if (state.status === 'loading') return { status: 'loading' }
  if (state.status === 'error') {
    return {
      status: 'error',
      title: 'Provisioner configuration unavailable',
      message: state.message,
    }
  }
  if (state.integrations.length === 0) {
    return {
      status: 'error',
      title: 'Connect a provisioner',
      message: 'This Site has no provisioner Integration. Connect one before deploying an OS.',
    }
  }
  if (state.integrations.length > 1) {
    return {
      status: 'error',
      title: 'Site configuration conflict',
      message: 'OS deployment requires exactly one provisioner Integration for this Site.',
    }
  }
  const integration = state.integrations[0]
  if (!integration.enabled) {
    return {
      status: 'error',
      title: 'Enable the Site provisioner',
      message: `${integration.name} is disabled. Enable it before deploying an OS.`,
    }
  }
  const sourceMismatch = context.kind === 'fixed-image'
    ? context.integrationId !== integration.id
    : context.targets.some(
        (server) =>
          server.source.siteId !== context.siteId ||
          server.source.integrationId !== integration.id,
      )
  if (sourceMismatch) {
    return {
      status: 'error',
      title: 'Inconsistent deployment source',
      message: 'The selected Server or OS image does not belong to the Site\'s configured provisioner.',
    }
  }
  return { status: 'ready', integration }
}

/**
 * Contextual OS deployment dialog used by Server and OS Image entry points.
 *
 * Fixed-target launches perform live preflight and network inspection before exposing
 * Operating system, Installation, and Networking steps. Fixed-image launches keep the image
 * immutable, begin at Targets, and continue directly to Installation. Cloud-init stays only in
 * component memory; successful launches remain on the source page.
 */
export function DeployOSDialog({ context, onClose, onLaunched }: DeployOSDialogProps) {
  const { provisioning, sites: siteRepository } = useApp()
  const navigate = useNavigate()
  const { showToast } = useToast()
  const templatesEnabled = useExperimentalFeature('deploymentTemplates')
  const workingSet = useServerWorkingSet({ siteId: context.siteId, includeAbsent: true })
  const [resourceNonce, setResourceNonce] = useState(0)
  const [resourceState, setResourceState] = useState<ResourceState>({ status: 'loading' })
  const [images, setImages] = useState<OSImage[]>(
    context.kind === 'fixed-image' ? [context.image] : [],
  )
  const [catalogLoading, setCatalogLoading] = useState(context.kind === 'fixed-targets')
  const [catalogError, setCatalogError] = useState('')
  const [catalogNonce, setCatalogNonce] = useState(0)
  const initialTargetIds = context.kind === 'fixed-targets'
    ? context.targets.map((server) => server.id)
    : []
  const [selected, setSelected] = useState<ReadonlySet<string>>(
    () => new Set(initialTargetIds),
  )
  const [targetQuery, setTargetQuery] = useState('')
  const [imageId, setImageId] = useState(
    context.kind === 'fixed-image' ? context.image.id : '',
  )
  const [deployTarget, setDeployTarget] = useState<DeployTarget>(() =>
    defaultDeployTargetForImage(context.kind === 'fixed-image' ? context.image : undefined),
  )
  const [userDataMode, setUserDataMode] = useState<DeploymentUserDataMode>('omit')
  const [userData, setUserData] = useState('')
  const [networkMode, setNetworkMode] = useState<DeploymentNetworkMode>('automatic')
  const [defaultGateway, setDefaultGateway] = useState(false)
  const [networkAssignments, setNetworkAssignments] = useState<
    Record<string, NetworkAssignmentDraft>
  >({})
  const [inspectionState, setInspectionState] = useState<InspectionState>({ status: 'idle' })
  const [saveTemplate, setSaveTemplate] = useState(false)
  const [templateName, setTemplateName] = useState('')
  const [dirty, setDirty] = useState(false)
  const [confirmDiscard, setConfirmDiscard] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [currentStepId, setCurrentStepId] = useState<DialogStepId>(
    context.kind === 'fixed-image' ? 'os-targets' : 'os-operating-system',
  )
  const autoInspectionStarted = useRef(false)
  const stepStatusRef = useRef<HTMLDivElement>(null)
  const progressRef = useRef<HTMLElement>(null)
  const scrollViewportRef = useRef<HTMLDivElement>(null)
  const previousStepIdRef = useRef(currentStepId)

  useEffect(() => {
    let cancelled = false
    setResourceState({ status: 'loading' })
    siteRepository
      .listIntegrations({ siteId: context.siteId, kind: 'provisioner' })
      .then((integrations) => {
        if (!cancelled) setResourceState({ status: 'ready', integrations })
      })
      .catch((error: Error) => {
        if (!cancelled) setResourceState({ status: 'error', message: error.message })
      })
    return () => {
      cancelled = true
    }
  }, [context.siteId, resourceNonce, siteRepository])

  const resolution = useMemo(
    () => resolveProvisioner(context, resourceState),
    [context, resourceState],
  )
  const integration = resolution.status === 'ready' ? resolution.integration : undefined
  const resolvedIntegrationId = integration?.id ?? ''

  useEffect(() => {
    if (context.kind !== 'fixed-targets') {
      setCatalogLoading(false)
      setCatalogError('')
      return
    }
    if (!resolvedIntegrationId) {
      setImages([])
      setCatalogLoading(false)
      return
    }
    let cancelled = false
    setCatalogLoading(true)
    setCatalogError('')
    provisioning
      .listOSImages(resolvedIntegrationId)
      .then((items) => {
        if (!cancelled) setImages(items)
      })
      .catch((error: Error) => {
        if (!cancelled) setCatalogError(error.message)
      })
      .finally(() => {
        if (!cancelled) setCatalogLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [
    catalogNonce,
    context.kind,
    provisioning,
    resolvedIntegrationId,
  ])

  const servers = useMemo(() => {
    if (context.kind === 'fixed-targets') return context.targets
    return workingSet.state.status === 'ready' ? workingSet.state.data.servers : []
  }, [context, workingSet.state])
  const selectedServers = useMemo(
    () => servers.filter((server) => selected.has(server.id)),
    [selected, servers],
  )
  const availableServers = useMemo(
    () => integration
      ? servers.filter(
          (server) =>
            server.source.siteId === context.siteId &&
            server.source.integrationId === integration.id &&
            serverIsSelectable(server),
        )
      : [],
    [context.siteId, integration, servers],
  )
  const normalizedTargetQuery = targetQuery.trim().toLocaleLowerCase()
  const filteredAvailableServers = useMemo(
    () => availableServers.filter((server) => {
      if (!normalizedTargetQuery) return true
      return [
        serverDisplayName(server),
        server.fqdn ?? '',
        ...server.addresses,
        server.source.providerMachineId,
      ].some((value) => value.toLocaleLowerCase().includes(normalizedTargetQuery))
    }),
    [availableServers, normalizedTargetQuery],
  )

  useEffect(() => {
    if (context.kind !== 'fixed-image' || workingSet.state.status !== 'ready') return
    const eligibleIds = new Set(availableServers.map((server) => server.id))
    const eligibleSelection = new Set([...selected].filter((id) => eligibleIds.has(id)))
    if (eligibleSelection.size === selected.size) return
    setSelected(eligibleSelection)
    setInspectionState({ status: 'idle' })
    setNetworkAssignments({})
  }, [availableServers, context.kind, selected, workingSet.state.status])
  const effectiveImageId = context.kind === 'fixed-image' ? context.image.id : imageId
  const effectiveDeployTarget = deployTarget
  const effectiveNetworkMode = networkMode
  const effectiveDefaultGateway = defaultGateway
  const selectedImage = images.find((image) => image.id === effectiveImageId)
  const selectedImageModeUnsupported = Boolean(
    selectedImage && !osImageSupportsDeployTarget(selectedImage, effectiveDeployTarget),
  )
  const targetSelectionValid =
    selected.size > 0 &&
    selected.size <= MAX_TARGETS &&
    selectedServers.length === selected.size &&
    Boolean(integration) &&
    selectedServers.every(
      (server) =>
        serverIsSelectable(server) &&
        server.source.siteId === context.siteId &&
        server.source.integrationId === integration?.id,
    )
  const networkInspection = inspectionState.status === 'ready'
    ? inspectionState.inspection
    : null
  const assignedStaticIPs = selectedServers
    .map((server) => networkAssignments[server.id]?.ipAddress.trim() ?? '')
    .filter(Boolean)
  const networkAssignmentsValid =
    Boolean(networkInspection) &&
    selectedServers.every((server) => {
      const assignment = networkAssignments[server.id]
      const target = networkInspection?.targets.find((candidate) => candidate.serverId === server.id)
      const iface = target?.network.interfaces.find(
        (candidate) => candidate.id === assignment?.interfaceId,
      )
      if (
        !assignment?.interfaceId ||
        !assignment.subnetId ||
        !iface?.availableSubnets.some((subnet) => subnet.id === assignment.subnetId)
      ) {
        return false
      }
      return effectiveNetworkMode !== 'static' || validIPv4(assignment.ipAddress)
    }) &&
    (
      effectiveNetworkMode !== 'static' ||
      new Set(assignedStaticIPs).size === assignedStaticIPs.length
    )
  const assignedSubnetIds = [
    ...new Set(
      selectedServers
        .map((server) => networkAssignments[server.id]?.subnetId)
        .filter((value): value is string => Boolean(value)),
    ),
  ]
  const reusableSubnetId = assignedSubnetIds.length === 1 ? assignedSubnetIds[0] : ''
  const reusableNetworkValid =
    effectiveNetworkMode === 'automatic' || Boolean(reusableSubnetId)
  const operatingSystemValid = Boolean(
    integration &&
      effectiveImageId &&
      !catalogLoading &&
      !catalogError,
  )
  const installationValid = Boolean(
    userDataMode !== 'replace' || userData.trim(),
  )
  const networkingValid = networkAssignmentsValid
  const configurationValid =
    operatingSystemValid && installationValid && networkingValid
  const reviewValid =
    targetSelectionValid &&
    configurationValid &&
    (!saveTemplate || Boolean(templateName.trim())) &&
    (!saveTemplate || reusableNetworkValid)

  const inspectTargets = useCallback(async () => {
    if (!targetSelectionValid) return false
    const targetIds = [...selected]
    setInspectionState({ status: 'loading' })
    try {
      const [preflight, inspection] = await Promise.all([
        provisioning.preflightDeploymentTargets(targetIds),
        provisioning.inspectDeploymentNetworks(targetIds),
      ])
      const issues = [...preflight.issues, ...inspection.issues]
      if (!preflight.valid || issues.length > 0) {
        setInspectionState({ status: 'attention', issues })
        return false
      }
      const suggestedMode: DeploymentNetworkMode = inspection.targets.some(
        (target) => target.suggestion.mode === 'static',
      )
        ? 'static'
        : 'automatic'
      setNetworkMode(suggestedMode)
      setDefaultGateway(
        suggestedMode === 'static' &&
          inspection.targets.some(
            (target) =>
              target.suggestion.mode === 'static' &&
              target.suggestion.defaultGateway,
          ),
      )
      setNetworkAssignments((current) => {
        const next: Record<string, NetworkAssignmentDraft> = {}
        for (const target of inspection.targets) {
          const previous = current[target.serverId]
          next[target.serverId] = {
            interfaceId: target.suggestion.interfaceId,
            subnetId: target.suggestion.subnetId,
            ipAddress:
              previous?.ipAddress ??
              (target.suggestion.mode === 'static' ? target.suggestion.ipAddress : ''),
          }
        }
        return next
      })
      setInspectionState({ status: 'ready', inspection })
      return true
    } catch (error) {
      setInspectionState({
        status: 'error',
        message: error instanceof Error ? error.message : 'Unknown error',
      })
      return false
    }
  }, [
    provisioning,
    selected,
    targetSelectionValid,
  ])

  useEffect(() => {
    if (
      context.kind !== 'fixed-targets' ||
      resolution.status !== 'ready' ||
      autoInspectionStarted.current
    ) {
      return
    }
    autoInspectionStarted.current = true
    if (context.targets.length > MAX_TARGETS) {
      setInspectionState({
        status: 'error',
        message: `Deploy OS supports at most ${MAX_TARGETS} Servers.`,
      })
      return
    }
    void inspectTargets()
  }, [context, inspectTargets, resolution.status])

  const markDirty = () => setDirty(true)

  const resetInspectionForTargetChange = () => {
    setInspectionState({ status: 'idle' })
    setNetworkAssignments({})
  }

  const toggleServer = (server: Server) => {
    if (!serverIsSelectable(server)) return
    markDirty()
    resetInspectionForTargetChange()
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(server.id)) next.delete(server.id)
      else if (next.size < MAX_TARGETS) next.add(server.id)
      return next
    })
  }

  const requestClose = () => {
    if (submitting) return
    if (dirty) {
      setConfirmDiscard(true)
      return
    }
    onClose()
  }

  const deploy = async () => {
    if (!reviewValid || !integration || submitting) return
    setSubmitting(true)
    try {
      if (saveTemplate) {
        await provisioning.createTemplate({
          integrationId: integration.id,
          name: templateName.trim(),
          imageId: effectiveImageId,
          deployTarget: effectiveDeployTarget,
          network: {
            mode: effectiveNetworkMode,
            subnetId: reusableSubnetId || undefined,
            defaultGateway: effectiveDefaultGateway,
          },
          userData: userDataMode === 'replace' ? userData : undefined,
        })
      }
      const configuration: OSDeploymentConfiguration = {
        serverIds: selectedServers.map((server) => server.id),
        customized: true,
        imageId: effectiveImageId,
        deployTarget: effectiveDeployTarget,
        userData: {
          mode: userDataMode === 'replace' ? 'replace' : 'omit',
          value: userDataMode === 'replace' ? userData : undefined,
        },
        network: {
          mode: effectiveNetworkMode,
          defaultGateway: effectiveDefaultGateway,
          assignments: selectedServers.map((server) => ({
            serverId: server.id,
            interfaceId: networkAssignments[server.id].interfaceId,
            subnetId: networkAssignments[server.id].subnetId,
            ipAddress:
              effectiveNetworkMode === 'static'
                ? networkAssignments[server.id].ipAddress.trim()
                : undefined,
          })),
        },
      }
      const response = await provisioning.createDeploymentOperation(
        toDeployServersInput(configuration),
      )
      setDirty(false)
      setUserData('')
      showToast({
        tone: 'success',
        title: 'OS deployment started',
        description: 'Server status will update automatically.',
        duration: 12_000,
        action: {
          label: 'View workflow',
          onClick: () =>
            navigate(
              siteHref(
                `/workflows/${encodeURIComponent(response.operationId)}`,
                context.siteId,
              ),
            ),
        },
      })
      onLaunched(response.operationId, configuration.serverIds)
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

  const manageIntegrations = () =>
    navigate(siteHref('/infrastructure/integrations', context.siteId))

  const issuesContent = (issues: readonly DeploymentTargetIssue[]) => (
    <Stack gap="3" aria-live="polite">
      {issues.map((issue) => (
        <Alert
          key={`${issue.serverId}:${issue.code}`}
          status="warning"
          title={`${issueServerName(issue, servers)}: Deployment blocked`}
          actions={
            <Button
              variant="plain"
              size="sm"
              onClick={() =>
                navigate(
                  siteHref(
                    `/servers/${encodeURIComponent(issue.serverId)}/network`,
                    context.siteId,
                  ),
                )
              }
            >
              Networking
            </Button>
          }
        >
          {issue.message}
        </Alert>
      ))}
    </Stack>
  )

  const targetsStep: DialogStep = {
    id: 'os-targets',
    name: 'Targets',
    canProceed: targetSelectionValid && inspectionState.status !== 'loading',
    onNext: (proceed) => {
      void inspectTargets().then((ready) => {
        if (ready) proceed()
      })
    },
    nextLabel:
      inspectionState.status === 'attention' || inspectionState.status === 'error'
        ? 'Check again'
        : undefined,
    nextLoading: inspectionState.status === 'loading',
    content: (
      <DialogWizardSection title="Deployment targets">
        <Text
          className="sw-deploy-os-dialog__step-description"
          color="fg.muted"
          fontSize="sm"
        >
          Choose up to {MAX_TARGETS} ready, unlocked Servers for this deployment.
        </Text>
        {inspectionState.status === 'attention' &&
          issuesContent(inspectionState.issues)}
        {inspectionState.status === 'error' && (
          <Alert status="warning" title="Could not inspect deployment targets">
            {inspectionState.message}
          </Alert>
        )}
        {workingSet.state.status === 'error' && (
          <Alert status="warning" title="Servers unavailable">
            {workingSet.state.message}
          </Alert>
        )}
        {workingSet.state.status === 'loading' ? (
          <LoadingState rows={5} />
        ) : (
          <div className="sw-deploy-target-picker">
            <div className="sw-deploy-target-picker__toolbar">
              <div className="sw-deploy-target-picker__search">
                <Search size={16} aria-hidden="true" />
                <Input
                  value={targetQuery}
                  size="sm"
                  aria-label="Search deployment targets"
                  placeholder="Search by name, address, or provider ID"
                  onChange={(event) => setTargetQuery(event.target.value)}
                />
              </div>
              <HStack className="sw-deploy-target-picker__selection" gap="3">
                <Text color="fg.muted" fontSize="sm">
                  <strong>{selected.size}</strong> selected · {availableServers.length} available
                </Text>
                {selected.size > 0 && (
                  <Button
                    variant="plain"
                    size="sm"
                    onClick={() => {
                      markDirty()
                      setSelected(new Set())
                      resetInspectionForTargetChange()
                    }}
                  >
                    Clear
                  </Button>
                )}
              </HStack>
            </div>
            {availableServers.length === 0 ? (
              <div className="sw-deploy-target-picker__empty">
                No ready, unlocked Servers are available for this provisioner.
              </div>
            ) : filteredAvailableServers.length === 0 ? (
              <div className="sw-deploy-target-picker__empty">
                <span>No Servers match this search.</span>
                <Button variant="plain" size="sm" onClick={() => setTargetQuery('')}>
                  Clear search
                </Button>
              </div>
            ) : (
              <div
                className="sw-deploy-target-picker__grid"
                role="group"
                aria-label="Deployment target selection"
              >
                {filteredAvailableServers.map((server) => {
                  const checked = selected.has(server.id)
                  const selectionLimitReached = selected.size >= MAX_TARGETS && !checked
                  return (
                    <Checkbox
                      key={server.id}
                      className="sw-deploy-target-card"
                      checked={checked}
                      disabled={selectionLimitReached}
                      aria-label={`Select ${serverDisplayName(server)}`}
                      inputProps={{ 'aria-label': `Select ${serverDisplayName(server)}` }}
                      onCheckedChange={() => toggleServer(server)}
                    >
                      <span className="sw-deploy-target-card__content">
                        <strong className="sw-deploy-target-card__name">
                          {serverDisplayName(server)}
                        </strong>
                        <span className="sw-deploy-target-card__facts">
                          {serverTargetFacts(server)}
                        </span>
                      </span>
                    </Checkbox>
                  )
                })}
              </div>
            )}
          </div>
        )}
      </DialogWizardSection>
    ),
  }

  const operatingSystemStep: DialogStep = {
    id: 'os-operating-system',
    name: 'OS image',
    canProceed: operatingSystemValid,
    content: (
      <OSImageSelectionStep
        images={images}
        value={effectiveImageId}
        siteId={context.siteId}
        integrationId={resolvedIntegrationId}
        loading={catalogLoading}
        error={catalogError}
        disabled={Boolean(catalogError)}
        onRefresh={() => setCatalogNonce((value) => value + 1)}
        onChange={(value) => {
          markDirty()
          setImageId(value)
          setDeployTarget(defaultDeployTargetForImage(
            images.find((image) => image.id === value),
          ))
        }}
      />
    ),
  }

  const installationStep: DialogStep = {
    id: 'os-installation',
    name: 'Installation',
    canProceed: installationValid,
    content: (
      <DialogWizardSection title="Installation">
        <Text
          className="sw-deploy-os-dialog__step-description"
          color="fg.muted"
          fontSize="sm"
        >
          Choose where the OS runs and whether this deployment supplies cloud-init data.
        </Text>
        <div className="sw-deploy-config-grid">
          <DeployTargetField
            value={effectiveDeployTarget}
            onChange={(nextTarget) => {
              markDirty()
              setDeployTarget(nextTarget)
            }}
          />
          <Field.Root required>
            <Field.Label>Cloud-init</Field.Label>
            <Select
              value={userDataMode}
              aria-label="Cloud-init mode"
              onChange={(value) => {
                markDirty()
                setUserDataMode(value as DeploymentUserDataMode)
                if (value !== 'replace') setUserData('')
              }}
              options={[
                { value: 'replace', label: 'Replace for this deployment' },
                { value: 'omit', label: 'Omit cloud-init' },
              ]}
            />
            <Field.HelperText>Cloud-init stays in memory and is never saved in the browser.</Field.HelperText>
          </Field.Root>
          {userDataMode === 'replace' && (
            <Field.Root required className="sw-deploy-config-grid__wide">
              <Field.Label>Cloud-init user data</Field.Label>
              <Textarea
                value={userData}
                aria-label="Cloud-init user data"
                rows={9}
                autoComplete="off"
                onChange={(event) => {
                  markDirty()
                  setUserData(event.target.value)
                }}
              />
            </Field.Root>
          )}
          {selectedImageModeUnsupported && (
            <UnsupportedDeployModeWarning
              imageName={selectedImage?.name || effectiveImageId}
              deployTarget={effectiveDeployTarget}
              className="sw-deploy-config-grid__wide"
            />
          )}
        </div>
      </DialogWizardSection>
    ),
  }

  const networkingStep: DialogStep = {
    id: 'os-networking',
    name: 'Networking',
    canProceed: networkingValid,
    content: (
      <DialogWizardSection title="Networking">
        <Text
          className="sw-deploy-os-dialog__step-description"
          color="fg.muted"
          fontSize="sm"
        >
          Choose Automatic or Static addressing for the inspected target NICs.
        </Text>
        <div className="sw-deploy-config-grid">
          <AddressingModeField
            value={effectiveNetworkMode}
            onChange={(mode) => {
              markDirty()
              setNetworkMode(mode)
              if (mode === 'automatic') setDefaultGateway(false)
            }}
          />
          {effectiveNetworkMode === 'static' && (
            <Checkbox
              checked={effectiveDefaultGateway}
              onCheckedChange={(checked) => {
                markDirty()
                setDefaultGateway(checked)
              }}
            >
              Use the selected subnet for the default route
            </Checkbox>
          )}
        </div>
        {!networkAssignmentsValid && (
          <Alert status="warning" title="Complete every network assignment">
            Select a NIC and subnet for each Server. Static mode also requires a unique IPv4 address per target.
          </Alert>
        )}
        {networkInspection && (
          <NetworkAssignmentsEditor
            targets={networkInspection.targets}
            serverNames={Object.fromEntries(
              selectedServers.map((server) => [server.id, serverDisplayName(server)]),
            )}
            assignments={networkAssignments}
            mode={effectiveNetworkMode}
            ariaLabel="Deployment network assignments"
            showCurrentProviderMode={false}
            onChange={(serverId, assignment) => {
              markDirty()
              setNetworkAssignments((current) => ({
                ...current,
                [serverId]: assignment,
              }))
            }}
          />
        )}
      </DialogWizardSection>
    ),
  }

  const reviewStep: DialogStep = {
    id: 'os-review',
    name: 'Review',
    canProceed: reviewValid && !submitting,
    content: (
      <DialogWizardSection title="Review deployment">
        <Card.Root size="sm">
          <Card.Body gap="3">
            <Heading size="sm">
              {selectedServers.length} Server{selectedServers.length === 1 ? '' : 's'}
            </Heading>
            <DescriptionList
              emptyText="-"
              items={[
                { label: 'Site', value: context.siteId },
                { label: 'Provisioner', value: integration?.name ?? '-' },
                { label: 'Configuration', value: 'Custom' },
                { label: 'Image', value: selectedImage?.name || effectiveImageId },
                {
                  label: 'Deploy mode',
                  value: DEPLOY_TARGET_LABELS[effectiveDeployTarget],
                },
                {
                  label: 'Cloud-init',
                  value:
                    userDataMode === 'inherit'
                      ? 'Inherit from template'
                      : userDataMode === 'replace'
                        ? 'Replace for this deployment'
                        : 'Omit',
                },
                {
                  label: 'Network mode',
                  value: effectiveNetworkMode === 'automatic' ? 'Automatic' : 'Static',
                },
              ]}
            />
          </Card.Body>
        </Card.Root>
        <StickyTableFrame>
          <Table.Root size="sm" aria-label="Deployment review targets">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Server</Table.ColumnHeader>
                <Table.ColumnHeader>Interface</Table.ColumnHeader>
                <Table.ColumnHeader>Subnet</Table.ColumnHeader>
                <Table.ColumnHeader>IP address</Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {selectedServers.map((server) => {
                const target = networkInspection?.targets.find(
                  (candidate) => candidate.serverId === server.id,
                )
                const assignment = networkAssignments[server.id]
                const iface = target?.network.interfaces.find(
                  (candidate) => candidate.id === assignment?.interfaceId,
                )
                const subnet = iface?.availableSubnets.find(
                  (candidate) => candidate.id === assignment?.subnetId,
                )
                return (
                  <Table.Row key={server.id}>
                    <Table.Cell>{serverDisplayName(server)}</Table.Cell>
                    <Table.Cell>{iface?.name ?? '-'}</Table.Cell>
                    <Table.Cell>{subnet?.cidr ?? '-'}</Table.Cell>
                    <Table.Cell className="sw-mono">
                      {effectiveNetworkMode === 'static'
                        ? assignment?.ipAddress || '-'
                        : 'Automatic'}
                    </Table.Cell>
                  </Table.Row>
                )
              })}
            </Table.Body>
          </Table.Root>
        </StickyTableFrame>
        {templatesEnabled && (
          <Card.Root size="sm">
            <Card.Body gap="3">
              <Heading size="sm">Reuse this configuration</Heading>
              <Checkbox
                checked={saveTemplate}
                onCheckedChange={(checked) => {
                  markDirty()
                  setSaveTemplate(checked)
                }}
              >
                Save as a deployment template
              </Checkbox>
              {saveTemplate && (
                <Input
                  aria-label="New deployment template name"
                  value={templateName}
                  placeholder="Template name"
                  onChange={(event) => {
                    markDirty()
                    setTemplateName(event.target.value)
                  }}
                />
              )}
              {saveTemplate && !reusableNetworkValid && (
                <Alert status="warning" title="Static templates require one shared subnet">
                  Choose the same subnet for every target before saving this configuration.
                </Alert>
              )}
            </Card.Body>
          </Card.Root>
        )}
        <Alert status="warning" title="Deployment starts immediately after preflight">
          Accepted provider requests cannot be rolled back as a batch.
        </Alert>
      </DialogWizardSection>
    ),
  }

  const steps: DialogStep[] =
    context.kind === 'fixed-image'
      ? [targetsStep, installationStep, networkingStep, reviewStep]
      : [operatingSystemStep, installationStep, networkingStep, reviewStep]
  const currentStepIndex = Math.max(
    0,
    steps.findIndex((step) => step.id === currentStepId),
  )
  const currentStep = steps[currentStepIndex]
  const stepCount = steps.length
  const flowReady =
    resolution.status === 'ready' &&
    (context.kind === 'fixed-image' || inspectionState.status === 'ready')

  useEffect(() => {
    if (!flowReady) return
    const frame = window.requestAnimationFrame(() => {
      stepStatusRef.current?.focus({ preventScroll: true })
    })
    return () => window.cancelAnimationFrame(frame)
  }, [flowReady])

  useEffect(() => {
    if (previousStepIdRef.current === currentStepId) return
    previousStepIdRef.current = currentStepId
    if (scrollViewportRef.current) scrollViewportRef.current.scrollTop = 0
    const frame = window.requestAnimationFrame(() => {
      const progress = progressRef.current
      const activeStep = progress?.querySelector<HTMLElement>('[aria-current="step"]')
      if (progress && activeStep) {
        const inset = 8
        const activeStart = activeStep.offsetLeft
        const activeEnd = activeStart + activeStep.offsetWidth
        const visibleStart = progress.scrollLeft + inset
        const visibleEnd = progress.scrollLeft + progress.clientWidth - inset
        if (activeStart < visibleStart) {
          progress.scrollLeft = Math.max(0, activeStart - inset)
        } else if (activeEnd > visibleEnd) {
          progress.scrollLeft = activeEnd - progress.clientWidth + inset
        }
      }
      stepStatusRef.current?.focus({ preventScroll: true })
    })
    return () => window.cancelAnimationFrame(frame)
  }, [currentStepId])

  const goBack = () => {
    const previous = steps[currentStepIndex - 1]
    if (previous) setCurrentStepId(previous.id)
  }

  const goForward = () => {
    const next = steps[currentStepIndex + 1]
    if (!next) return
    const proceed = () => setCurrentStepId(next.id)
    if (currentStep.onNext) currentStep.onNext(proceed)
    else proceed()
  }

  let content: ReactNode
  if (resolution.status === 'loading') {
    content = <LoadingState rows={5} />
  } else if (resolution.status === 'error') {
    content = (
      <Alert
        status="warning"
        title={resolution.title}
        actions={
          <HStack>
            <Button variant="outline" size="sm" onClick={manageIntegrations}>
              Manage integrations
            </Button>
            {resourceState.status === 'error' && (
              <Button
                variant="plain"
                size="sm"
                onClick={() => setResourceNonce((value) => value + 1)}
              >
                Retry
              </Button>
            )}
          </HStack>
        }
      >
        {resolution.message}
      </Alert>
    )
  } else if (
    context.kind === 'fixed-targets' &&
    inspectionState.status !== 'ready'
  ) {
    content = (
      <Stack gap="4">
        <Card.Root size="sm">
          <Card.Body>
            <Heading size="sm">
              {context.targets.length} fixed target{context.targets.length === 1 ? '' : 's'}
            </Heading>
            <Text color="fg.muted">
              Checking live deployment readiness and network configuration.
            </Text>
          </Card.Body>
        </Card.Root>
        {inspectionState.status === 'loading' || inspectionState.status === 'idle' ? (
          <LoadingState rows={4} />
        ) : inspectionState.status === 'attention' ? (
          <>
            {issuesContent(inspectionState.issues)}
            <Button alignSelf="flex-start" onClick={() => void inspectTargets()}>
              Check again
            </Button>
          </>
        ) : (
          <Alert
            status="warning"
            title="Could not inspect deployment targets"
            actions={
              <Button
                variant="outline"
                size="sm"
                onClick={() => void inspectTargets()}
              >
                Retry
              </Button>
            }
          >
            {inspectionState.message}
          </Alert>
        )}
      </Stack>
    )
  } else {
    content = currentStep.content
  }

  const footer = flowReady ? (
    <div className="sw-deploy-os-dialog__footer-actions">
      <Button variant="ghost" disabled={submitting} onClick={requestClose}>
        Cancel
      </Button>
      <HStack gap="2">
        {currentStepIndex > 0 && (
          <Button variant="outline" disabled={submitting} onClick={goBack}>
            Back
          </Button>
        )}
        {currentStepIndex === stepCount - 1 ? (
          <Button
            colorPalette="brand"
            loading={submitting}
            disabled={!currentStep.canProceed || submitting}
            onClick={() => void deploy()}
          >
            Deploy OS
          </Button>
        ) : (
          <Button
            colorPalette="brand"
            loading={currentStep.nextLoading}
            disabled={!currentStep.canProceed || Boolean(currentStep.nextLoading)}
            onClick={goForward}
          >
            {currentStep.nextLabel ?? 'Next'}
          </Button>
        )}
      </HStack>
    </div>
  ) : (
    <div className="sw-deploy-os-dialog__footer-actions sw-deploy-os-dialog__footer-actions--end">
      <Button variant="outline" disabled={submitting} onClick={requestClose}>
        Close
      </Button>
    </div>
  )

  return (
    <>
      <Modal
        open
        onClose={requestClose}
        closeOnInteractOutside={!submitting}
        dismissDisabled={submitting}
        size="xl"
        title="Deploy OS"
        description={
          context.kind === 'fixed-image'
            ? `Choose Servers for ${context.image.name || context.image.id}. The image is fixed from the catalog.`
            : 'Configure the selected Servers without leaving this page.'
        }
        footer={footer}
        contentClassName="sw-deploy-os-dialog-modal"
      >
        <div className="sw-deploy-os-dialog">
          {flowReady && (
            <nav ref={progressRef} className="sw-deploy-os-dialog__progress" aria-label="Deployment progress">
              <ol>
                {steps.map((step, index) => {
                  const state =
                    index < currentStepIndex
                      ? 'complete'
                      : index === currentStepIndex
                        ? 'current'
                        : 'upcoming'
                  return (
                    <li
                      key={step.id}
                      data-state={state}
                      aria-current={state === 'current' ? 'step' : undefined}
                    >
                      <span className="sw-deploy-os-dialog__step-number" aria-hidden="true">
                        {state === 'complete' ? '✓' : index + 1}
                      </span>
                      <span>{step.name}</span>
                    </li>
                  )
                })}
              </ol>
            </nav>
          )}
          <div
            ref={scrollViewportRef}
            className="sw-deploy-os-dialog__viewport"
            data-testid="deploy-os-dialog-viewport"
          >
            {flowReady && (
              <div
                ref={stepStatusRef}
                className="sw-deploy-os-dialog__step-status"
                aria-live="polite"
                tabIndex={-1}
              >
                <span>Step {currentStepIndex + 1} of {stepCount}</span>
                <strong>{currentStep.name}</strong>
              </div>
            )}
            {content}
          </div>
        </div>
      </Modal>
      <ConfirmDialog
        open={confirmDiscard}
        title="Discard deployment draft?"
        confirmLabel="Discard draft"
        onCancel={() => setConfirmDiscard(false)}
        onConfirm={() => {
          setConfirmDiscard(false)
          setDirty(false)
          onClose()
        }}
      >
        Your OS deployment changes will be lost. Cloud-init content has not been saved.
      </ConfirmDialog>
    </>
  )
}
