import { Fragment, useEffect, useState, type ReactNode } from 'react'
import { Badge, Button, Card, Field, Heading, HStack, Input, Table, Text, Textarea } from '@chakra-ui/react'
import { Search } from 'lucide-react'
import { Link as RouterLink, useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { validIPv4Address } from '@/domain/network/ipv4'
import { platformLifecycleLabel } from '@/domain/platform/lifecycle'
import { formatGiB } from '@/domain/platform/resourceEligibility'
import type { Platform, PlatformMachinePreparation, PlatformType } from '@/domain/platform/types'
import type { DeploymentNetworkMode, DeploymentTemplate, DeployTarget, NetworkInspectionResult } from '@/domain/provisioning/types'
import type { Integration, OSImage } from '@/domain/site/types'
import { serverGPUsByKind } from '@/domain/server/gpu'
import { serverDisplayName, serverPrimaryAddress, type Server } from '@/domain/server/types'
import {
  platformDeployFeatures,
  platformModuleUI,
  type PlatformDeployFeature,
  type PlatformDeployFeatureContext,
  type PlatformDeployFeatureContribution,
  type PlatformModuleUI,
} from '@/modules/registry'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { FailureMessage } from '@/presentation/components/FailureMessage'
import { changePlanWaitDescription } from '@/presentation/components/changePlanWait'
import { failureOf, failureText, type DisplayedFailure } from '@/presentation/components/failure'
import { LoadingState } from '@/presentation/components/LoadingState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { SectionHeader, StickyTableFrame } from '@/presentation/components/ConsolePrimitives'
import { ResponsiveDataView, ResourceCard, ResourceCardField } from '@/presentation/components/ResponsiveDataView'
import { gpuInventoryProfile } from '@/presentation/components/serverGpuPresentation'
import { Wizard, type WizardStepDef } from '@/presentation/components/Wizard'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { DEPLOYMENT_NETWORK_MODE_LABELS, deployTargetForEphemeral } from '@/domain/provisioning/types'
import { AddressingModeField } from '@/presentation/pages/provisioning/AddressingModeField'
import { NetworkAssignmentsEditor } from '@/presentation/pages/provisioning/NetworkAssignmentsEditor'
import { DeployTargetField } from '@/presentation/pages/provisioning/DeployTargetField'
import { OSImageSelectionStep } from '@/presentation/pages/provisioning/OSImageSelectionStep'
import { SiteDhcpAddressingWarning } from '@/presentation/pages/provisioning/SiteDhcpAddressingWarning'
import { UnsupportedDeployModeWarning } from '@/presentation/pages/provisioning/UnsupportedDeployModeWarning'
import {
  defaultDeployTargetForImage,
  osImageSupportsDeployTarget,
  siteDhcpConflictSubnets,
  suggestedBatchNetworkMode,
  toPlatformMachinePreparation,
  type OSDeploymentConfiguration,
} from '@/presentation/pages/provisioning/osDeploymentFlow'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import { Select } from '@/presentation/components/ui/select'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useExperimentalFeature } from '@/presentation/contexts/ExperimentalFeaturesContext'
import { formatQuantity } from '@/shared/utils/quantity'
import { useDeployableServers } from './useDeployableServers'
import { AdvancedSettings } from './AdvancedSettings'
import { TimeSyncCheckToggle } from './TimeSyncCheckToggle'
import './platformDeployment.css'

// A platform deploy converges over both pool-ready and already-deployed Servers (decision 072):
// ready Servers are provisioned to an OS first, deployed Servers are reused as-is. The wizard
// therefore offers both and derives the machine-preparation mode from the selection instead of
// asking the user to pick it up front. Kept module-level so its reference is stable for the
// candidate-loading effect.
const CANDIDATE_PROVISIONING_STATES = ['ready', 'deployed'] as const

/** Matches the Server identity fields exposed by the deployment target picker. */
function serverMatchesDeployQuery(server: Server, query: string): boolean {
  const needle = query.trim().toLowerCase()
  if (!needle) return true
  return [serverDisplayName(server), server.status.fqdn ?? '', server.id, ...server.status.addresses, ...server.status.tags]
    .some((value) => value.toLowerCase().includes(needle))
}

interface ExistingPlatformAssignment {
  platformName: string
  platformType: string
  detail: string
}

/**
 * Maps the Server's Platform Claim and observed membership into one candidate-table
 * explanation. The claim takes precedence because it also covers partial deployments whose
 * membership has not become observable yet. A claim whose Platform is not in the Site list still
 * blocks the Server, named by its Platform id.
 */
function existingPlatformAssignment(server: Server, platforms: Platform[]): ExistingPlatformAssignment | null {
  const claim = server.status.platformClaim
  if (claim) {
    const platform = platforms.find((candidate) => candidate.id === claim.platformId)
    const role = server.status.membership?.platformId === claim.platformId ? server.status.membership.role : null
    const lifecycle = platform ? platformLifecycleLabel(platform.lifecycleState) : 'Claimed'
    return {
      platformName: platform?.name ?? claim.platformId,
      platformType: platform?.type ?? 'unknown',
      detail: role ? `${role} · ${lifecycle}` : lifecycle,
    }
  }
  if (!server.status.membership) return null
  const platform = platforms.find((candidate) => candidate.id === server.status.membership?.platformId)
  return {
    platformName: platform?.name ?? server.status.membership.platformId,
    platformType: platform?.type ?? 'unknown',
    detail: server.status.membership.role,
  }
}

/**
 * One-line hardware facts for a deploy candidate, shared by the table and the mobile cards. Storage
 * is rounded like the Servers page (`formatQuantity`), and only compute GPUs are listed: GPU Inventory
 * also holds display controllers (for example a VM's virtual display), which no Platform schedules.
 * A fact the provider has not reported (zero) is left out rather than shown as 0, and a Server with
 * none reads "Not reported", as on the Servers page.
 */
