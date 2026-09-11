import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Badge, Box, Card, Field, Heading, HStack, IconButton, Input, SegmentGroup, Table, Text, Textarea } from '@chakra-ui/react'
import { RefreshCcw } from 'lucide-react'
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
import { SectionHeader, StickyTableFrame } from '@/presentation/components/OperatorPrimitives'
import { Wizard, type WizardStepDef } from '@/presentation/components/Wizard'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { DescriptionList } from '@/presentation/components/ui/description-list'
import { Select } from '@/presentation/components/ui/select'
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
  return parts.length === 4 && parts.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255)
}

function validCIDR(value: string): boolean {
  const [address, prefix, ...rest] = value.split('/')
  const numericPrefix = Number(prefix)
  return rest.length === 0 && validAddress(address) && /^\d{1,2}$/.test(prefix ?? '') && numericPrefix >= 0 && numericPrefix <= 32
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
 * explanation. Durable claims take precedence because they also cover partial deployments whose
 * membership has not become observable yet.
 */
function existingPlatformAssignment(server: Server, platforms: Platform[], deploymentClaims: Record<string, Platform>): ExistingPlatformAssignment | null {
  const claim = deploymentClaims[server.id]
  if (claim) {
    const role = server.membership?.platformId === claim.id ? server.membership.role : null
    return {
      platformName: claim.name,
      platformType: claim.type,
      detail: role ? `${role} · ${platformLifecycleLabel(claim.lifecycleState)}` : platformLifecycleLabel(claim.lifecycleState),
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
 * Platform deployment workflow.
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
  const [slurmDaemons, setSlurmDaemons] = useState<Record<string, { controller: boolean; compute: boolean; login: boolean }>>({})
  // Shared workload filesystem (user/job data), distinct from controller state. "none" = no
  // shared storage; self-hosted exports from the login node; external mounts an operator NFS.
  const [slurmWorkloadMode, setSlurmWorkloadMode] = useState<'none' | 'self-hosted' | 'external'>('none')
  const [slurmWorkloadMountPath, setSlurmWorkloadMountPath] = useState('/shared')
  const [slurmWorkloadNfsUrl, setSlurmWorkloadNfsUrl] = useState('')
  const [slurmWorkloadMountOptions, setSlurmWorkloadMountOptions] = useState('')
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
    provisioning
      .listTemplates({ siteId: effectiveSiteId })
      .then((nextTemplates) => {
        if (!canceled) setTemplates(nextTemplates)
      })
      .catch((error: Error) => {
        if (!canceled) setProvisioningError(error.message)
      })
    return () => {
      canceled = true
    }
  }, [effectiveSiteId, provisioning])

  const isSlurm = platformType === 'slurm'
  const assignments = useMemo<RoleAssignment[]>(
    () =>
      Object.entries(roles)
        .filter(([, role]) => role !== 'none')
        .map(([serverId, role]) => ({
          serverId,
          role: role as NodeRole,
          ...(role === 'control-plane' && workloadControllers[serverId] ? { runWorkloads: true } : {}),
        })),
    [roles, workloadControllers],
  )
  const controllers = assignments.filter((item) => item.role === 'control-plane').length
  const workers = assignments.filter((item) => item.role === 'worker').length
  const workloadCount = workers + assignments.filter((item) => item.role === 'control-plane' && item.runWorkloads).length
  // Slurm per-daemon selection: a Server may run slurmctld, slurmd, or both. More than one
  // controller is HA; Swallow provisions the shared StateSaveLocation automatically.
  const slurmSelectedIds = Object.entries(slurmDaemons)
    .filter(([, daemons]) => daemons.controller || daemons.compute || daemons.login)
    .map(([serverId]) => serverId)
  const slurmControllerIds = Object.entries(slurmDaemons)
    .filter(([, daemons]) => daemons.controller)
    .map(([serverId]) => serverId)
  const slurmComputeIds = Object.entries(slurmDaemons)
    .filter(([, daemons]) => daemons.compute)
    .map(([serverId]) => serverId)
  const slurmLoginIds = Object.entries(slurmDaemons)
    .filter(([, daemons]) => daemons.login)
    .map(([serverId]) => serverId)
  const slurmHighlyAvailable = slurmControllerIds.length > 1
  // Workload storage validity: self-hosted needs a login node to export it; external needs an
  // NFS url shaped host:/path. "none" is always valid.
  const slurmWorkloadValid =
    slurmWorkloadMode === 'none' ||
    (slurmWorkloadMode === 'self-hosted' && slurmLoginIds.length > 0) ||
    (slurmWorkloadMode === 'external' && /^[^\s:]+:\/\S*$/.test(slurmWorkloadNfsUrl.trim()))
  const slurmTopologyValid = slurmControllerIds.length >= 1 && slurmComputeIds.length >= 1 && slurmWorkloadValid
  const basicsValid = Boolean(effectiveSiteId && name.trim() && (isSlurm || k0sVersion.trim()))
  // Locked Servers are excluded from the candidate list (see useDeployableServers), so the only
  // "unavailable" reason left to surface is an existing Platform assignment.
  const hasAssignedServers =
    state.status === 'ready' &&
    state.data.servers.some((server) => Boolean(existingPlatformAssignment(server, state.data.platforms, state.data.deploymentClaims)))
  const topologyValid =
    topology === 'standalone'
      ? assignments.length === 1 && controllers === 1 && workloadCount === 1
      : topology === 'multi-node'
        ? assignments.length >= 2 && controllers === 1 && workers >= 1
        : controllers >= 3 && controllers % 2 === 1 && workloadCount >= 1
  const networkingValid =
    validCIDR(podCidr.trim()) &&
    validCIDR(serviceCidr.trim()) &&
    (topology !== 'high-availability' || (validAddress(apiVip.trim()) && Number(apiVipPrefix) >= 1 && Number(apiVipPrefix) <= 32))

  // Kubernetes selection comes from role assignments; Slurm from the per-daemon checkboxes.
  const selectedServerIds = isSlurm ? slurmSelectedIds : assignments.map((assignment) => assignment.serverId)
  const selectedServers =
    state.status === 'ready'
      ? selectedServerIds.flatMap((serverId) => {
          const server = state.data.servers.find((candidate) => candidate.id === serverId)
          return server ? [server] : []
        })
      : []
  const selectedAddresses = selectedServers.flatMap((server) => server.addresses)
  const initialController = selectedServers.find((server) => roles[server.id] === 'control-plane')
  const initialControllerAddress = initialController ? serverPrimaryAddress(initialController) : null

  // Convergent selection (ADR 017): a Server already `deployed` is reused as-is; anything else
  // (pool-ready) is provisioned first. The machine-preparation mode is derived from this.
  const deployedSelectedServers = selectedServers.filter((server) => server.provisioning?.state === 'deployed')
  const readySelectedServers = selectedServers.filter((server) => server.provisioning?.state !== 'deployed')
  const needsProvisioning = readySelectedServers.length > 0
  // The provisioner is derived from the ready Servers rather than picked: they are provisioned
  // with one OS image, so they must share a single provisioner.
  const readyProvisionerIds = [...new Set(readySelectedServers.map((server) => server.source.integrationId))]
  const provisionerConflict = readyProvisionerIds.length > 1
  const integrationId = readyProvisionerIds.length === 1 ? readyProvisionerIds[0] : ''
  const machinesValid = (isSlurm ? slurmTopologyValid : topologyValid) && (!needsProvisioning || (!provisionerConflict && Boolean(integrationId)))
  // Slurm has no platform-level network step; its networking is only the OS provisioning step.
  const platformNetworkingValid = isSlurm ? true : networkingValid

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
        if (!canceled) setProvisioningError(error.message)
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
    if (!needsProvisioning || !preparationTargetKey) return
    let canceled = false
    provisioning
      .inspectDeploymentNetworks(preparationTargetKey.split(','))
      .then((inspection) => {
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
      })
      .catch((error: Error) => {
        if (!canceled) setProvisioningError(error.message)
      })
      .finally(() => {
        if (!canceled) setProvisioningLoading(false)
      })
    return () => {
      canceled = true
    }
  }, [needsProvisioning, preparationTargetKey, provisioning])

  // Ignore a template left selected from a previously derived provisioner so it can never leak
  // another provisioner's image/network into this deploy.
  const selectedTemplate = templates.find((template) => template.id === templateId && template.integrationId === integrationId)
  const effectiveImageId = selectedTemplate?.imageId ?? imageId
  const effectiveEphemeral = selectedTemplate?.ephemeral ?? ephemeral
  const effectiveNetworkMode = selectedTemplate?.network.mode ?? networkMode
  const inspectedTargetKey = networkInspection?.targets.map((target) => target.serverId).sort().join(',') ?? ''
  const networkAssignmentsValid =
    !needsProvisioning ||
    (networkInspection !== null &&
      inspectedTargetKey === preparationTargetKey &&
      readySelectedServers.every((server) => {
        const assignment = networkAssignments[server.id]
        const target = networkInspection.targets.find((item) => item.serverId === server.id)
        const selectedInterface = target?.network.interfaces.find((item) => item.id === assignment?.interfaceId)
        if (!assignment?.interfaceId || !assignment.subnetId || !selectedInterface?.availableSubnets.some((subnet) => subnet.id === assignment.subnetId)) return false
        return effectiveNetworkMode !== 'static' || validAddress(assignment.ipAddress)
      }))
  const osConfigurationValid = !needsProvisioning || Boolean(integrationId && effectiveImageId && networkAssignmentsValid && !provisioningError)

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
    setSlurmWorkloadMode('none')
    setSlurmWorkloadMountPath('/shared')
    setSlurmWorkloadNfsUrl('')
    setSlurmWorkloadMountOptions('')
    setTemplateId('')
    setImageId('')
    setNetworkInspection(null)
    setNetworkAssignments({})
    if (next === 'slurm') setGPUStackOwner('provisioning')
  }

  // Toggle one Slurm role on a node. A node keeps whichever roles are checked; a node with none
  // is simply not part of the cluster. Login is a submission/client host (no cluster daemon).
  const toggleSlurmDaemon = (serverId: string, daemon: 'controller' | 'compute' | 'login', checked: boolean) => {
    const server = state.status === 'ready' ? state.data.servers.find((candidate) => candidate.id === serverId) : undefined
    if (server?.provisioning?.locked) return
    setSlurmDaemons((current) => {
      const existing = current[serverId] ?? { controller: false, compute: false, login: false }
      return { ...current, [serverId]: { ...existing, [daemon]: checked } }
    })
  }

  const changeRole = (serverId: string, next: RoleChoice) => {
    const server = state.status === 'ready' ? state.data.servers.find((candidate) => candidate.id === serverId) : undefined
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
    // The mode is derived from the selection, not chosen: reuse existing-OS Servers when nothing
    // needs provisioning, otherwise converge — provision the ready Servers and reuse deployed.
    const machinePreparation: PlatformMachinePreparation = !needsProvisioning
      ? { mode: 'existing_os' }
      : {
          mode: 'provision_os',
          templateId: templateId || undefined,
          settings: templateId ? undefined : { imageId: effectiveImageId, ephemeral: effectiveEphemeral },
          userData: templateId ? { mode: 'inherit' } : cloudInit.trim() ? { mode: 'replace', value: cloudInit } : { mode: 'omit' },
          network: {
            mode: effectiveNetworkMode,
            subnetId: selectedTemplate?.network.subnetId,
            defaultGateway: selectedTemplate?.network.defaultGateway ?? defaultGateway,
            assignments: readySelectedServers.map((server) => ({
              serverId: server.id,
              interfaceId: networkAssignments[server.id]?.interfaceId ?? '',
              subnetId: networkAssignments[server.id]?.subnetId,
              ipAddress: effectiveNetworkMode === 'static' ? networkAssignments[server.id]?.ipAddress.trim() : undefined,
            })),
          },
        }
    try {
      const result = await platforms.deployPlatform(
        isSlurm
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
                  login: Boolean(slurmDaemons[serverId]?.login),
                })),
                // Optional shared workload filesystem; omitted when "none".
                workloadStorage:
                  slurmWorkloadMode === 'none'
                    ? undefined
                    : {
                        mode: slurmWorkloadMode,
                        type: 'nfs',
                        mountPath: slurmWorkloadMountPath.trim() || '/shared',
                        nfs: slurmWorkloadMode === 'external' ? { url: slurmWorkloadNfsUrl.trim(), mountOptions: slurmWorkloadMountOptions.trim() || undefined } : undefined,
                      },
              },
              machinePreparation,
            }
          : {
              siteId: effectiveSiteId,
              name: name.trim(),
              type: 'kubernetes',
              gpuStackOwner,
              k0sVersion: k0sVersion.trim(),
              ...(topology === 'high-availability' ? { apiVip: apiVip.trim(), apiVipPrefix: Number(apiVipPrefix) } : {}),
              podCidr: podCidr.trim(),
              serviceCidr: serviceCidr.trim(),
              roleAssignments: assignments,
              machinePreparation,
            },
      )
      showToast({
        title: 'Platform deployment started',
        description: 'Lifecycle and membership will update on the Platform page. Detailed automation output remains available when troubleshooting.',
        tone: 'success',
      })
      navigate(scopedHref(`/platforms/${result.platformId}`))
    } catch (error) {
      showToast({ title: 'Deployment failed', description: error instanceof Error ? error.message : 'Could not start the deployment.', tone: 'error' })
      setSubmitting(false)
    }
  }

  const steps: WizardStepDef[] = []
  if (state.status === 'ready') {
    steps.push({
      id: 'deploy-basics',
      name: 'Basics',
      canProceed: basicsValid,
      content: (
        <WizardSection title="Platform identity">
          <div className="sw-form-grid">
            <Field.Root required>
              <Field.Label>Platform type</Field.Label>
              <Select
                value={platformType}
                aria-label="Platform type"
                onChange={(value) => changeType(value as PlatformType)}
                options={[
                  { value: 'kubernetes', label: 'Kubernetes' },
                  { value: 'slurm', label: 'Slurm' },
                ]}
              />
            </Field.Root>
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
            </Field.Root>
            <Field.Root required>
              <Field.Label>Platform name</Field.Label>
              <Input value={name} onChange={(event) => setName(event.target.value)} placeholder={isSlurm ? 'lab-slurm' : 'lab-k0s'} />
            </Field.Root>
            {!isSlurm && (
              <Field.Root required>
                <Field.Label>GPU stack owner</Field.Label>
                <Select
                  value={gpuStackOwner}
                  aria-label="GPU stack owner"
                  onChange={(value) => setGPUStackOwner(value as GPUStackOwner)}
                  options={[
                    { value: 'provisioning', label: 'Provisioning' },
                    { value: 'gpu-operator', label: 'GPU Operator' },
                  ]}
                />
              </Field.Root>
            )}
            {!isSlurm && (
              <Field.Root required>
                <Field.Label>k0s version</Field.Label>
                <Input value={k0sVersion} onChange={(event) => setK0sVersion(event.target.value)} />
              </Field.Root>
            )}
            {isSlurm && (
              <Field.Root>
                <Field.Label>Cluster name</Field.Label>
                <Input value={clusterName} onChange={(event) => setClusterName(event.target.value)} placeholder={name.trim() || 'lab-slurm'} />
                <Field.HelperText>Slurm ClusterName; defaults to a sanitized platform name.</Field.HelperText>
              </Field.Root>
            )}
            {isSlurm && (
              <Field.Root>
                <Field.Label>slurmrestd API version</Field.Label>
                <Input value={slurmApiVersion} onChange={(event) => setSlurmApiVersion(event.target.value)} placeholder="auto-detect (e.g. v0.0.42)" />
                <Field.HelperText>Optional. Leave blank to let the deployment detect the endpoint version.</Field.HelperText>
              </Field.Root>
            )}
          </div>
        </WizardSection>
      ),
    })

    steps.push({
      id: 'deploy-machines',
      name: 'Machines',
      canProceed: machinesValid,
      content: (
        <WizardSection title={isSlurm ? 'Nodes and daemons' : 'Topology and machines'}>
          {!isSlurm && (
            <>
              <Field.Root required>
                <Field.Label>Topology</Field.Label>
                <Select
                  value={topology}
                  aria-label="Topology"
                  onChange={(value) => changeTopology(value as TopologyChoice)}
                  options={[
                    { value: 'standalone', label: 'Standalone (single Server)' },
                    { value: 'multi-node', label: 'Multi-node (non-HA)' },
                    { value: 'high-availability', label: 'High availability' },
                  ]}
                />
              </Field.Root>
              <Alert status={topology === 'high-availability' ? 'info' : 'warning'} title={topologyPresentation(topology).label}>
                {topologyPresentation(topology).guidance}
              </Alert>
              <HStack gap="2" wrap="wrap">
                <Badge colorPalette={controllers > 0 ? 'green' : 'orange'} variant="subtle">{controllers} control-plane</Badge>
                <Badge colorPalette={workloadCount > 0 ? 'green' : 'orange'} variant="subtle">{workloadCount} workload-capable</Badge>
                <Badge colorPalette={assignments.length > 0 ? 'blue' : 'gray'} variant="subtle">{assignments.length} selected</Badge>
              </HStack>
            </>
          )}
          {isSlurm && (
            <>
              <Alert status="info" title="Assign Slurm roles per node">
                A node may run the controller daemon (slurmctld), the compute daemon (slurmd), both, or be a login (submission) host. At least one controller
                and one compute node are required. A login node also hosts the shared storage for HA and self-hosted workload storage.
              </Alert>
              <HStack gap="2" wrap="wrap">
                <Badge colorPalette={slurmControllerIds.length > 0 ? 'green' : 'orange'} variant="subtle">{slurmControllerIds.length} controller</Badge>
                <Badge colorPalette={slurmComputeIds.length > 0 ? 'green' : 'orange'} variant="subtle">{slurmComputeIds.length} compute</Badge>
                <Badge colorPalette={slurmLoginIds.length > 0 ? 'blue' : 'gray'} variant="subtle">{slurmLoginIds.length} login</Badge>
                <Badge colorPalette={slurmSelectedIds.length > 0 ? 'blue' : 'gray'} variant="subtle">{slurmSelectedIds.length} selected</Badge>
              </HStack>
              {slurmHighlyAvailable && (
                <Alert status="info" title="Shared controller state is provisioned automatically">
                  Multiple controllers require one shared StateSaveLocation. Swallow sets it up for you — a managed NFS export on a selected node, mounted on
                  every controller — so there is nothing to enter here. (Lab-grade: the export host is a single storage failure domain.)
                </Alert>
              )}
              <Field.Root>
                <Field.Label>Shared workload storage</Field.Label>
                <Select
                  value={slurmWorkloadMode}
                  aria-label="Shared workload storage mode"
                  onChange={(value) => setSlurmWorkloadMode(value as 'none' | 'self-hosted' | 'external')}
                  options={[
                    { value: 'none', label: 'None (jobs stage their own data)' },
                    { value: 'self-hosted', label: 'Self-hosted (NFS exported from the login node)' },
                    { value: 'external', label: 'External (operator-provided NFS)' },
                  ]}
                />
                <Field.HelperText>
                  Optional shared filesystem for user/job data, mounted on every node. Distinct from controller state. Self-hosted requires a login node.
                </Field.HelperText>
              </Field.Root>
              {slurmWorkloadMode !== 'none' && (
                <>
                  <Field.Root>
                    <Field.Label>Storage type</Field.Label>
                    <Select value="nfs" disabled aria-label="Workload storage type" onChange={() => undefined} options={[{ value: 'nfs', label: 'NFS' }]} />
                  </Field.Root>
                  <Field.Root>
                    <Field.Label>Mount path</Field.Label>
                    <Input value={slurmWorkloadMountPath} onChange={(event) => setSlurmWorkloadMountPath(event.target.value)} placeholder="/shared" />
                    <Field.HelperText>Mounted on every node. Must not be /home (that would hide the SSH user's home).</Field.HelperText>
                  </Field.Root>
                  {slurmWorkloadMode === 'self-hosted' && slurmLoginIds.length === 0 && (
                    <Alert status="warning" title="Self-hosted workload storage needs a login node">
                      Assign the Login role to one node above; it will export the shared filesystem.
                    </Alert>
                  )}
                  {slurmWorkloadMode === 'external' && (
                    <>
                      <Field.Root required invalid={Boolean(slurmWorkloadNfsUrl.trim()) && !/^[^\s:]+:\/\S*$/.test(slurmWorkloadNfsUrl.trim())}>
                        <Field.Label>NFS URL</Field.Label>
                        <Input value={slurmWorkloadNfsUrl} onChange={(event) => setSlurmWorkloadNfsUrl(event.target.value)} placeholder="nfs-server:/export/data" />
                        <Field.HelperText>Form host:/path, for example 10.0.0.9:/export/data.</Field.HelperText>
                        <Field.ErrorText>Form host:/path, for example 10.0.0.9:/export/data.</Field.ErrorText>
                      </Field.Root>
                      <Field.Root>
                        <Field.Label>Mount options (optional)</Field.Label>
                        <Input value={slurmWorkloadMountOptions} onChange={(event) => setSlurmWorkloadMountOptions(event.target.value)} placeholder="rw,_netdev,hard,timeo=600,retrans=2" />
                      </Field.Root>
                    </>
                  )}
                </>
              )}
            </>
          )}
          {hasAssignedServers && (
            <Alert status="warning" title="Some Servers are already assigned">
              Assigned Servers remain visible for context. Remove the existing Platform assignment before selecting a role.
            </Alert>
          )}
          {provisionerConflict && (
            <Alert status="warning" title="Ready Servers span multiple provisioners">
              Servers that still need an OS must share one provisioner so a single OS image applies. Deselect ready Servers from other provisioners, or include
              only already-deployed Servers from them.
            </Alert>
          )}
          {state.data.servers.length === 0 ? (
            <EmptyState title="No deployable Servers" message="This Site has no Ready or Deployed Servers to build a Platform from." />
          ) : (
            <StickyTableFrame>
              <Table.Root size="sm" aria-label="Deployable Servers" className="sw-deploy-machines-table">
                <Table.Header>
                  <Table.Row>
                    <Table.ColumnHeader>Server</Table.ColumnHeader>
                    <Table.ColumnHeader>Address</Table.ColumnHeader>
                    <Table.ColumnHeader>OS state</Table.ColumnHeader>
                    <Table.ColumnHeader>Current assignment</Table.ColumnHeader>
                    {isSlurm ? (
                      <>
                        <Table.ColumnHeader>Controller</Table.ColumnHeader>
                        <Table.ColumnHeader>Compute</Table.ColumnHeader>
                        <Table.ColumnHeader>Login</Table.ColumnHeader>
                      </>
                    ) : (
                      <>
                        <Table.ColumnHeader>Role</Table.ColumnHeader>
                        <Table.ColumnHeader>Runs workloads</Table.ColumnHeader>
                      </>
                    )}
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {state.data.servers.map((server) => {
                    const existing = existingPlatformAssignment(server, state.data.platforms, state.data.deploymentClaims)
                    const deployed = server.provisioning?.state === 'deployed'
                    const unavailable = Boolean(existing)
                    const role = unavailable ? 'none' : roles[server.id] ?? 'none'
                    const daemons = slurmDaemons[server.id] ?? { controller: false, compute: false, login: false }
                    const willProvision = isSlurm ? daemons.controller || daemons.compute || daemons.login : role !== 'none'
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
                        {isSlurm ? (
                          <>
                            <Table.Cell>
                              <Checkbox
                                id={`slurm-controller-${server.id}`}
                                aria-label={existing ? `slurmctld on ${serverDisplayName(server)}, unavailable because it is assigned to ${existing.platformName}` : `Run slurmctld on ${serverDisplayName(server)}`}
                                checked={daemons.controller}
                                disabled={unavailable}
                                onCheckedChange={(checked) => toggleSlurmDaemon(server.id, 'controller', checked)}
                              />
                            </Table.Cell>
                            <Table.Cell>
                              <Checkbox
                                id={`slurm-compute-${server.id}`}
                                aria-label={existing ? `slurmd on ${serverDisplayName(server)}, unavailable because it is assigned to ${existing.platformName}` : `Run slurmd on ${serverDisplayName(server)}`}
                                checked={daemons.compute}
                                disabled={unavailable}
                                onCheckedChange={(checked) => toggleSlurmDaemon(server.id, 'compute', checked)}
                              />
                            </Table.Cell>
                            <Table.Cell>
                              <Checkbox
                                id={`slurm-login-${server.id}`}
                                aria-label={existing ? `login node on ${serverDisplayName(server)}, unavailable because it is assigned to ${existing.platformName}` : `Make ${serverDisplayName(server)} a login (submission) host`}
                                checked={daemons.login}
                                disabled={unavailable}
                                onCheckedChange={(checked) => toggleSlurmDaemon(server.id, 'login', checked)}
                              />
                            </Table.Cell>
                          </>
                        ) : (
                          <>
                            <Table.Cell>
                              <Select
                                aria-label={existing ? `Role for ${serverDisplayName(server)}, unavailable because it is assigned to ${existing.platformName}` : `Role for ${serverDisplayName(server)}`}
                                value={role}
                                disabled={unavailable}
                                size="sm"
                                onChange={(value) => changeRole(server.id, value as RoleChoice)}
                                options={[
                                  { value: 'none', label: 'Not included' },
                                  { value: 'control-plane', label: topology === 'standalone' ? 'Standalone node' : 'Control-plane' },
                                  ...(topology !== 'standalone' ? [{ value: 'worker', label: 'Worker' }] : []),
                                ]}
                              />
                            </Table.Cell>
                            <Table.Cell>
                              {role === 'control-plane' ? (
                                <Checkbox
                                  id={`platform-workload-${server.id}`}
                                  aria-label={`Run workloads on ${serverDisplayName(server)}`}
                                  checked={topology === 'standalone' || Boolean(workloadControllers[server.id])}
                                  disabled={topology === 'standalone'}
                                  onCheckedChange={(checked) => setWorkloadControllers((current) => ({ ...current, [server.id]: checked }))}
                                />
                              ) : role === 'worker' ? (
                                <Badge colorPalette="green" variant="subtle">Yes</Badge>
                              ) : (
                                '-'
                              )}
                            </Table.Cell>
                          </>
                        )}
                      </Table.Row>
                    )
                  })}
                </Table.Body>
              </Table.Root>
            </StickyTableFrame>
          )}
        </WizardSection>
      ),
    })

    if (needsProvisioning) {
      steps.push({
        id: 'deploy-operating-system',
        name: 'Operating system',
        canProceed: osConfigurationValid,
        content: (
          <WizardSection title="Operating system configuration">
            <Alert status="info" title={`Preparing ${readySelectedServers.length} Server${readySelectedServers.length === 1 ? '' : 's'} that still need an operating system`}>
              {deployedSelectedServers.length > 0
                ? `${deployedSelectedServers.length} already-deployed Server${deployedSelectedServers.length === 1 ? '' : 's'} in this deployment are used as-is and skip these settings.`
                : 'These settings apply to every selected Server.'}
            </Alert>
            {provisioningError && (
              <Alert status="error" title="Provisioning data is unavailable">
                {provisioningError}
              </Alert>
            )}
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
              <Field.Root required>
                <Field.Label>OS image</Field.Label>
                <HStack gap="2" align="stretch">
                  <Box flex="1">
                    <Select
                      id="platform-os-image"
                      aria-label="OS image"
                      value={effectiveImageId}
                      placeholder="Select an OS image"
                      options={images.map((image) => ({ value: image.id, label: `${image.name} - ${image.architecture} (${image.osSystem} ${image.release})` }))}
                      required
                      disabled={Boolean(selectedTemplate)}
                      onChange={setImageId}
                    />
                  </Box>
                  <IconButton
                    variant="outline"
                    aria-label="Refresh OS images"
                    loading={provisioningLoading}
                    onClick={() => {
                      if (!integrationId) return
                      setProvisioningLoading(true)
                      setProvisioningError('')
                      provisioning
                        .listOSImages(integrationId)
                        .then(setImages)
                        .catch((error: Error) => setProvisioningError(error.message))
                        .finally(() => setProvisioningLoading(false))
                    }}
                  >
                    <RefreshCcw size={16} />
                  </IconButton>
                </HStack>
              </Field.Root>
              <Field.Root>
                <Checkbox id="platform-ephemeral" checked={effectiveEphemeral} disabled={Boolean(selectedTemplate)} onCheckedChange={(checked) => setEphemeral(checked)}>
                  Run the operating system from memory
                </Checkbox>
                <Field.HelperText>Disks remain untouched and operating-system changes are lost after reboot.</Field.HelperText>
              </Field.Root>
              {!selectedTemplate && (
                <Field.Root>
                  <Field.Label>Cloud-init user data</Field.Label>
                  <Textarea value={cloudInit} onChange={(event) => setCloudInit(event.target.value)} rows={7} autoComplete="off" placeholder="#cloud-config" />
                </Field.Root>
              )}
            </div>

            <section className="sw-section">
              <SectionHeader title="Network configuration" description="Swallow configures each boot interface before the operating system deployment starts." />
              <div className="sw-section-body">
                <div className="sw-form-grid">
                  <Field.Root required>
                    <Field.Label>Addressing mode</Field.Label>
                    <SegmentGroup.Root
                      value={effectiveNetworkMode}
                      disabled={Boolean(selectedTemplate)}
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
                {!networkInspection && !provisioningLoading && <Alert status="warning" title="Select machines to inspect their network interfaces" />}
                {networkInspection && !networkAssignmentsValid && (
                  <Alert status="warning" title="Complete every network assignment">
                    Select one compatible interface and subnet for every Server. Static mode also requires an IPv4 address per target.
                  </Alert>
                )}
              </div>
              {networkInspection && (
                <StickyTableFrame>
                  <Table.Root size="sm" aria-label="Platform operating system network assignments" className="sw-network-assignment-table">
                    <Table.Header>
                      <Table.Row>
                        <Table.ColumnHeader>Server</Table.ColumnHeader>
                        <Table.ColumnHeader>Interface</Table.ColumnHeader>
                        <Table.ColumnHeader>Subnet</Table.ColumnHeader>
                        {effectiveNetworkMode === 'static' && <Table.ColumnHeader>Static IPv4 address</Table.ColumnHeader>}
                        <Table.ColumnHeader>Current mode</Table.ColumnHeader>
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {networkInspection.targets.map((target) => {
                        const server = selectedServers.find((item) => item.id === target.serverId)
                        const assignment = networkAssignments[target.serverId] ?? { interfaceId: '', subnetId: '', ipAddress: '' }
                        const iface = target.network.interfaces.find((item) => item.id === assignment.interfaceId)
                        const currentMode = iface?.rawProviderMode === 'AUTO' ? 'Provider-managed (MAAS AUTO)' : iface?.rawProviderMode || '-'
                        return (
                          <Table.Row key={target.serverId}>
                            <Table.Cell>
                              <strong>{server ? serverDisplayName(server) : target.serverId}</strong>
                            </Table.Cell>
                            <Table.Cell>
                              <Select
                                aria-label={`Interface for ${server ? serverDisplayName(server) : target.serverId}`}
                                value={assignment.interfaceId}
                                size="sm"
                                placeholder="Select an interface"
                                onChange={(value) => {
                                  const nextInterface = target.network.interfaces.find((item) => item.id === value)
                                  const subnetId = nextInterface?.availableSubnets.some((subnet) => subnet.id === assignment.subnetId)
                                    ? assignment.subnetId
                                    : nextInterface?.availableSubnets.length === 1
                                      ? nextInterface.availableSubnets[0].id
                                      : ''
                                  setNetworkAssignments((current) => ({ ...current, [target.serverId]: { ...assignment, interfaceId: value, subnetId } }))
                                }}
                                options={target.network.interfaces.map((item) => ({
                                  value: item.id,
                                  label: `${item.name} - ${item.macAddress}${item.boot ? ' (boot NIC)' : ''}`,
                                }))}
                              />
                            </Table.Cell>
                            <Table.Cell>
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
                              <Table.Cell>
                                <Input
                                  aria-label={`Static IPv4 address for ${server ? serverDisplayName(server) : target.serverId}`}
                                  value={assignment.ipAddress}
                                  onChange={(event) => setNetworkAssignments((current) => ({ ...current, [target.serverId]: { ...assignment, ipAddress: event.target.value } }))}
                                  placeholder="192.0.2.10"
                                />
                              </Table.Cell>
                            )}
                            <Table.Cell title={currentMode}>{currentMode}</Table.Cell>
                          </Table.Row>
                        )
                      })}
                    </Table.Body>
                  </Table.Root>
                </StickyTableFrame>
              )}
            </section>
          </WizardSection>
        ),
      })
    }

    if (!isSlurm) {
      steps.push({
        id: 'deploy-networking',
        name: 'Networking',
        canProceed: networkingValid,
        content: (
          <WizardSection title="Platform network">
            <section className="sw-network-context" aria-labelledby="selected-machine-addresses">
              <Heading as="h3" size="sm" id="selected-machine-addresses">
                Selected machine addresses
              </Heading>
              <HStack gap="2" wrap="wrap" aria-label="Selected machine addresses">
                {selectedServers.map((server) => (
                  <Badge key={server.id} colorPalette="gray" variant="subtle">
                    {serverDisplayName(server)}: {serverPrimaryAddress(server) ?? '-'}
                  </Badge>
                ))}
              </HStack>
              <Text className="sw-muted">{selectedNetworkGuidance(selectedAddresses)}</Text>
            </section>
            {topology === 'high-availability' ? (
              <Alert status="info" title="Allocate one unused virtual IP reachable by every selected machine">
                Swallow cannot verify DHCP reservations. Confirm the address is unused and outside dynamic ranges before deployment.
              </Alert>
            ) : (
              <Alert status="warning" title="Direct control-plane endpoint">
                The Kubernetes API will use {initialControllerAddress ?? 'the selected control-plane address'}:6443. This topology has no endpoint failover.
              </Alert>
            )}
            <div className="sw-form-grid">
              {topology === 'high-availability' && (
                <>
                  <Field.Root required invalid={Boolean(apiVip) && !validAddress(apiVip)}>
                    <Field.Label>API virtual IP</Field.Label>
                    <Input value={apiVip} onChange={(event) => setAPIVip(event.target.value)} placeholder="192.168.40.200" />
                    <Field.HelperText>Enter an unused IPv4 address on the machine network.</Field.HelperText>
                    <Field.ErrorText>Enter an unused IPv4 address on the machine network.</Field.ErrorText>
                  </Field.Root>
                  <Field.Root required>
                    <Field.Label>VIP prefix length</Field.Label>
                    <Input type="number" value={apiVipPrefix} onChange={(event) => setAPIVipPrefix(event.target.value)} min={1} max={32} />
                  </Field.Root>
                </>
              )}
              <Field.Root required invalid={Boolean(podCidr) && !validCIDR(podCidr)}>
                <Field.Label>Pod CIDR</Field.Label>
                <Input value={podCidr} onChange={(event) => setPodCidr(event.target.value)} />
                <Field.HelperText>Default {DEFAULT_POD_CIDR}; must not overlap selected machine addresses.</Field.HelperText>
                <Field.ErrorText>Default {DEFAULT_POD_CIDR}; must not overlap selected machine addresses.</Field.ErrorText>
              </Field.Root>
              <Field.Root required invalid={Boolean(serviceCidr) && !validCIDR(serviceCidr)}>
                <Field.Label>Service CIDR</Field.Label>
                <Input value={serviceCidr} onChange={(event) => setServiceCidr(event.target.value)} />
                <Field.HelperText>Default {DEFAULT_SERVICE_CIDR}; must not overlap selected machine addresses.</Field.HelperText>
                <Field.ErrorText>Default {DEFAULT_SERVICE_CIDR}; must not overlap selected machine addresses.</Field.ErrorText>
              </Field.Root>
            </div>
          </WizardSection>
        ),
      })
    }

    steps.push({
      id: 'deploy-review',
      name: 'Review',
      canProceed: basicsValid && machinesValid && osConfigurationValid && platformNetworkingValid && !submitting,
      content: (
        <WizardSection title="Review deployment">
          <Card.Root size="sm">
            <Card.Body gap="3">
              <Heading size="sm">{name}</Heading>
              <DescriptionList
                items={(isSlurm
                  ? [
                      ['Site', state.data.sites.find((site) => site.id === effectiveSiteId)?.name ?? effectiveSiteId],
                      ['Platform type', 'Slurm'],
                      ['Cluster name', clusterName.trim() || name.trim()],
                      ['Machine preparation', needsProvisioning ? `Provision ${readySelectedServers.length}, ${deployedSelectedServers.length} already deployed` : 'Use existing OS'],
                      ...(needsProvisioning
                        ? [
                            ['OS image', images.find((image) => image.id === effectiveImageId)?.name ?? effectiveImageId],
                            ['OS addressing', effectiveNetworkMode === 'static' ? 'Static per target' : 'Automatic'],
                          ]
                        : []),
                      ['Controllers', String(slurmControllerIds.length)],
                      ['Compute nodes', String(slurmComputeIds.length)],
                      ...(slurmLoginIds.length > 0 ? [['Login nodes', String(slurmLoginIds.length)]] : []),
                      ...(slurmHighlyAvailable ? [['Shared state', 'Swallow-provisioned (NFS)']] : []),
                      ...(slurmWorkloadMode === 'self-hosted'
                        ? [['Workload storage', `Self-hosted NFS at ${slurmWorkloadMountPath.trim() || '/shared'}`]]
                        : slurmWorkloadMode === 'external'
                          ? [['Workload storage', `External ${slurmWorkloadNfsUrl.trim()} at ${slurmWorkloadMountPath.trim() || '/shared'}`]]
                          : []),
                      ...(slurmApiVersion.trim() ? [['slurmrestd API version', slurmApiVersion.trim()]] : []),
                    ]
                  : [
                      ['Site', state.data.sites.find((site) => site.id === effectiveSiteId)?.name ?? effectiveSiteId],
                      ['Platform type', 'Kubernetes'],
                      ['Topology', topologyPresentation(topology).label],
                      ['Machine preparation', needsProvisioning ? `Provision ${readySelectedServers.length}, ${deployedSelectedServers.length} already deployed` : 'Use existing OS'],
                      ...(needsProvisioning
                        ? [
                            ['OS image', images.find((image) => image.id === effectiveImageId)?.name ?? effectiveImageId],
                            ['OS addressing', effectiveNetworkMode === 'static' ? 'Static per target' : 'Automatic'],
                          ]
                        : []),
                      ['k0s version', k0sVersion],
                      ['GPU stack owner', gpuStackOwner],
                      ['API endpoint', topology === 'high-availability' ? `${apiVip}/${apiVipPrefix} (virtual IP)` : `${initialControllerAddress ?? '-'}:6443 (direct)`],
                      ['Pod CIDR', podCidr],
                      ['Service CIDR', serviceCidr],
                      ['Machines', `${assignments.length} selected`],
                      ['Roles', `${controllers} control-plane, ${workloadCount} workload-capable`],
                    ]
                ).map(([label, value]) => ({ label: String(label), value }))}
              />
            </Card.Body>
          </Card.Root>
          <Alert status="info" title="Submitting creates a Platform and starts its deployment">
            You will continue on the Platform page. Open detailed Operation output only when you need automation-level troubleshooting.
          </Alert>
        </WizardSection>
      ),
    })
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
      {state.status === 'ready' && <Wizard steps={steps} onFinish={() => void deploy()} finishLabel="Deploy platform" finishing={submitting} />}
    </div>
  )
}
