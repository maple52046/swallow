import { useEffect, useMemo, useState, type ReactNode } from 'react'
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
  FormHelperText,
  FormSelect,
  FormSelectOption,
  HelperText,
  HelperTextItem,
  InputGroup,
  InputGroupItem,
  Label,
  LabelGroup,
  TextArea,
  TextInput,
  ToggleGroup,
  ToggleGroupItem,
  Title,
  Wizard,
  WizardStep,
} from '@patternfly/react-core'
import { RefreshCcw } from 'lucide-react'
import { Table, Tbody, Td, Th, Thead, Tr } from '@patternfly/react-table'
import { useNavigate } from 'react-router-dom'
import { useApp } from '@/di/AppProvider'
import { platformLifecycleLabel } from '@/domain/platform/lifecycle'
import type { Platform, GPUStackOwner, NodeRole, PlatformType, PlatformMachinePreparation, RoleAssignment } from '@/domain/platform/types'
import type { DeploymentNetworkMode, DeploymentTemplate, NetworkInspectionResult } from '@/domain/provisioning/types'
import type { OSImage } from '@/domain/site/types'
import { serverDisplayName, serverPrimaryAddress, type Server } from '@/domain/server/types'
import { EmptyState } from '@/presentation/components/EmptyState'
import { ErrorState } from '@/presentation/components/ErrorState'
import { LoadingState } from '@/presentation/components/LoadingState'
import { PageHeader } from '@/presentation/components/PageHeader'
import { SingleSelect } from '@/presentation/components/SingleSelect'
import { SectionHeader, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { formatSubnetOptionLabel } from '@/presentation/utils/network'
import { useToast } from '@/presentation/components/toast/toastContext'
import { useSiteScope } from '@/presentation/contexts/SiteScopeContext'
import { useDeployableServers } from './useDeployableServers'

const DEFAULT_K0S_VERSION = 'v1.36.3+k0s.2'
const DEFAULT_POD_CIDR = '10.244.0.0/16'
const DEFAULT_SERVICE_CIDR = '10.96.0.0/12'

type RoleChoice = 'none' | NodeRole
type TopologyChoice = 'standalone' | 'multi-node' | 'high-availability'

// A platform deploy converges over both pool-ready and already-deployed Servers (ADR 017):
// ready Servers are provisioned to an OS first, deployed Servers are reused as-is. The wizard
// therefore offers both and derives the machine-preparation mode from the selection instead of
// asking the operator to pick it up front. Kept module-level so its reference is stable for the
// candidate-loading effect.
const CANDIDATE_PROVISIONING_STATES = ['ready', 'deployed'] as const

function validAddress(value: string): boolean {
  const parts = value.split('.')
  return parts.length === 4 && parts.every(
    (part) => /^\d{1,3}$/.test(part) && Number(part) <= 255,
  )
}

function validCIDR(value: string): boolean {
  const [address, prefix, ...rest] = value.split('/')
  const numericPrefix = Number(prefix)
  return rest.length === 0 && validAddress(address) && /^\d{1,2}$/.test(prefix ?? '') &&
    numericPrefix >= 0 && numericPrefix <= 32
}

/** Human labels and risk language for the three supported placement shapes. */
function topologyPresentation(topology: TopologyChoice) {
  switch (topology) {
    case 'standalone':
      return {
        label: 'Standalone',
        guidance: 'One Server runs the control plane and workloads. There is no control-plane failover, but the platform can be expanded later.',
      }
    case 'multi-node':
      return {
        label: 'Multi-node (non-HA)',
        guidance: 'One Server runs the control plane and one or more workers run workloads. A control-plane outage stops platform management.',
      }
    case 'high-availability':
      return {
        label: 'High availability',
        guidance: 'An odd control-plane quorum of at least three uses a virtual IP. At least one selected Server must run workloads.',
      }
  }
}

/** Suggests a network range to verify without claiming that any particular address is free. */
function selectedNetworkGuidance(addresses: string[]): string {
  const ipv4 = addresses.find(validAddress)
  if (!ipv4) return 'Selected machines have no observed IPv4 address. Verify provider inventory before deployment.'
  const octets = ipv4.split('.')
  return `Observed addresses include ${octets.slice(0, 3).join('.')}.x. Confirm routing, DHCP ranges, and address reservations with the network owner.`
}

interface ExistingPlatformAssignment {
  platformName: string
  platformType: string
  detail: string
}

/**
 * Maps backend-owned deployment claims and observed membership into one candidate-table
 * explanation. Durable claims take precedence because they also cover partial deployments
 * whose membership has not become observable yet.
 */
function existingPlatformAssignment(
  server: Server,
  platforms: Platform[],
  deploymentClaims: Record<string, Platform>,
): ExistingPlatformAssignment | null {
  const claim = deploymentClaims[server.id]
  if (claim) {
    const role = server.membership?.platformId === claim.id
      ? server.membership.role
      : null
    return {
      platformName: claim.name,
      platformType: claim.type,
      detail: role
        ? `${role} · ${platformLifecycleLabel(claim.lifecycleState)}`
        : platformLifecycleLabel(claim.lifecycleState),
    }
  }
  if (!server.membership) return null
  const platform = platforms.find((candidate) => candidate.id === server.membership?.platformId)
  return {
    platformName: platform?.name ?? server.membership.platformId,
    platformType: platform?.type ?? 'unknown',
    detail: server.membership.role,
  }
}

/**
 * PatternFly platform deployment workflow.
 *
 * Machine selection intentionally precedes networking: the selected topology and observed
 * addresses decide whether a VIP exists and give the operator concrete allocation context.
 * Accepted work stays on the Platform page; Operations remains a troubleshooting drill-down.
 */
export function DeployPlatformWizardPage() {
  const navigate = useNavigate()
  const { platforms, provisioning } = useApp()
  const { siteId: scopedSiteId, scopedHref } = useSiteScope()
  const { showToast } = useToast()
  const [siteId, setSiteId] = useState<string | undefined>(scopedSiteId)
  const [name, setName] = useState('')
  const [platformType, setPlatformType] = useState<PlatformType>('kubernetes')
  const [gpuStackOwner, setGPUStackOwner] = useState<GPUStackOwner>('provisioning')
  const [k0sVersion, setK0sVersion] = useState(DEFAULT_K0S_VERSION)
  // Slurm intent. Node roles are per-daemon (a Server may run slurmctld, slurmd, or both).
  const [clusterName, setClusterName] = useState('')
  const [slurmApiVersion, setSlurmApiVersion] = useState('')
  const [slurmDaemons, setSlurmDaemons] = useState<Record<string, { controller: boolean; compute: boolean }>>({})
  const [topology, setTopology] = useState<TopologyChoice>('high-availability')
  const [apiVip, setAPIVip] = useState('')
  const [apiVipPrefix, setAPIVipPrefix] = useState('24')
  const [podCidr, setPodCidr] = useState(DEFAULT_POD_CIDR)
  const [serviceCidr, setServiceCidr] = useState(DEFAULT_SERVICE_CIDR)
  const [roles, setRoles] = useState<Record<string, RoleChoice>>({})
  const [workloadControllers, setWorkloadControllers] = useState<Record<string, boolean>>({})
  const [submitting, setSubmitting] = useState(false)
  const [templates, setTemplates] = useState<DeploymentTemplate[]>([])
  const [templateId, setTemplateId] = useState('')
  const [images, setImages] = useState<OSImage[]>([])
  const [imageId, setImageId] = useState('')
  const [ephemeral, setEphemeral] = useState(false)
  const [cloudInit, setCloudInit] = useState('')
  const [networkMode, setNetworkMode] = useState<DeploymentNetworkMode>('automatic')
  const [defaultGateway, setDefaultGateway] = useState(false)
  const [networkInspection, setNetworkInspection] = useState<NetworkInspectionResult | null>(null)
  const [networkAssignments, setNetworkAssignments] = useState<Record<string, { interfaceId: string; subnetId: string; ipAddress: string }>>({})
  const [provisioningLoading, setProvisioningLoading] = useState(false)
  const [provisioningError, setProvisioningError] = useState('')
  const effectiveSiteId = siteId ?? scopedSiteId
  const state = useDeployableServers(effectiveSiteId, CANDIDATE_PROVISIONING_STATES)

  // Deployment templates are per-Site; the OS step filters them to the derived provisioner.
  useEffect(() => {
    if (!effectiveSiteId) return
    let canceled = false
    provisioning.listTemplates({ siteId: effectiveSiteId })
      .then((nextTemplates) => { if (!canceled) setTemplates(nextTemplates) })
      .catch((error: Error) => { if (!canceled) setProvisioningError(error.message) })
    return () => { canceled = true }
  }, [effectiveSiteId, provisioning])

  const isSlurm = platformType === 'slurm'
  const assignments = useMemo<RoleAssignment[]>(() => Object.entries(roles)
    .filter(([, role]) => role !== 'none')
    .map(([serverId, role]) => ({
      serverId,
      role: role as NodeRole,
      ...(role === 'control-plane' && workloadControllers[serverId]
        ? { runWorkloads: true }
        : {}),
    })), [roles, workloadControllers])
  const controllers = assignments.filter((item) => item.role === 'control-plane').length
  const workers = assignments.filter((item) => item.role === 'worker').length
  const workloadCount = workers + assignments.filter(
    (item) => item.role === 'control-plane' && item.runWorkloads,
  ).length
  // Slurm per-daemon selection: a Server may run slurmctld, slurmd, or both. More than one
  // controller is HA; Swallow provisions the shared StateSaveLocation automatically, so the
  // operator field is only an optional override and never gates the deploy.
  const slurmSelectedIds = Object.entries(slurmDaemons)
    .filter(([, daemons]) => daemons.controller || daemons.compute)
    .map(([serverId]) => serverId)
  const slurmControllerIds = Object.entries(slurmDaemons)
    .filter(([, daemons]) => daemons.controller)
    .map(([serverId]) => serverId)
  const slurmComputeIds = Object.entries(slurmDaemons)
    .filter(([, daemons]) => daemons.compute)
    .map(([serverId]) => serverId)
  const slurmHighlyAvailable = slurmControllerIds.length > 1
  const slurmTopologyValid = slurmControllerIds.length >= 1 && slurmComputeIds.length >= 1
  const basicsValid = Boolean(effectiveSiteId && name.trim() && (isSlurm || k0sVersion.trim()))
  // Locked Servers are excluded from the candidate list (see useDeployableServers), so the only
  // "unavailable" reason left to surface is an existing Platform assignment.
  const hasAssignedServers = state.status === 'ready'
    && state.data.servers.some((server) => Boolean(existingPlatformAssignment(
      server,
      state.data.platforms,
      state.data.deploymentClaims,
    )))
  const topologyValid = topology === 'standalone'
    ? assignments.length === 1 && controllers === 1 && workloadCount === 1
    : topology === 'multi-node'
      ? assignments.length >= 2 && controllers === 1 && workers >= 1
      : controllers >= 3 && controllers % 2 === 1 && workloadCount >= 1
  const networkingValid = validCIDR(podCidr.trim()) && validCIDR(serviceCidr.trim()) && (
    topology !== 'high-availability' || (
      validAddress(apiVip.trim()) && Number(apiVipPrefix) >= 1 && Number(apiVipPrefix) <= 32
    )
  )

  // Kubernetes selection comes from role assignments; Slurm from the per-daemon checkboxes.
  // Both funnel into one selected-server set so the OS provisioning step, provisioner
  // derivation, and candidate table stay shared across platform types.
  const selectedServerIds = isSlurm ? slurmSelectedIds : assignments.map((assignment) => assignment.serverId)
  const selectedServers = state.status === 'ready'
    ? selectedServerIds.flatMap((serverId) => {
      const server = state.data.servers.find((candidate) => candidate.id === serverId)
      return server ? [server] : []
    })
    : []
  const selectedAddresses = selectedServers.flatMap((server) => server.addresses)
  const initialController = selectedServers.find(
    (server) => roles[server.id] === 'control-plane',
  )
  const initialControllerAddress = initialController
    ? serverPrimaryAddress(initialController)
    : null

  // Convergent selection (ADR 017): a Server already `deployed` is reused as-is; anything else
  // (pool-ready) is provisioned first. The machine-preparation mode is derived from this rather
  // than chosen, so `deploy-kubernetes` may mix both in one wizard.
  const deployedSelectedServers = selectedServers.filter(
    (server) => server.provisioning?.state === 'deployed',
  )
  const readySelectedServers = selectedServers.filter(
    (server) => server.provisioning?.state !== 'deployed',
  )
  const needsProvisioning = readySelectedServers.length > 0
  // The provisioner is derived from the ready Servers rather than picked: they are provisioned
  // with one OS image, so they must share a single provisioner. `integrationId` is empty when
  // nothing needs provisioning or when the ready Servers disagree, which fails validation below.
  const readyProvisionerIds = [...new Set(readySelectedServers.map((server) => server.source.integrationId))]
  const provisionerConflict = readyProvisionerIds.length > 1
  const integrationId = readyProvisionerIds.length === 1 ? readyProvisionerIds[0] : ''
  // A convergent deploy is valid once the topology holds and — when any Server needs an OS —
  // those ready Servers share one resolvable provisioner. Slurm uses its per-daemon topology
  // rule; the shared provisioner requirement is identical.
  const machinesValid = (isSlurm ? slurmTopologyValid : topologyValid)
    && (!needsProvisioning || (!provisionerConflict && Boolean(integrationId)))
  // Slurm has no platform-level network step (no Pod/Service CIDR or API VIP); its networking
  // is only the OS provisioning network handled in the Operating system step.
  const platformNetworkingValid = isSlurm ? true : networkingValid

  // OS images are per-provisioner; (re)load them whenever the derived provisioner changes.
  useEffect(() => {
    if (!integrationId) return
    let canceled = false
    provisioning.listOSImages(integrationId).then((nextImages) => {
      if (!canceled) setImages(nextImages)
    }).catch((error: Error) => {
      if (!canceled) setProvisioningError(error.message)
    }).finally(() => {
      if (!canceled) setProvisioningLoading(false)
    })
    return () => { canceled = true }
  }, [integrationId, provisioning])

  // Only the pool-ready Servers need OS + network preparation; deployed Servers are reused.
  const preparationTargetKey = readySelectedServers.map((server) => server.id).sort().join(',')
  useEffect(() => {
    if (!needsProvisioning || !preparationTargetKey) return
    let canceled = false
    provisioning.inspectDeploymentNetworks(preparationTargetKey.split(',')).then((inspection) => {
      if (canceled) return
      setNetworkInspection(inspection)
      const nextAssignments: Record<string, { interfaceId: string; subnetId: string; ipAddress: string }> = {}
      let anyStatic = false
      for (const target of inspection.targets) {
        nextAssignments[target.serverId] = {
          interfaceId: target.suggestion.interfaceId,
          subnetId: target.suggestion.subnetId,
          ipAddress: target.suggestion.ipAddress,
        }
        if (target.suggestion.mode === 'static') anyStatic = true
      }
      setNetworkAssignments(nextAssignments)
      setNetworkMode(anyStatic ? 'static' : 'automatic')
      setDefaultGateway(inspection.targets.some((target) => target.suggestion.defaultGateway))
    }).catch((error: Error) => {
      if (!canceled) setProvisioningError(error.message)
    }).finally(() => {
      if (!canceled) setProvisioningLoading(false)
    })
    return () => { canceled = true }
  }, [needsProvisioning, preparationTargetKey, provisioning])

  // Ignore a template left selected from a previously derived provisioner so it can never leak
  // another provisioner's image/network into this deploy.
  const selectedTemplate = templates.find(
    (template) => template.id === templateId && template.integrationId === integrationId,
  )
  const effectiveImageId = selectedTemplate?.imageId ?? imageId
  const effectiveEphemeral = selectedTemplate?.ephemeral ?? ephemeral
  const effectiveNetworkMode = selectedTemplate?.network.mode ?? networkMode
  const inspectedTargetKey = networkInspection?.targets.map((target) => target.serverId).sort().join(',') ?? ''
  const networkAssignmentsValid = !needsProvisioning || (
    networkInspection !== null && inspectedTargetKey === preparationTargetKey && readySelectedServers.every((server) => {
      const assignment = networkAssignments[server.id]
      const target = networkInspection.targets.find((item) => item.serverId === server.id)
      const selectedInterface = target?.network.interfaces.find((item) => item.id === assignment?.interfaceId)
      if (!assignment?.interfaceId || !assignment.subnetId || !selectedInterface?.availableSubnets.some((subnet) => subnet.id === assignment.subnetId)) return false
      return effectiveNetworkMode !== 'static' || validAddress(assignment.ipAddress)
    })
  )
  const osConfigurationValid = !needsProvisioning || Boolean(
    integrationId && effectiveImageId && networkAssignmentsValid && !provisioningError,
  )

  const selectTemplate = (nextTemplateId: string) => {
    setTemplateId(nextTemplateId)
    const template = templates.find((item) => item.id === nextTemplateId)
    if (!template) return
    setImageId(template.imageId)
    setEphemeral(template.ephemeral)
    setNetworkMode(template.network.mode)
    setDefaultGateway(template.network.defaultGateway)
  }
  const changeTopology = (next: TopologyChoice) => {
    setTopology(next)
    setRoles({})
    setWorkloadControllers({})
    setAPIVip('')
  }

  // Switching platform type clears every selection so a k0s topology never leaks into a Slurm
  // deploy or vice versa. Slurm forces the provisioning GPU owner (it has no GPU operator).
  const changeType = (next: PlatformType) => {
    setPlatformType(next)
    setRoles({})
    setWorkloadControllers({})
    setAPIVip('')
    setSlurmDaemons({})
    setTemplateId('')
    setImageId('')
    setNetworkInspection(null)
    setNetworkAssignments({})
    if (next === 'slurm') setGPUStackOwner('provisioning')
  }

  // Toggle one Slurm daemon on a node. A node keeps whichever daemons are checked; a node with
  // neither is simply not part of the cluster.
  const toggleSlurmDaemon = (serverId: string, daemon: 'controller' | 'compute', checked: boolean) => {
    const server = state.status === 'ready'
      ? state.data.servers.find((candidate) => candidate.id === serverId)
      : undefined
    if (server?.provisioning?.locked) return
    setSlurmDaemons((current) => {
      const existing = current[serverId] ?? { controller: false, compute: false }
      return { ...current, [serverId]: { ...existing, [daemon]: checked } }
    })
  }

  const changeRole = (serverId: string, next: RoleChoice) => {
    const server = state.status === 'ready'
      ? state.data.servers.find((candidate) => candidate.id === serverId)
      : undefined
    if (server?.provisioning?.locked) return

    if (topology === 'standalone') {
      setRoles(next === 'none' ? {} : { [serverId]: 'control-plane' })
      setWorkloadControllers(next === 'none' ? {} : { [serverId]: true })
      return
    }

    setRoles((current) => {
      const updated = { ...current, [serverId]: next }
      if (topology === 'multi-node' && next === 'control-plane') {
        for (const [candidateID, role] of Object.entries(updated)) {
          if (candidateID !== serverId && role === 'control-plane') updated[candidateID] = 'none'
        }
      }
      return updated
    })
    if (next !== 'control-plane') {
      setWorkloadControllers((current) => ({ ...current, [serverId]: false }))
    }
  }

  const deploy = async () => {
    if (!effectiveSiteId || !basicsValid || !machinesValid || !osConfigurationValid || !platformNetworkingValid || submitting) return
    setSubmitting(true)
    // The mode is derived from the selection, not chosen: reuse existing-OS Servers when
    // nothing needs provisioning, otherwise converge — provision the ready Servers and reuse the
    // deployed ones. Only ready Servers carry OS + network preparation. Shared by both types.
    const machinePreparation: PlatformMachinePreparation = !needsProvisioning
      ? { mode: 'existing_os' }
      : {
        mode: 'provision_os',
        templateId: templateId || undefined,
        settings: templateId ? undefined : { imageId: effectiveImageId, ephemeral: effectiveEphemeral },
        userData: templateId
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
            ipAddress: effectiveNetworkMode === 'static'
              ? networkAssignments[server.id]?.ipAddress.trim()
              : undefined,
          })),
        },
      }
    try {
      const result = await platforms.deployPlatform(isSlurm
        ? {
          siteId: effectiveSiteId,
          name: name.trim(),
          type: 'slurm',
          gpuStackOwner: 'provisioning',
          slurm: {
            clusterName: clusterName.trim() || undefined,
            apiVersion: slurmApiVersion.trim() || undefined,
            // stateSaveLocation is intentionally omitted: Swallow provisions the HA shared
            // StateSaveLocation itself, so the wizard does not collect a path.
            nodeAssignments: slurmSelectedIds.map((serverId) => ({
              serverId,
              controller: Boolean(slurmDaemons[serverId]?.controller),
              compute: Boolean(slurmDaemons[serverId]?.compute),
            })),
          },
          machinePreparation,
        }
        : {
          siteId: effectiveSiteId,
          name: name.trim(),
          type: 'kubernetes',
          gpuStackOwner,
          k0sVersion: k0sVersion.trim(),
          ...(topology === 'high-availability'
            ? { apiVip: apiVip.trim(), apiVipPrefix: Number(apiVipPrefix) }
            : {}),
          podCidr: podCidr.trim(),
          serviceCidr: serviceCidr.trim(),
          roleAssignments: assignments,
          machinePreparation,
        })
      showToast({
        title: 'Platform deployment started',
        description: 'Lifecycle and membership will update on the Platform page. Detailed automation output remains available when troubleshooting.',
        tone: 'success',
      })
      navigate(scopedHref(`/platforms/${result.platformId}`))
    } catch (error) {
      showToast({
        title: 'Deployment failed',
        description: error instanceof Error ? error.message : 'Could not start the deployment.',
        tone: 'error',
      })
      setSubmitting(false)
    }
  }

  return (
    <div className="operator-page">
      <PageHeader
        title="Deploy platform"
        breadcrumbs={[{ label: 'Platforms', href: scopedHref('/platforms') }, { label: 'Deploy' }]}
        subtitle="Build a Kubernetes (k0s) or Slurm platform on Ready or already-deployed Servers, with an existing or newly provisioned operating system."
      />
      {state.status === 'loading' && <LoadingState rows={7} />}
      {state.status === 'error' && <ErrorState message={state.message} />}
      {state.status === 'ready' && (
        <Wizard
          aria-label="Deploy platform"
          className="sw-deploy-wizard"
          height="min(700px, calc(100vh - 220px))"
          isVisitRequired
          shouldFocusContent
          onClose={() => navigate(scopedHref('/platforms'))}
          onSave={() => void deploy()}
        >
          <WizardStep
            name="Basics"
            id="deploy-basics"
            status={basicsValid ? 'success' : 'default'}
            footer={{ isNextDisabled: !basicsValid }}
          >
            <WizardSection title="Platform identity">
              <Form className="sw-form-grid">
                <FormGroup label="Platform type" isRequired fieldId="platform-type">
                  <FormSelect
                    id="platform-type"
                    value={platformType}
                    onChange={(_event, value) => changeType(value as PlatformType)}
                  >
                    <FormSelectOption value="kubernetes" label="Kubernetes" />
                    <FormSelectOption value="slurm" label="Slurm" />
                  </FormSelect>
                </FormGroup>
                <FormGroup label="Site" isRequired fieldId="platform-site">
                  <SingleSelect
                    id="platform-site"
                    ariaLabel="Site"
                    value={effectiveSiteId ?? ''}
                    placeholder="Select a Site"
                    options={state.data.sites.map((site) => ({
                      value: site.id,
                      label: site.name,
                    }))}
                    isRequired
                    onChange={(value) => {
                      setSiteId(value)
                      setRoles({})
                      setWorkloadControllers({})
                      setAPIVip('')
                      setSlurmDaemons({})
                      setTemplateId('')
                      setImageId('')
                      setNetworkInspection(null)
                      setNetworkAssignments({})
                    }}
                  />
                </FormGroup>
                <FormGroup label="Platform name" isRequired fieldId="platform-name">
                  <TextInput
                    id="platform-name"
                    value={name}
                    onChange={(_event, value) => setName(value)}
                    placeholder={isSlurm ? 'lab-slurm' : 'lab-k0s'}
                  />
                </FormGroup>
                {!isSlurm && (
                  <FormGroup label="GPU stack owner" isRequired fieldId="platform-gpu-owner">
                    <FormSelect
                      id="platform-gpu-owner"
                      value={gpuStackOwner}
                      onChange={(_event, value) => setGPUStackOwner(value as GPUStackOwner)}
                    >
                      <FormSelectOption value="provisioning" label="Provisioning" />
                      <FormSelectOption value="gpu-operator" label="GPU Operator" />
                    </FormSelect>
                  </FormGroup>
                )}
                {!isSlurm && (
                  <FormGroup label="k0s version" isRequired fieldId="platform-version">
                    <TextInput
                      id="platform-version"
                      value={k0sVersion}
                      onChange={(_event, value) => setK0sVersion(value)}
                    />
                  </FormGroup>
                )}
                {isSlurm && (
                  <FormGroup label="Cluster name" fieldId="platform-slurm-cluster">
                    <TextInput
                      id="platform-slurm-cluster"
                      value={clusterName}
                      onChange={(_event, value) => setClusterName(value)}
                      placeholder={name.trim() || 'lab-slurm'}
                    />
                    <FormHelperText>
                      <HelperText>
                        <HelperTextItem>Slurm ClusterName; defaults to a sanitized platform name.</HelperTextItem>
                      </HelperText>
                    </FormHelperText>
                  </FormGroup>
                )}
                {isSlurm && (
                  <FormGroup label="slurmrestd API version" fieldId="platform-slurm-apiversion">
                    <TextInput
                      id="platform-slurm-apiversion"
                      value={slurmApiVersion}
                      onChange={(_event, value) => setSlurmApiVersion(value)}
                      placeholder="auto-detect (e.g. v0.0.42)"
                    />
                    <FormHelperText>
                      <HelperText>
                        <HelperTextItem>Optional. Leave blank to let the deployment detect the endpoint version.</HelperTextItem>
                      </HelperText>
                    </FormHelperText>
                  </FormGroup>
                )}
              </Form>
            </WizardSection>
          </WizardStep>

          <WizardStep
            name="Machines"
            id="deploy-machines"
            status={machinesValid ? 'success' : 'default'}
            footer={{ isNextDisabled: !machinesValid }}
          >
            <WizardSection title={isSlurm ? 'Nodes and daemons' : 'Topology and machines'}>
              {!isSlurm && (
                <>
                  <FormGroup label="Topology" isRequired fieldId="platform-topology">
                    <FormSelect
                      id="platform-topology"
                      value={topology}
                      onChange={(_event, value) => changeTopology(value as TopologyChoice)}
                    >
                      <FormSelectOption value="standalone" label="Standalone (single Server)" />
                      <FormSelectOption value="multi-node" label="Multi-node (non-HA)" />
                      <FormSelectOption value="high-availability" label="High availability" />
                    </FormSelect>
                  </FormGroup>
                  <Alert
                    variant={topology === 'high-availability' ? AlertVariant.info : AlertVariant.warning}
                    title={topologyPresentation(topology).label}
                    isInline
                  >
                    {topologyPresentation(topology).guidance}
                  </Alert>
                  <LabelGroup aria-label="Topology status">
                    <Label color={controllers > 0 ? 'green' : 'orange'}>{controllers} control-plane</Label>
                    <Label color={workloadCount > 0 ? 'green' : 'orange'}>{workloadCount} workload-capable</Label>
                    <Label color={assignments.length > 0 ? 'blue' : 'grey'}>{assignments.length} selected</Label>
                  </LabelGroup>
                </>
              )}
              {isSlurm && (
                <>
                  <Alert variant={AlertVariant.info} title="Assign Slurm daemons per node" isInline>
                    A node may run the controller daemon (slurmctld), the compute daemon (slurmd), or both. At least one controller and one compute node are required.
                  </Alert>
                  <LabelGroup aria-label="Slurm status">
                    <Label color={slurmControllerIds.length > 0 ? 'green' : 'orange'}>{slurmControllerIds.length} controller</Label>
                    <Label color={slurmComputeIds.length > 0 ? 'green' : 'orange'}>{slurmComputeIds.length} compute</Label>
                    <Label color={slurmSelectedIds.length > 0 ? 'blue' : 'grey'}>{slurmSelectedIds.length} selected</Label>
                  </LabelGroup>
                  {slurmHighlyAvailable && (
                    <Alert
                      variant={AlertVariant.info}
                      title="Shared controller state is provisioned automatically"
                      isInline
                    >
                      Multiple controllers require one shared StateSaveLocation. Swallow sets it
                      up for you — a managed NFS export on a selected node, mounted on every
                      controller — so there is nothing to enter here. (Lab-grade: the export host
                      is a single storage failure domain.)
                    </Alert>
                  )}
                </>
              )}
              {hasAssignedServers && (
                <Alert
                  variant={AlertVariant.warning}
                  title="Some Servers are already assigned"
                  isInline
                >
                  Assigned Servers remain visible for context. Remove the existing Platform assignment before selecting a role.
                </Alert>
              )}
              {provisionerConflict && (
                <Alert variant={AlertVariant.warning} title="Ready Servers span multiple provisioners" isInline>
                  Servers that still need an OS must share one provisioner so a single OS image applies. Deselect ready Servers from other provisioners, or include only already-deployed Servers from them.
                </Alert>
              )}
              {state.data.servers.length === 0 ? (
                <EmptyState
                  title="No deployable Servers"
                  message="This Site has no Ready or Deployed Servers to build a Platform from."
                />
              ) : (
                <StickyTableFrame>
                  <Table aria-label="Deployable Servers" variant="compact" className="sw-deploy-machines-table">
                    <Thead>
                      <Tr>
                        <Th>Server</Th><Th>Address</Th><Th>OS state</Th><Th>Current assignment</Th>
                        {isSlurm
                          ? <><Th>Controller</Th><Th>Compute</Th></>
                          : <><Th>Role</Th><Th>Runs workloads</Th></>}
                      </Tr>
                    </Thead>
                    <Tbody>
                      {state.data.servers.map((server) => {
                        const existing = existingPlatformAssignment(
                          server,
                          state.data.platforms,
                          state.data.deploymentClaims,
                        )
                        const deployed = server.provisioning?.state === 'deployed'
                        const unavailable = Boolean(existing)
                        const role = unavailable ? 'none' : roles[server.id] ?? 'none'
                        const daemons = slurmDaemons[server.id] ?? { controller: false, compute: false }
                        const willProvision = isSlurm ? (daemons.controller || daemons.compute) : role !== 'none'
                        return (
                          <Tr key={server.id}>
                            <Td dataLabel="Server"><strong>{serverDisplayName(server)}</strong></Td>
                            <Td dataLabel="Address" className="sw-mono">
                              {serverPrimaryAddress(server) ?? '-'}
                            </Td>
                            <Td dataLabel="OS state">
                              {deployed
                                ? <Label color="green">Deployed</Label>
                                : willProvision
                                  ? <Label color="blue">Will provision</Label>
                                  : <Label color="blue">Ready</Label>}
                            </Td>
                            <Td dataLabel="Current assignment">
                              {existing ? (
                                <span className="sw-cell-inline">
                                  <Label color="red">In use</Label>
                                  <span>
                                    {existing.platformName} ({existing.platformType}, {existing.detail})
                                  </span>
                                </span>
                              ) : '-'}
                            </Td>
                            {isSlurm ? (
                              <>
                                <Td dataLabel="Controller">
                                  <Checkbox
                                    id={`slurm-controller-${server.id}`}
                                    aria-label={existing
                                      ? `slurmctld on ${serverDisplayName(server)}, unavailable because it is assigned to ${existing.platformName}`
                                      : `Run slurmctld on ${serverDisplayName(server)}`}
                                    isChecked={daemons.controller}
                                    isDisabled={unavailable}
                                    onChange={(_event, checked) => toggleSlurmDaemon(server.id, 'controller', checked)}
                                  />
                                </Td>
                                <Td dataLabel="Compute">
                                  <Checkbox
                                    id={`slurm-compute-${server.id}`}
                                    aria-label={existing
                                      ? `slurmd on ${serverDisplayName(server)}, unavailable because it is assigned to ${existing.platformName}`
                                      : `Run slurmd on ${serverDisplayName(server)}`}
                                    isChecked={daemons.compute}
                                    isDisabled={unavailable}
                                    onChange={(_event, checked) => toggleSlurmDaemon(server.id, 'compute', checked)}
                                  />
                                </Td>
                              </>
                            ) : (
                              <>
                                <Td dataLabel="Role">
                                  <FormSelect
                                    aria-label={existing
                                      ? `Role for ${serverDisplayName(server)}, unavailable because it is assigned to ${existing.platformName}`
                                      : `Role for ${serverDisplayName(server)}`}
                                    value={role}
                                    isDisabled={unavailable}
                                    onChange={(_event, value) => changeRole(server.id, value as RoleChoice)}
                                  >
                                    <FormSelectOption value="none" label="Not included" />
                                    <FormSelectOption
                                      value="control-plane"
                                      label={topology === 'standalone' ? 'Standalone node' : 'Control-plane'}
                                    />
                                    {topology !== 'standalone' && (
                                      <FormSelectOption value="worker" label="Worker" />
                                    )}
                                  </FormSelect>
                                </Td>
                                <Td dataLabel="Runs workloads">
                                  {role === 'control-plane' ? (
                                    <Checkbox
                                      id={`platform-workload-${server.id}`}
                                      aria-label={`Run workloads on ${serverDisplayName(server)}`}
                                      isChecked={topology === 'standalone' || Boolean(workloadControllers[server.id])}
                                      isDisabled={topology === 'standalone'}
                                      onChange={(_event, checked) => setWorkloadControllers((current) => ({
                                        ...current,
                                        [server.id]: checked,
                                      }))}
                                    />
                                  ) : role === 'worker' ? <Label color="green">Yes</Label> : '-'}
                                </Td>
                              </>
                            )}
                          </Tr>
                        )
                      })}
                    </Tbody>
                  </Table>
                </StickyTableFrame>
              )}
            </WizardSection>
          </WizardStep>

          {needsProvisioning && (
            <WizardStep
              name="Operating system"
              id="deploy-operating-system"
              status={osConfigurationValid ? 'success' : 'default'}
              footer={{ isNextDisabled: !osConfigurationValid }}
            >
              <WizardSection title="Operating system configuration">
                <Alert
                  variant={AlertVariant.info}
                  title={`Preparing ${readySelectedServers.length} Server${readySelectedServers.length === 1 ? '' : 's'} that still need an operating system`}
                  isInline
                >
                  {deployedSelectedServers.length > 0
                    ? `${deployedSelectedServers.length} already-deployed Server${deployedSelectedServers.length === 1 ? '' : 's'} in this deployment are used as-is and skip these settings.`
                    : 'These settings apply to every selected Server.'}
                </Alert>
                {provisioningError && (
                  <Alert variant={AlertVariant.danger} title="Provisioning data is unavailable" isInline>
                    {provisioningError}
                  </Alert>
                )}
                <Form className="sw-form-grid">
                  <FormGroup label="Configuration source" isRequired fieldId="platform-template">
                    <FormSelect
                      id="platform-template"
                      value={templateId}
                      onChange={(_event, value) => selectTemplate(value)}
                    >
                      <FormSelectOption value="" label="Custom configuration" />
                      {templates
                        .filter((template) => template.integrationId === integrationId)
                        .map((template) => (
                          <FormSelectOption key={template.id} value={template.id} label={template.name} />
                        ))}
                    </FormSelect>
                  </FormGroup>
                  <FormGroup label="OS image" isRequired fieldId="platform-os-image">
                    <InputGroup>
                      <InputGroupItem isFill>
                        <SingleSelect
                          id="platform-os-image"
                          ariaLabel="OS image"
                          value={effectiveImageId}
                          placeholder="Select an OS image"
                          options={images.map((image) => ({
                            value: image.id,
                            label: `${image.name} - ${image.architecture}`,
                            description: `${image.osSystem} ${image.release}`,
                          }))}
                          isRequired
                          isDisabled={Boolean(selectedTemplate)}
                          onChange={setImageId}
                        />
                      </InputGroupItem>
                      <InputGroupItem>
                        <Button
                          variant="plain"
                          className="sw-icon-button"
                          icon={<RefreshCcw />}
                          aria-label="Refresh OS images"
                          isLoading={provisioningLoading}
                          onClick={() => {
                            if (!integrationId) return
                            setProvisioningLoading(true)
                            setProvisioningError('')
                            provisioning.listOSImages(integrationId)
                              .then(setImages)
                              .catch((error: Error) => setProvisioningError(error.message))
                              .finally(() => setProvisioningLoading(false))
                          }}
                        />
                      </InputGroupItem>
                    </InputGroup>
                  </FormGroup>
                  <FormGroup fieldId="platform-ephemeral">
                    <Checkbox
                      id="platform-ephemeral"
                      label="Run the operating system from memory"
                      description="Disks remain untouched and operating-system changes are lost after reboot."
                      isChecked={effectiveEphemeral}
                      isDisabled={Boolean(selectedTemplate)}
                      onChange={(_event, checked) => setEphemeral(checked)}
                    />
                  </FormGroup>
                  {!selectedTemplate && (
                    <FormGroup label="Cloud-init user data" fieldId="platform-user-data">
                      <TextArea
                        id="platform-user-data"
                        value={cloudInit}
                        onChange={(_event, value) => setCloudInit(value)}
                        rows={7}
                        autoComplete="off"
                        placeholder="#cloud-config"
                      />
                    </FormGroup>
                  )}
                </Form>

                <section className="sw-section">
                  <SectionHeader
                    title="Network configuration"
                    description="Swallow configures each boot interface before the operating system deployment starts."
                  />
                  <div className="sw-section-body">
                    <Form className="sw-form-grid">
                      <FormGroup label="Addressing mode" isRequired fieldId="platform-network-mode">
                        <ToggleGroup aria-label="Operating system addressing mode">
                          <ToggleGroupItem
                            text="Automatic"
                            buttonId="platform-network-automatic"
                            isSelected={effectiveNetworkMode === 'automatic'}
                            isDisabled={Boolean(selectedTemplate)}
                            onChange={() => {
                              setNetworkMode('automatic')
                              setDefaultGateway(false)
                            }}
                          />
                          <ToggleGroupItem
                            text="Static"
                            buttonId="platform-network-static"
                            isSelected={effectiveNetworkMode === 'static'}
                            isDisabled={Boolean(selectedTemplate)}
                            onChange={() => setNetworkMode('static')}
                          />
                        </ToggleGroup>
                      </FormGroup>
                      {effectiveNetworkMode === 'static' && (
                        <FormGroup fieldId="platform-default-gateway">
                          <Checkbox
                            id="platform-default-gateway"
                            label="Use the selected subnet for the default route"
                            description="The provider uses each selected subnet's configured gateway for this static link."
                            isChecked={selectedTemplate?.network.defaultGateway ?? defaultGateway}
                            isDisabled={Boolean(selectedTemplate)}
                            onChange={(_event, checked) => setDefaultGateway(checked)}
                          />
                        </FormGroup>
                      )}
                    </Form>
                    {!networkInspection && !provisioningLoading && (
                      <Alert variant={AlertVariant.warning} title="Select machines to inspect their network interfaces" isInline />
                    )}
                    {networkInspection && !networkAssignmentsValid && (
                      <Alert variant={AlertVariant.warning} title="Complete every network assignment" isInline>
                        Select one compatible interface and subnet for every Server. Static mode also requires an IPv4 address per target.
                      </Alert>
                    )}
                  </div>
                  {networkInspection && (
                    <StickyTableFrame>
                      <Table aria-label="Platform operating system network assignments" variant="compact" className="sw-network-assignment-table">
                        <Thead>
                          <Tr>
                            <Th>Server</Th>
                            <Th>Interface</Th>
                            <Th>Subnet</Th>
                            {effectiveNetworkMode === 'static' && <Th>Static IPv4 address</Th>}
                            <Th>Current mode</Th>
                          </Tr>
                        </Thead>
                        <Tbody>
                          {networkInspection.targets.map((target) => {
                            const server = selectedServers.find((item) => item.id === target.serverId)
                            const assignment = networkAssignments[target.serverId] ?? { interfaceId: '', subnetId: '', ipAddress: '' }
                            const iface = target.network.interfaces.find((item) => item.id === assignment.interfaceId)
                            const currentMode = iface?.rawProviderMode === 'AUTO'
                              ? 'Provider-managed (MAAS AUTO)'
                              : iface?.rawProviderMode || '-'
                            return (
                              <Tr key={target.serverId}>
                                <Td dataLabel="Server"><strong>{server ? serverDisplayName(server) : target.serverId}</strong></Td>
                                <Td dataLabel="Interface">
                                  <FormSelect
                                    aria-label={`Interface for ${server ? serverDisplayName(server) : target.serverId}`}
                                    value={assignment.interfaceId}
                                    onChange={(_event, value) => {
                                      const nextInterface = target.network.interfaces.find((item) => item.id === value)
                                      const subnetId = nextInterface?.availableSubnets.some((subnet) => subnet.id === assignment.subnetId)
                                        ? assignment.subnetId
                                        : nextInterface?.availableSubnets.length === 1
                                          ? nextInterface.availableSubnets[0].id
                                          : ''
                                      setNetworkAssignments((current) => ({
                                        ...current,
                                        [target.serverId]: { ...assignment, interfaceId: value, subnetId },
                                      }))
                                    }}
                                  >
                                    <FormSelectOption value="" label="Select an interface" isDisabled isPlaceholder />
                                    {target.network.interfaces.map((item) => (
                                      <FormSelectOption
                                        key={item.id}
                                        value={item.id}
                                        label={`${item.name} - ${item.macAddress}${item.boot ? ' (boot NIC)' : ''}`}
                                      />
                                    ))}
                                  </FormSelect>
                                </Td>
                                <Td dataLabel="Subnet">
                                  <FormSelect
                                    aria-label={`Subnet for ${server ? serverDisplayName(server) : target.serverId}`}
                                    value={assignment.subnetId}
                                    onChange={(_event, value) => setNetworkAssignments((current) => ({
                                      ...current,
                                      [target.serverId]: { ...assignment, subnetId: value },
                                    }))}
                                  >
                                    <FormSelectOption value="" label="Select a subnet" isDisabled isPlaceholder />
                                    {iface?.availableSubnets.map((subnet) => (
                                      <FormSelectOption key={subnet.id} value={subnet.id} label={formatSubnetOptionLabel(subnet)} />
                                    ))}
                                  </FormSelect>
                                </Td>
                                {effectiveNetworkMode === 'static' && (
                                  <Td dataLabel="Static IPv4 address">
                                    <TextInput
                                      aria-label={`Static IPv4 address for ${server ? serverDisplayName(server) : target.serverId}`}
                                      value={assignment.ipAddress}
                                      onChange={(_event, value) => setNetworkAssignments((current) => ({
                                        ...current,
                                        [target.serverId]: { ...assignment, ipAddress: value },
                                      }))}
                                      placeholder="192.0.2.10"
                                    />
                                  </Td>
                                )}
                                <Td dataLabel="Current mode" title={currentMode}>{currentMode}</Td>
                              </Tr>
                            )
                          })}
                        </Tbody>
                      </Table>
                    </StickyTableFrame>
                  )}
                </section>
              </WizardSection>
            </WizardStep>
          )}

          {!isSlurm && (
          <WizardStep
            name="Networking"
            id="deploy-networking"
            status={networkingValid ? 'success' : 'default'}
            footer={{ isNextDisabled: !networkingValid }}
          >
            <WizardSection title="Platform network">
              <section className="sw-network-context" aria-labelledby="selected-machine-addresses">
                <Title headingLevel="h3" size="md" id="selected-machine-addresses">
                  Selected machine addresses
                </Title>
                <LabelGroup aria-label="Selected machine addresses">
                  {selectedServers.map((server) => (
                    <Label key={server.id} color="grey">
                      {serverDisplayName(server)}: {serverPrimaryAddress(server) ?? '-'}
                    </Label>
                  ))}
                </LabelGroup>
                <p className="sw-muted">{selectedNetworkGuidance(selectedAddresses)}</p>
              </section>
              {topology === 'high-availability' ? (
                <Alert
                  variant={AlertVariant.info}
                  title="Allocate one unused virtual IP reachable by every selected machine"
                  isInline
                >
                  Swallow cannot verify DHCP reservations. Confirm the address is unused and outside dynamic ranges before deployment.
                </Alert>
              ) : (
                <Alert variant={AlertVariant.warning} title="Direct control-plane endpoint" isInline>
                  The Kubernetes API will use {initialControllerAddress ?? 'the selected control-plane address'}:6443. This topology has no endpoint failover.
                </Alert>
              )}
              <Form className="sw-form-grid">
                {topology === 'high-availability' && (
                  <>
                    <FormGroup label="API virtual IP" isRequired fieldId="platform-api-vip">
                      <TextInput
                        id="platform-api-vip"
                        value={apiVip}
                        onChange={(_event, value) => setAPIVip(value)}
                        placeholder="192.168.40.200"
                        validated={!apiVip || validAddress(apiVip) ? 'default' : 'error'}
                      />
                      <FormHelperText>
                        <HelperText>
                          <HelperTextItem variant={!apiVip || validAddress(apiVip) ? 'default' : 'error'}>
                            Enter an unused IPv4 address on the machine network.
                          </HelperTextItem>
                        </HelperText>
                      </FormHelperText>
                    </FormGroup>
                    <FormGroup label="VIP prefix length" isRequired fieldId="platform-api-prefix">
                      <TextInput
                        id="platform-api-prefix"
                        type="number"
                        value={apiVipPrefix}
                        onChange={(_event, value) => setAPIVipPrefix(value)}
                        min={1}
                        max={32}
                      />
                    </FormGroup>
                  </>
                )}
                <FormGroup label="Pod CIDR" isRequired fieldId="platform-pod-cidr">
                  <TextInput
                    id="platform-pod-cidr"
                    value={podCidr}
                    onChange={(_event, value) => setPodCidr(value)}
                    validated={!podCidr || validCIDR(podCidr) ? 'default' : 'error'}
                  />
                  <FormHelperText>
                    <HelperText>
                      <HelperTextItem variant={!podCidr || validCIDR(podCidr) ? 'default' : 'error'}>
                        Default {DEFAULT_POD_CIDR}; must not overlap selected machine addresses.
                      </HelperTextItem>
                    </HelperText>
                  </FormHelperText>
                </FormGroup>
                <FormGroup label="Service CIDR" isRequired fieldId="platform-service-cidr">
                  <TextInput
                    id="platform-service-cidr"
                    value={serviceCidr}
                    onChange={(_event, value) => setServiceCidr(value)}
                    validated={!serviceCidr || validCIDR(serviceCidr) ? 'default' : 'error'}
                  />
                  <FormHelperText>
                    <HelperText>
                      <HelperTextItem variant={!serviceCidr || validCIDR(serviceCidr) ? 'default' : 'error'}>
                        Default {DEFAULT_SERVICE_CIDR}; must not overlap selected machine addresses.
                      </HelperTextItem>
                    </HelperText>
                  </FormHelperText>
                </FormGroup>
              </Form>
            </WizardSection>
          </WizardStep>
          )}

          <WizardStep
            name="Review"
            id="deploy-review"
            status={basicsValid && machinesValid && osConfigurationValid && platformNetworkingValid ? 'success' : 'warning'}
            footer={{
              nextButtonText: 'Deploy platform',
              isNextDisabled: submitting || !basicsValid || !machinesValid || !osConfigurationValid || !platformNetworkingValid,
              nextButtonProps: { isLoading: submitting },
            }}
          >
            <WizardSection title="Review deployment">
              <Card isCompact>
                <CardTitle>{name}</CardTitle>
                <CardBody>
                  <DescriptionList isHorizontal isCompact>
                    {(isSlurm
                      ? [
                        ['Site', state.data.sites.find((site) => site.id === effectiveSiteId)?.name ?? effectiveSiteId],
                        ['Platform type', 'Slurm'],
                        ['Cluster name', clusterName.trim() || name.trim()],
                        ['Machine preparation', needsProvisioning
                          ? `Provision ${readySelectedServers.length}, ${deployedSelectedServers.length} already deployed`
                          : 'Use existing OS'],
                        ...(needsProvisioning ? [
                          ['OS image', images.find((image) => image.id === effectiveImageId)?.name ?? effectiveImageId],
                          ['OS addressing', effectiveNetworkMode === 'static' ? 'Static per target' : 'Automatic'],
                        ] : []),
                        ['Controllers', String(slurmControllerIds.length)],
                        ['Compute nodes', String(slurmComputeIds.length)],
                        ...(slurmHighlyAvailable ? [['Shared state', 'Swallow-provisioned (NFS)']] : []),
                        ...(slurmApiVersion.trim() ? [['slurmrestd API version', slurmApiVersion.trim()]] : []),
                      ]
                      : [
                        ['Site', state.data.sites.find((site) => site.id === effectiveSiteId)?.name ?? effectiveSiteId],
                        ['Platform type', 'Kubernetes'],
                        ['Topology', topologyPresentation(topology).label],
                        ['Machine preparation', needsProvisioning
                          ? `Provision ${readySelectedServers.length}, ${deployedSelectedServers.length} already deployed`
                          : 'Use existing OS'],
                        ...(needsProvisioning ? [
                          ['OS image', images.find((image) => image.id === effectiveImageId)?.name ?? effectiveImageId],
                          ['OS addressing', effectiveNetworkMode === 'static' ? 'Static per target' : 'Automatic'],
                        ] : []),
                        ['k0s version', k0sVersion],
                        ['GPU stack owner', gpuStackOwner],
                        ['API endpoint', topology === 'high-availability'
                          ? `${apiVip}/${apiVipPrefix} (virtual IP)`
                          : `${initialControllerAddress ?? '-'}:6443 (direct)`],
                        ['Pod CIDR', podCidr],
                        ['Service CIDR', serviceCidr],
                        ['Machines', `${assignments.length} selected`],
                        ['Roles', `${controllers} control-plane, ${workloadCount} workload-capable`],
                      ]
                    ).map(([label, value]) => (
                      <DescriptionListGroup key={label}>
                        <DescriptionListTerm>{label}</DescriptionListTerm>
                        <DescriptionListDescription>{value}</DescriptionListDescription>
                      </DescriptionListGroup>
                    ))}
                  </DescriptionList>
                </CardBody>
              </Card>
              <Alert variant={AlertVariant.info} title="Submitting creates a Platform and starts its deployment" isInline>
                You will continue on the Platform page. Open detailed Operation output only when you need automation-level troubleshooting.
              </Alert>
            </WizardSection>
          </WizardStep>
        </Wizard>
      )}
    </div>
  )
}

/** Keeps every wizard step on the same compact vertical rhythm. */
function WizardSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="sw-wizard-section">
      <Title headingLevel="h2" size="lg">{title}</Title>
      {children}
    </section>
  )
}