function candidateHardwareSummary(server: Server): string {
  const { cpuCores, memoryMiB, storageGB } = server.status
  const computeGPUs = serverGPUsByKind(server, 'compute')
  const facts = [
    ...(cpuCores > 0 ? [`${cpuCores} cores`] : []),
    ...(memoryMiB > 0 ? [`${formatGiB(memoryMiB)} GiB`] : []),
    ...(storageGB > 0 ? [formatQuantity(storageGB, 'GB')] : []),
    ...(computeGPUs.length > 0 ? [gpuInventoryProfile(computeGPUs)] : []),
  ]
  return facts.length > 0 ? facts.join(' · ') : 'Not reported'
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
interface ReviewGroupProps {
  title: string
  items: Array<{ label: string; value: ReactNode }>
  onEdit: () => void
}

/**
 * One review section with a direct return path to its owning deployment step. Every card has an
 * Edit button, so each is named after its card (`Edit Platform`, `Edit Monitoring`) to stay
 * distinguishable in a screen reader's list of buttons.
 */
function ReviewGroup({ title, items, onEdit }: ReviewGroupProps) {
  return (
    <Card.Root size="sm" className="sw-platform-review-card">
      <Card.Body gap="3">
        <HStack justify="space-between" align="center" gap="3">
          <Heading as="h3" size="sm">{title}</Heading>
          <Button size="xs" variant="plain" colorPalette="brand" aria-label={`Edit ${title}`} onClick={onEdit}>Edit</Button>
        </HStack>
        <DescriptionList items={items} />
      </Card.Body>
    </Card.Root>
  )
}

/**
 * Calls every deploy feature hook another module contributes to this Platform type. The list is
 * fixed per type by the static registry, so the hooks run in the same order on every render.
 */
function useDeployFeatures(contributions: PlatformDeployFeatureContribution[], context: PlatformDeployFeatureContext): PlatformDeployFeature[] {
  return contributions.map((contribution) => contribution.useFeature(context))
}

interface DeployPlatformWizardPageProps {
  platformType: PlatformType
  module: PlatformModuleUI
}

/**
 * Platform deployment workflow: a type-agnostic shell around the Platform module's deploy form.
 *
 * The shell owns the Site, the name, the candidate Servers, OS preparation for the Servers that
 * still need an operating system, the clock check, and submission; the module owns its settings,
 * roles, features, and any steps only it has. Submitting is the user's confirmation: the install's
 * Change Plan is approved with the request and the page opens the new Platform, or, when the
 * Change Plan policy or a blocker keeps the plan waiting, opens Change Plans and says why. While Deployment Templates are
 * in development (always, in release builds) the OS step offers only a custom configuration and
 * never lists templates.
 */
function DeployPlatformWizardPage({ platformType, module }: DeployPlatformWizardPageProps) {
  const navigate = useNavigate()
  const { platforms, provisioning, sites: siteRepository } = useApp()
  const { siteId: scopedSiteId, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [siteId, setSiteId] = useState<string | undefined>(scopedSiteId)
  const [name, setName] = useState('')
  // The time-sync check runs on every deployment (decision 065); this only decides whether a node
  // with an unsynchronized clock fails it. Strict by default, as the backend is.
  const [timeSyncEnforced, setTimeSyncEnforced] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [serverQuery, setServerQuery] = useState('')
  const [wizardStep, setWizardStep] = useState(0)
  const [candidateRevision, setCandidateRevision] = useState(0)
  const templatesEnabled = useExperimentalFeature('deploymentTemplates')
  const [templates, setTemplates] = useState<DeploymentTemplate[]>([])
  const [templateId, setTemplateId] = useState('')
  const [images, setImages] = useState<OSImage[]>([])
  const [imageId, setImageId] = useState('')
  const [deployTarget, setDeployTarget] = useState<DeployTarget>('ram')
  const [cloudInit, setCloudInit] = useState('')
  const [networkMode, setNetworkMode] = useState<DeploymentNetworkMode>('automatic')
  const [defaultGateway, setDefaultGateway] = useState(false)
  const [networkInspection, setNetworkInspection] = useState<NetworkInspectionResult | null>(null)
  const [networkAssignments, setNetworkAssignments] = useState<Record<string, { interfaceId: string; subnetId: string; ipAddress: string }>>({})
  const [provisioningLoading, setProvisioningLoading] = useState(false)
  const [provisioningError, setProvisioningError] = useState<DisplayedFailure | null>(null)
  const [siteProvisionerState, setSiteProvisionerState] = useState<{
    siteKey: string | null
    items: Integration[]
    error: DisplayedFailure | null
  }>({ siteKey: null, items: [], error: null })
  const effectiveSiteId = siteId ?? scopedSiteId
  const state = useDeployableServers(effectiveSiteId, CANDIDATE_PROVISIONING_STATES, candidateRevision)
  const candidates = state.status === 'ready' ? state.data.servers : []
  const sitePlatforms = state.status === 'ready' ? state.data.platforms : []
  const heldBy = (server: Server) => {
    const existing = existingPlatformAssignment(server, sitePlatforms)
    return existing ? 'assigned to ' + existing.platformName : null
  }
  const form = module.deploy.useForm({ platformName: name, servers: candidates, heldBy })
  const features = useDeployFeatures(platformDeployFeatures(platformType), form.featureContext)


  useEffect(() => {
    if (!effectiveSiteId) return
    let canceled = false
    siteRepository
      .listIntegrations({ siteId: effectiveSiteId, kind: 'provisioner' })
      .then((integrations) => {
        if (!canceled) {
          setSiteProvisionerState({
            siteKey: effectiveSiteId,
            items: integrations,
            error: null,
          })
        }
      })
      .catch((error: Error) => {
        if (!canceled) {
          setSiteProvisionerState({
            siteKey: effectiveSiteId,
            items: [],
            error: failureOf(error, 'The Site provisioner could not be read.'),
          })
        }
      })
    return () => {
      canceled = true
    }
  }, [effectiveSiteId, siteRepository])


  // Deployment templates are per-Site; the OS step filters them to the derived provisioner.
  useEffect(() => {
    if (!effectiveSiteId || !templatesEnabled) return
    let canceled = false
    provisioning
      .listTemplates({ siteId: effectiveSiteId })
      .then((nextTemplates) => {
        if (!canceled) setTemplates(nextTemplates)
      })
      .catch((error: Error) => {
        if (!canceled) setProvisioningError(failureOf(error, 'Deployment templates could not be loaded.'))
      })
    return () => {
      canceled = true
    }
  }, [effectiveSiteId, provisioning, templatesEnabled])

  const visibleServers = state.status === 'ready'
    ? state.data.servers.filter((server) => serverMatchesDeployQuery(server, serverQuery))
    : []
  const basicsValid = Boolean(effectiveSiteId && name.trim() && form.basicsValid)
  // Locked Servers are excluded from the candidate list (see useDeployableServers), so the only
  // "unavailable" reason left to surface is an existing Platform assignment.
  const hasAssignedServers = candidates.some((server) => Boolean(heldBy(server)))
  const invalidFeature = features.find((feature) => !feature.valid)
  const featuresValid = form.features.valid && !invalidFeature
  const timeSyncReview = timeSyncEnforced
    ? 'Required: an unsynchronized node fails the deployment'
    : 'Warning only: an unsynchronized node is reported and the deployment continues'

  const selectedServerIds = form.selectedServerIds
  const selectedServers = selectedServerIds.flatMap((serverId) => {
    const server = candidates.find((candidate) => candidate.id === serverId)
    return server ? [server] : []
  })

  // Convergent selection (decision 072): a Server already `deployed` is reused as-is; anything else
  // (pool-ready) is provisioned first. The machine-preparation mode is derived from this.
  const deployedSelectedServers = selectedServers.filter((server) => server.status.provisioning?.state === 'deployed')
  const readySelectedServers = selectedServers.filter((server) => server.status.provisioning?.state !== 'deployed')
  const needsProvisioning = readySelectedServers.length > 0
  const siteProvisioners = siteProvisionerState.siteKey === effectiveSiteId
    ? siteProvisionerState.items
    : null
  const siteProvisionerError = siteProvisionerState.siteKey === effectiveSiteId ? siteProvisionerState.error : null
  const siteProvisioner =
    siteProvisioners?.length === 1 ? siteProvisioners[0] : undefined
  const provisionerSourceMismatch = Boolean(
    siteProvisioner &&
      readySelectedServers.some(
        (server) =>
          server.status.source.siteId !== effectiveSiteId ||
          server.status.source.integrationId !== siteProvisioner.id,
      ),
  )
  const siteProvisionerIssue = !needsProvisioning
    ? null
    : siteProvisioners === null
      ? {
          title: 'Resolving Site provisioner',
          message: 'Checking the Site configuration before OS preparation.',
        }
      : siteProvisionerError
        ? {
            title: 'Provisioner configuration unavailable',
            ...siteProvisionerError,
          }
        : siteProvisioners.length === 0
          ? {
              title: 'Connect a provisioner',
              message: 'This Site has no provisioner Integration.',
            }
          : siteProvisioners.length > 1
            ? {
                title: 'Site configuration conflict',
                message: 'Platform OS preparation requires exactly one provisioner for this Site.',
              }
            : !siteProvisioner?.enabled
              ? {
                  title: 'Enable the Site provisioner',
                  message: `${siteProvisioner?.name ?? 'The provisioner'} is disabled.`,
                }
              : provisionerSourceMismatch
                ? {
                    title: 'Inconsistent deployment source',
                    message: 'A selected ready Server does not belong to the Site provisioner.',
                  }
                : null
  const provisionerConflict = Boolean(siteProvisionerIssue)
  const integrationId =
    needsProvisioning && !siteProvisionerIssue ? siteProvisioner?.id ?? '' : ''
  const machinesValid =
    form.machines.valid &&
    (!needsProvisioning || (!provisionerConflict && Boolean(integrationId)))

  // OS images are per-provisioner; (re)load them whenever the derived provisioner changes.
  useEffect(() => {
    if (!integrationId) return
    let canceled = false
    provisioning
      .listOSImages(integrationId)
      .then((nextImages) => {
        if (!canceled) setImages(nextImages)
      })
      .catch((error: Error) => {
        if (!canceled) setProvisioningError(failureOf(error, 'OS images could not be loaded.'))
      })
      .finally(() => {
        if (!canceled) setProvisioningLoading(false)
      })
    return () => {
      canceled = true
    }
  }, [integrationId, provisioning])

  // Only the pool-ready Servers need OS + network preparation; deployed Servers are reused.
  const preparationTargetKey = readySelectedServers.map((server) => server.id).sort().join(',')
  useEffect(() => {
    if (!needsProvisioning || !preparationTargetKey || !integrationId) return
    let canceled = false
    provisioning
      .inspectDeploymentNetworks(preparationTargetKey.split(','))
      .then((inspection) => {
        if (canceled) return
        setNetworkInspection(inspection)
        const nextAssignments: Record<string, { interfaceId: string; subnetId: string; ipAddress: string }> = {}
        for (const target of inspection.targets) {
          nextAssignments[target.serverId] = {
            interfaceId: target.suggestion.interfaceId,
            subnetId: target.suggestion.subnetId,
            ipAddress: target.suggestion.ipAddress,
          }
        }
        setNetworkAssignments(nextAssignments)
        setNetworkMode(suggestedBatchNetworkMode(inspection.targets.map((target) => target.suggestion.mode)))
        setDefaultGateway(inspection.targets.some((target) => target.suggestion.defaultGateway))
      })
      .catch((error: Error) => {
        if (!canceled) setProvisioningError(failureOf(error, 'Deployment networks could not be inspected.'))
      })
      .finally(() => {
        if (!canceled) setProvisioningLoading(false)
      })
    return () => {
      canceled = true
    }
  }, [integrationId, needsProvisioning, preparationTargetKey, provisioning])

  // Ignore a template left selected from a previously derived provisioner so it can never leak
  // another provisioner's image/network into this deploy, or one selected before templates
  // were hidden.
  const selectedTemplate = templatesEnabled
    ? templates.find((template) => template.id === templateId && template.integrationId === integrationId)
    : undefined
  const effectiveImageId = selectedTemplate?.imageId ?? imageId
  const effectiveDeployTarget = selectedTemplate
    ? deployTargetForEphemeral(selectedTemplate.ephemeral)
    : deployTarget
  const selectedImage = images.find((image) => image.id === effectiveImageId)
  const selectedImageModeUnsupported = Boolean(
    selectedImage && !osImageSupportsDeployTarget(selectedImage, effectiveDeployTarget),
  )
  const effectiveEphemeral = effectiveDeployTarget === 'ram'
  // Review the same resolved mode that deployment submits, including a template-owned setting.
  const osDeploymentModeLabel = effectiveEphemeral ? 'Ephemeral (memory-backed)' : 'Persistent (disk-backed)'
  const effectiveNetworkMode = selectedTemplate?.network.mode ?? networkMode
  const inspectedTargetKey = networkInspection?.targets.map((target) => target.serverId).sort().join(',') ?? ''
  // Automatic stays preselected on subnets without provisioner DHCP (decision 058); the warning
  // follows the resolved mode, a template's included, and the subnets currently assigned. An
  // inspection of a previous selection is ignored until the current one arrives.
  const siteDhcpSubnets = networkInspection && inspectedTargetKey === preparationTargetKey
    ? siteDhcpConflictSubnets(effectiveNetworkMode, networkInspection.targets, networkAssignments)
    : []
  const networkAssignmentsValid =
    !needsProvisioning ||
    (networkInspection !== null &&
      inspectedTargetKey === preparationTargetKey &&
      readySelectedServers.every((server) => {
        const assignment = networkAssignments[server.id]
        const target = networkInspection.targets.find((item) => item.serverId === server.id)
        const selectedInterface = target?.network.interfaces.find((item) => item.id === assignment?.interfaceId)
        if (!assignment?.interfaceId || !assignment.subnetId || !selectedInterface?.availableSubnets.some((subnet) => subnet.id === assignment.subnetId)) return false
        return effectiveNetworkMode !== 'static' || validIPv4Address(assignment.ipAddress)
      }))
  // The static address of each provisioned Server, trimmed as deploy() submits it, so the module's
  // steps after OS preparation and its review show the address the deploy will use. Only valid
  // addresses are passed (modules/types.ts DeployStaticAddresses).
  const staticAddresses: Record<string, string> = {}
  if (effectiveNetworkMode === 'static') {
    for (const server of readySelectedServers) {
      const ipAddress = networkAssignments[server.id]?.ipAddress.trim() ?? ''
      if (validIPv4Address(ipAddress)) staticAddresses[server.id] = ipAddress
    }
  }
  const extraSteps = form.extraSteps(selectedServers, staticAddresses)
  const extraStepsValid = extraSteps.every((step) => step.canProceed)
  const osImageValid =
    !needsProvisioning ||
    Boolean(integrationId && effectiveImageId && !provisioningLoading && !provisioningError)
  const osInstallationValid = !needsProvisioning || Boolean(effectiveImageId)
  const osNetworkingValid =
    !needsProvisioning ||
    Boolean(integrationId && networkAssignmentsValid && !provisioningError)
  const osConfigurationValid =
    osImageValid && osInstallationValid && osNetworkingValid
  const provisioningErrorText = provisioningError ? failureText(provisioningError) : ''

  const selectTemplate = (nextTemplateId: string) => {
    setTemplateId(nextTemplateId)
    const template = templates.find((item) => item.id === nextTemplateId)
    if (!template) return
    setImageId(template.imageId)
    setDeployTarget(deployTargetForEphemeral(template.ephemeral))
    setNetworkMode(template.network.mode)
    setDefaultGateway(template.network.defaultGateway)
  }

  const deploy = async () => {
    if (!effectiveSiteId || !basicsValid || !machinesValid || !featuresValid || !osConfigurationValid || !extraStepsValid || submitting) return
    setSubmitting(true)
    // The mode is derived from the selection, not chosen: reuse existing-OS Servers when nothing
    // needs provisioning, otherwise converge — provision the ready Servers and reuse deployed.
    const osConfiguration: OSDeploymentConfiguration = {
      serverIds: readySelectedServers.map((server) => server.id),
      templateId: selectedTemplate?.id,
      customized: !selectedTemplate,
      imageId: effectiveImageId,
      deployTarget: effectiveDeployTarget,
      userData: selectedTemplate
        ? { mode: 'inherit' }
        : cloudInit.trim()
          ? { mode: 'replace', value: cloudInit }
          : { mode: 'omit' },
      network: {
        mode: effectiveNetworkMode,
        subnetId: selectedTemplate?.network.subnetId,
        defaultGateway: selectedTemplate?.network.defaultGateway ?? defaultGateway,
        assignments: readySelectedServers.map((server) => ({
          serverId: server.id,
          interfaceId: networkAssignments[server.id]?.interfaceId ?? '',
          subnetId: networkAssignments[server.id]?.subnetId,
          ipAddress:
            effectiveNetworkMode === 'static'
              ? networkAssignments[server.id]?.ipAddress.trim()
              : undefined,
        })),
      },
    }
    const machinePreparation: PlatformMachinePreparation = !needsProvisioning
      ? { mode: 'existing_os' }
      : toPlatformMachinePreparation(osConfiguration)
    try {
      const created = await platforms.createPlatform({
        siteId: effectiveSiteId,
        name: name.trim(),
        type: platformType,
        ...form.input(),
        ...Object.assign({}, ...features.map((feature) => feature.input)),
        machinePreparation,
        timeSyncCheck: timeSyncEnforced ? 'enforce' : 'warn',
      })
      // Submitting the wizard is the user's confirmation, so the install is approved with the
      // request and starts at once; it waits in Change Plans only when the Change Plan policy or a
      // blocker says so, and then the toast says why.
      if (created.awaitingApproval) {
        showToast({
          title: 'Platform install waits for approval',
          description: changePlanWaitDescription(created, 'build the platform'),
          tone: 'warning',
        })
        navigate(scopedHref('/change-plans'))
        return
      }
      showToast({
        title: 'Platform install started',
        description: `Swallow is building ${name.trim()}. Its page shows the deployment as it runs.`,
        tone: 'success',
      })
      navigate(scopedHref(`/platforms/${encodeURIComponent(created.platformId)}/overview`), { state: { platformTransition: 'install' } })
    } catch (error) {
      form.refresh?.()
      setCandidateRevision((current) => current + 1)
      showToast({ title: 'Deployment failed', description: failureText(failureOf(error, 'Could not start the deployment.')), tone: 'error' })
      setSubmitting(false)
    }
  }

  const steps: WizardStepDef[] = []
  const editStep = (stepId: string) => {
    const index = steps.findIndex((step) => step.id === stepId)
    if (index >= 0) setWizardStep(index)
  }

  if (state.status === 'ready') {
    steps.push({
      id: 'deploy-basics',
      name: 'Platform',
      canProceed: basicsValid,
      blockedReason: !effectiveSiteId ? 'Choose a Site.' : !name.trim() ? 'Enter a Platform name.' : form.basicsBlockedReason,
      content: (
        <WizardSection title="Platform identity">
          <div className="sw-form-grid">
            <Field.Root required>
              <Field.Label>Site</Field.Label>
              <Select
                id="platform-site"
                aria-label="Site"
                value={effectiveSiteId ?? ''}
                placeholder="Select a Site"
                options={state.data.sites.map((site) => ({ value: site.id, label: site.name }))}
                required
                onChange={(value) => {
                  if (value === effectiveSiteId) return
                  if (selectedServerIds.length > 0 &&
                    !window.confirm('Changing Site clears the selected Servers and Site-specific deployment settings. Continue?')) {
                    return
                  }
                  setSiteId(value)
                  setWizardStep(0)
                  form.reset()
                  for (const feature of features) feature.reset()
                  setTemplateId('')
                  setImageId('')
                  setNetworkInspection(null)
                  setDeployTarget('ram')
                  setCloudInit('')
                  setNetworkMode('automatic')
                  setDefaultGateway(false)
                  setServerQuery('')
                  setNetworkAssignments({})
                }}
              />
            </Field.Root>
            <Field.Root required>
              <Field.Label>Platform name</Field.Label>
              <Input value={name} onChange={(event) => setName(event.target.value)} placeholder={module.deploy.namePlaceholder} />
            </Field.Root>
            {form.basicsFields}
          </div>
          {form.basicsExtras}
        </WizardSection>
      ),
    })

    const { eligibility, roleHeaders, roleCells } = form.machines
    steps.push({
      id: 'deploy-machines',
      name: form.machines.name,
      canProceed: machinesValid,
      blockedReason: form.machines.blockedReason,
      content: (
        <WizardSection title={form.machines.title}>
          {form.machines.intro}
          <HStack justify="space-between" gap="3" wrap="wrap">
            <HStack className="sw-platform-server-search" gap="2"><Search size={16} aria-hidden="true" /><Input value={serverQuery} onChange={(event) => setServerQuery(event.target.value)} aria-label="Search Servers" placeholder="Search hostname, address, or tag" /></HStack>
            <Text color="fg.muted" fontSize="sm">Showing {visibleServers.length} of {state.data.servers.length} Servers</Text>
          </HStack>

          {hasAssignedServers && (
            <Alert status="warning" title="Some Servers are already assigned">
              Assigned Servers remain visible for context. Remove the existing Platform assignment before selecting a role.
            </Alert>
          )}
          {siteProvisionerIssue && (
            <Alert
              status="warning"
              title={siteProvisionerIssue.title}
              actions={
                effectiveSiteId ? (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() =>
                      navigate(`/infrastructure/integrations?site=${encodeURIComponent(effectiveSiteId)}`)
                    }
                  >
                    Manage integrations
                  </Button>
                ) : undefined
              }
            >
              <FailureMessage failure={siteProvisionerIssue} />
            </Alert>
          )}
          {state.data.servers.length === 0 ? (
            <EmptyState title="No deployable Servers" message="This Site has no Ready or Deployed Servers to build a Platform from." />
          ) : visibleServers.length === 0 ? (
            <EmptyState title="No matching Servers" message="No deployable Server matches this search." />
          ) : (
            <ResponsiveDataView
              desktop={(
                <StickyTableFrame>
              <Table.Root size="sm" aria-label="Deployable Servers" className="sw-deploy-machines-table">
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeader>Server</Table.ColumnHeader>
                    <Table.ColumnHeader>Address</Table.ColumnHeader>
                    <Table.ColumnHeader>OS state</Table.ColumnHeader>
                    <Table.ColumnHeader>Current assignment</Table.ColumnHeader>
                    <Table.ColumnHeader>Hardware</Table.ColumnHeader>
                    {eligibility && <Table.ColumnHeader>Eligibility</Table.ColumnHeader>}
                    {roleHeaders.map((header) => <Table.ColumnHeader key={header}>{header}</Table.ColumnHeader>)}
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {visibleServers.map((server) => {
                    const existing = existingPlatformAssignment(server, state.data.platforms)
                    const deployed = server.status.provisioning?.state === 'deployed'
                    const serverEligibility = eligibility?.(server)
                    const unavailable = Boolean(existing) || Boolean(eligibility && !serverEligibility?.eligible)
                    const willProvision = !unavailable && selectedServerIds.includes(server.id)
                    return (
                      <Table.Row key={server.id}>
                        <Table.Cell>
                          <strong>{serverDisplayName(server)}</strong>
                        </Table.Cell>
                        <Table.Cell className="sw-mono">{serverPrimaryAddress(server) ?? '-'}</Table.Cell>
                        <Table.Cell>
                          {deployed ? (
                            <Badge colorPalette="green" variant="subtle">Deployed</Badge>
                          ) : willProvision ? (
                            <Badge colorPalette="blue" variant="subtle">Will provision</Badge>
                          ) : (
                            <Badge colorPalette="blue" variant="subtle">Ready</Badge>
                          )}
                        </Table.Cell>
                        <Table.Cell>
                          {existing ? (
                            <span className="sw-cell-inline">
                              <Badge colorPalette="red" variant="subtle">In use</Badge>
                              <span>
                                {existing.platformName} ({existing.platformType}, {existing.detail})
                              </span>
                            </span>
                          ) : (
                            '-'
                          )}
                        </Table.Cell>
                        <Table.Cell>{candidateHardwareSummary(server)}</Table.Cell>
                        {eligibility && (
                          <Table.Cell>
                            <EligibilityValue eligibility={serverEligibility} />
                          </Table.Cell>
                        )}
                        {roleCells(server, 'table').map((cell, index) => <Table.Cell key={roleHeaders[index]}>{cell}</Table.Cell>)}
                      </Table.Row>
                    )
                  })}
                </Table.Body>
              </Table.Root>
                </StickyTableFrame>
              )}
              mobile={(
                <div className="sw-resource-card-list sw-platform-server-card-list" aria-label="Deployable Servers">
                  {visibleServers.map((server) => {
                    const existing = existingPlatformAssignment(server, state.data.platforms)
                    const deployed = server.status.provisioning?.state === 'deployed'
                    const serverEligibility = eligibility?.(server)
                    const unavailable = Boolean(existing) || Boolean(eligibility && !serverEligibility?.eligible)
                    const willProvision = !unavailable && selectedServerIds.includes(server.id)
                    const status = deployed
                      ? <Badge colorPalette="green" variant="subtle">Deployed</Badge>
                      : willProvision
                        ? <Badge colorPalette="blue" variant="subtle">Will provision</Badge>
                        : <Badge colorPalette="blue" variant="subtle">Ready</Badge>

                    return (
                      <ResourceCard
                        key={server.id}
                        title={serverDisplayName(server)}
                        description={<span className="sw-mono">{serverPrimaryAddress(server) ?? 'No observed address'}</span>}
                        status={status}
                        selected={willProvision}
                      >
                        <ResourceCardField label="Hardware">{candidateHardwareSummary(server)}</ResourceCardField>
                        <ResourceCardField label="Current assignment">
                          {existing ? (
                            <span className="sw-cell-inline">
                              <Badge colorPalette="red" variant="subtle">In use</Badge>
                              <span>{existing.platformName} ({existing.platformType}, {existing.detail})</span>
                            </span>
                          ) : '-'}
                        </ResourceCardField>
                        {eligibility && (
                          <ResourceCardField label="Eligibility">
                            <EligibilityValue eligibility={serverEligibility} />
                          </ResourceCardField>
                        )}
                        {roleCells(server, 'card').map((cell, index) => (
                          <ResourceCardField key={roleHeaders[index]} label={roleHeaders[index]}>{cell}</ResourceCardField>
                        ))}
                      </ResourceCard>
                    )
                  })}
                </div>
              )}
            />
          )}
        </WizardSection>
      ),
    })

    steps.push({
      id: 'deploy-features',
      name: 'Features',
      canProceed: featuresValid,
      blockedReason: !form.features.valid ? form.features.blockedReason : invalidFeature?.blockedReason,
      content: (
        <WizardSection title={form.features.title}>
          {form.features.content}
          {features.map((feature, index) => <Fragment key={index}>{feature.content}</Fragment>)}
          <TimeSyncCheckToggle enforced={timeSyncEnforced} onEnforcedChange={setTimeSyncEnforced} />
        </WizardSection>
      ),
    })

    if (needsProvisioning) {
      const preparationScopeTitle =
        'Preparing ' + readySelectedServers.length +
        (readySelectedServers.length === 1 ? ' Server that still needs' : ' Servers that still need') +
        ' an operating system'
      const preparationScopeMessage = deployedSelectedServers.length > 0
        ? deployedSelectedServers.length + ' already-deployed Server' +
          (deployedSelectedServers.length === 1 ? '' : 's') +
          ' in this deployment are used as-is and skip these settings.'
        : 'These settings apply to every selected Server.'
      const preparationScope = (
        <Alert status="info" title={preparationScopeTitle}>
          {preparationScopeMessage}
        </Alert>
      )

      steps.push({
        id: 'deploy-os-image',
        name: 'OS image',
        canProceed: osImageValid,
        blockedReason: provisioningErrorText || (provisioningLoading ? 'Wait for the OS image catalog to load.' : 'Choose an OS image for the Servers that need provisioning.'),
        content: (
          <OSImageSelectionStep
            images={images}
            value={effectiveImageId}
            siteId={effectiveSiteId}
            integrationId={integrationId}
            loading={provisioningLoading}
            error={provisioningError}
            disabled={Boolean(selectedTemplate) || Boolean(provisioningError)}
            onChange={(value) => {
              setImageId(value)
              setDeployTarget(defaultDeployTargetForImage(
                images.find((image) => image.id === value),
              ))
            }}
            onRefresh={() => {
              if (!integrationId) return
              setProvisioningLoading(true)
              setProvisioningError(null)
              provisioning
                .listOSImages(integrationId)
                .then(setImages)
                .catch((error: Error) => setProvisioningError(failureOf(error, 'OS images could not be loaded.')))
                .finally(() => setProvisioningLoading(false))
            }}
            configurationSource={templatesEnabled ? {
              value: templateId,
              onChange: selectTemplate,
              options: [
                { value: '', label: 'Custom configuration' },
                ...templates
                  .filter((template) => template.integrationId === integrationId)
                  .map((template) => ({ value: template.id, label: template.name })),
              ],
            } : undefined}
          />
        ),
      })

      steps.push({
        id: 'deploy-os-installation',
        name: 'Installation',
        canProceed: osInstallationValid,
        blockedReason: 'Choose an OS image before configuring its installation mode.',
        content: (
          <WizardSection title="OS installation">
            <div className="sw-form-grid">
              <DeployTargetField
                value={effectiveDeployTarget}
                disabled={Boolean(selectedTemplate)}
                onChange={setDeployTarget}
                helperText={selectedTemplate
                  ? 'The selected template owns the install location.'
                  : undefined}
              />
              {form.deployModeNotice(effectiveEphemeral)}
              {!selectedTemplate && (
                <div className="sw-form-grid__wide">
                  <AdvancedSettings label="Advanced OS settings">
                    <Field.Root>
                      <Field.Label>Cloud-init user data</Field.Label>
                      <Textarea
                        value={cloudInit}
                        onChange={(event) => setCloudInit(event.target.value)}
                        rows={7}
                        autoComplete="off"
                        placeholder="#cloud-config"
                      />
                      <Field.HelperText>Cloud-init stays in memory and is submitted only with this deployment.</Field.HelperText>
                    </Field.Root>
                  </AdvancedSettings>
                </div>
              )}
            </div>
            {selectedImageModeUnsupported && (
              <UnsupportedDeployModeWarning
                imageName={selectedImage?.status.name || effectiveImageId}
                deployTarget={effectiveDeployTarget}
              />
            )}
            {selectedTemplate && (
              <Alert status="info" title="Template settings are locked">
                The selected template owns the install location, cloud-init, and addressing intent.
              </Alert>
            )}
          </WizardSection>
        ),
      })

      steps.push({
        id: 'deploy-os-networking',
        name: 'OS networking',
        canProceed: osNetworkingValid,
        blockedReason: provisioningErrorText || 'Complete the network interface and addressing assignment for every Server being provisioned.',
        content: (
          <WizardSection title="OS networking">
            {provisioningError && (
              <Alert status="error" title="Provisioning data is unavailable">
                <FailureMessage failure={provisioningError} />
              </Alert>
            )}
            <section className="sw-section">
              <SectionHeader title="Addressing and interfaces" description="Configure each boot interface before deployment." />
              <div className="sw-section-body">
                <div className="sw-form-grid">
                  <AddressingModeField
                    value={effectiveNetworkMode}
                    disabled={Boolean(selectedTemplate)}
                    helperText={selectedTemplate
                      ? 'The selected template owns the addressing intent for machine preparation.'
                      : undefined}
                    onChange={(mode) => {
                      setNetworkMode(mode)
                      if (mode !== 'static') setDefaultGateway(false)
                    }}
                  />
                  {effectiveNetworkMode === 'static' && (
                    <Field.Root>
                      <Checkbox
                        id="platform-default-gateway"
                        checked={selectedTemplate?.network.defaultGateway ?? defaultGateway}
                        disabled={Boolean(selectedTemplate)}
                        onCheckedChange={(checked) => setDefaultGateway(checked)}
                      >
                        Use the selected subnet for the default route
                      </Checkbox>
                      <Field.HelperText>The provider uses each selected subnet's configured gateway for this static link.</Field.HelperText>
                    </Field.Root>
                  )}
                </div>
                {siteDhcpSubnets.length > 0 && <SiteDhcpAddressingWarning subnets={siteDhcpSubnets} />}
                {!networkInspection && !provisioningLoading && <Alert status="warning" title="Select machines to inspect their network interfaces" />}
                {networkInspection && !networkAssignmentsValid && (
                  <Alert status="warning" title="Complete every network assignment">
                    Select one compatible interface and subnet for every Server. Static mode also requires an IPv4 address per target.
                  </Alert>
                )}
              </div>
              {networkInspection && (
                <NetworkAssignmentsEditor
                  targets={networkInspection.targets}
                  serverNames={Object.fromEntries(
                    selectedServers.map((server) => [server.id, serverDisplayName(server)]),
                  )}
                  assignments={networkAssignments}
                  mode={effectiveNetworkMode}
                  ariaLabel="Platform operating system network assignments"
                  onChange={(serverId, assignment) =>
                    setNetworkAssignments((current) => ({
                      ...current,
                      [serverId]: assignment,
                    }))
                  }
                />
              )}
            </section>
          </WizardSection>
        ),
      })

      // The three preparation sections share one wizard step, so the scope banner that says which
      // Servers they apply to is rendered once, above all three.
      const preparationSteps = steps.splice(-3, 3)
      steps.push({
        id: 'deploy-server-preparation',
        name: 'Operating system',
        canProceed: osConfigurationValid,
        blockedReason: provisioningErrorText || (effectiveImageId
          ? 'Complete the addressing assignment for every Server being provisioned.'
          : 'Choose an OS image for the Servers that need provisioning.'),
        content: (
          <div className="sw-platform-preparation-stack">
            {preparationScope}
            {preparationSteps.map((step) => <div key={step.id}>{step.content}</div>)}
          </div>
        ),
      })
    }

    for (const step of extraSteps) {
      steps.push({
        id: step.id,
        name: step.name,
        canProceed: step.canProceed,
        blockedReason: step.blockedReason,
        content: <WizardSection title={step.title}>{step.content}</WizardSection>,
      })
    }

    const reviewSite = state.data.sites.find((site) => site.id === effectiveSiteId)?.name ?? effectiveSiteId
    const preparationSummary = needsProvisioning
      ? `Provision ${readySelectedServers.length}; reuse existing OS on ${deployedSelectedServers.length}`
      : 'Use existing OS on every selected Server'
    const osReviewItems = [
      { label: 'OS image', value: images.find((image) => image.id === effectiveImageId)?.status.name ?? effectiveImageId },
      { label: 'Deploy mode', value: osDeploymentModeLabel },
      {
        label: 'Addressing',
        value: effectiveNetworkMode === 'static'
          ? 'Static per Server'
          : DEPLOYMENT_NETWORK_MODE_LABELS[effectiveNetworkMode],
      },
    ]
    const reviewGroups = [
      ...form.reviewGroups({ name, site: reviewSite ?? '', preparationSummary }),
      {
        title: features.length > 0 ? 'Features' : 'Time synchronization',
        stepId: 'deploy-features',
        items: [
          ...features.flatMap((feature) => feature.reviewItems),
          { label: 'Clock check', value: timeSyncReview },
        ],
      },
      ...(needsProvisioning
        ? [{ title: 'Operating system', stepId: 'deploy-server-preparation', items: osReviewItems }]
        : []),
      ...form.reviewGroupsAfterOS(selectedServers, staticAddresses),
    ]

    steps.push({
      id: 'deploy-review',
      name: 'Review',
      // Submitting is not a reason to block: the Wizard disables Finish through `finishing` and
      // hides this reason meanwhile, so it only ever names an incomplete step.
      canProceed: basicsValid && machinesValid && featuresValid && osConfigurationValid && extraStepsValid,
      blockedReason: 'Return to the incomplete deployment step before submitting.',
      content: (
        <WizardSection title="Review deployment">
          <div className="sw-platform-review-grid">
            {reviewGroups.map((group) => (
              <ReviewGroup
                key={group.title}
                title={group.title}
                items={group.items}
                onEdit={() => editStep(group.stepId)}
              />
            ))}
          </div>
          <Alert status="info" title="Submitting starts the Platform's install">
            Swallow records the Platform and its install as a Change Plan approved by you, then starts building it on the Servers above. When the Change Plan policy requires a separate approval, the install waits on the Change Plans page instead.
          </Alert>
          {needsProvisioning && form.deployModeNotice(effectiveEphemeral)}
        </WizardSection>
      ),
    })
  }

  const platformLabel = module.label
  const PlatformIcon = module.deploy.icon

  return (
    <div className="console-page">
      <PageHeader
        title={`Deploy ${platformLabel} Platform`}
        subtitle={`Configure a ${platformLabel} runtime with its own topology, features, and preparation workflow.`}
        breadcrumbs={[
          { label: 'Platforms', href: scopedHref('/platforms') },
          { label: 'Deploy', href: scopedHref('/platforms/deploy') },
          { label: platformLabel },
        ]}
      />
      <div className="sw-platform-deploy-hero">
        <div className="sw-platform-deploy-hero__identity">
          <span className="sw-platform-deploy-hero__icon" aria-hidden="true"><PlatformIcon size={25} /></span>
          <div>
            <Text className="sw-platform-deploy-eyebrow">Managed runtime</Text>
            <Heading as="h2" size="md">{platformLabel} deployment</Heading>
          </div>
        </div>
        <Button asChild size="sm" variant="outline">
          <RouterLink to={scopedHref('/platforms/deploy')}>Change platform</RouterLink>
        </Button>
      </div>

      {state.status === 'loading' && <LoadingState rows={7} />}
      {state.status === 'error' && <ErrorState message={state.message} requestId={state.requestId} />}
      {state.status === 'ready' && (
        <Wizard
          steps={steps}
          activeStep={wizardStep}
          onActiveStepChange={setWizardStep}
          onFinish={() => void deploy()}
          finishLabel={`Deploy ${platformLabel} Platform`}
          finishing={submitting}
        />
      )}
    </div>
  )
}

/** A Platform module's eligibility for one candidate, with its shortfalls when it is ineligible. */
function EligibilityValue({ eligibility }: { eligibility: { eligible: boolean; explanation: string } | undefined }) {
  return eligibility?.eligible ? (
    <Badge colorPalette="green" variant="subtle">Eligible</Badge>
  ) : (
    <span className="sw-cell-inline">
      <Badge colorPalette="red" variant="subtle">Ineligible</Badge>
      <Text className="sw-muted">{eligibility?.explanation ?? 'Policy unavailable'}</Text>
    </span>
  )
}

/** The deploy wizard of one Platform type, or an error when no module ships a deploy form for it. */
function DeployPlatformTypePage({ platformType }: { platformType: PlatformType }) {
  const module = platformModuleUI(platformType)
  if (!module) return <ErrorState message={`This dashboard cannot deploy ${platformType} Platforms.`} />
  return <DeployPlatformWizardPage platformType={platformType} module={module} />
}

/** Fixed Kubernetes deployment route; Platform type is no longer a form choice. */
export function DeployKubernetesPlatformPage() {
  return <DeployPlatformTypePage platformType="kubernetes" />
}

/** Fixed Slurm deployment route; its steps and optional features come from the Slurm module. */
export function DeploySlurmPlatformPage() {
  return <DeployPlatformTypePage platformType="slurm" />
}
