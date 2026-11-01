import { useMemo, useState } from 'react'
import { Badge, Button, Field, HStack, Input } from '@chakra-ui/react'
import { hasAMDComputeGPU } from '@/domain/platform/gpuOperator'
import { formatGPUDriverModuleOptions, isValidGPUGresType, parseGPUDriverModuleOptions } from '@/domain/platform/gpuDriver'
import { evaluateMinimumResourceEligibility, formatMinimumResources } from '@/domain/platform/resourceEligibility'
import type { ObservabilityInstallMethod, SlurmContainerRuntime } from '@/domain/platform/types'
import { serverDisplayName, type Server } from '@/domain/server/types'
import type { PlatformDeployContext, PlatformDeployForm } from '@/modules/types'
import { Alert } from '@/presentation/components/ui/alert'
import { Checkbox } from '@/presentation/components/ui/checkbox'
import { AdvancedSettings } from '@/presentation/pages/platforms/AdvancedSettings'
import { SlurmDeploymentFeatures, type GPUGresTypeMode, type SlurmMonitoringChoice } from './SlurmDeploymentFeatures'
import { useSlurmDeploymentRequirement } from './useSlurmDeploymentRequirement'

type SlurmDaemon = 'controller' | 'compute' | 'login'

const NO_DAEMONS = { controller: false, compute: false, login: false }

/** Display name of a Slurm container runtime choice; the domain value is what the request sends. */
function slurmContainerRuntimeLabel(runtime: SlurmContainerRuntime): string {
  return runtime === 'podman' ? 'Podman' : 'Docker CE'
}

/**
 * The Slurm deploy form: per-node daemons gated by the global minimum-resource policy, the optional
 * Slurm features, and the cluster's advanced identity. The policy loads fail-closed, so node
 * selection waits until it is known.
 */
export function useSlurmDeployForm({ platformName, servers, heldBy }: PlatformDeployContext): PlatformDeployForm {
  const { state: requirementState, reload: reloadRequirement } = useSlurmDeploymentRequirement()
  const [clusterName, setClusterName] = useState('')
  const [apiVersion, setAPIVersion] = useState('')
  // Node roles are per-daemon (a Server may run slurmctld, slurmd, or both).
  const [daemons, setDaemons] = useState<Record<string, { controller: boolean; compute: boolean; login: boolean }>>({})
  // Shared workload filesystem (user/job data), distinct from controller state. "none" = no
  // shared storage; self-hosted exports from the login node; external mounts an operator NFS.
  const [workloadMode, setWorkloadMode] = useState<'none' | 'self-hosted' | 'external'>('none')
  const [workloadMountPath, setWorkloadMountPath] = useState('/shared')
  const [workloadNfsUrl, setWorkloadNfsUrl] = useState('')
  const [workloadMountOptions, setWorkloadMountOptions] = useState('')
  // Optional containers (decisions 063 and 066), one card with two independent choices that are sent
  // only while the card is on: native OCI containers (slurm.ociContainers) and at most one container
  // engine, Docker CE or Podman, on every compute and login node (containerRuntime; '' installs none).
  // Off by default because Slurm needs neither; turning the card on starts with native OCI only.
  const [containersEnabled, setContainersEnabled] = useState(false)
  const [containerRuntime, setContainerRuntime] = useState<SlurmContainerRuntime | ''>('')
  const [ociContainers, setOCIContainers] = useState(false)
  // Optional GPU monitoring (decisions 063 and 066): the Device Metrics Exporter (per-GPU, jobs share
  // nodes) or Omnistat (one job per node), never both, each with Prometheus and Grafana on the
  // primary controller by the chosen install method. Off by default because it adds software there.
  const [monitoring, setMonitoring] = useState<SlurmMonitoringChoice>('none')
  const [monitoringInstallMethod, setMonitoringInstallMethod] = useState<ObservabilityInstallMethod>('container')
  // Optional host MPI (decision 066); off by default because it installs packages on every compute
  // and login node.
  const [pmix, setPMIx] = useState(false)
  // GPU node settings (platforms.md "GPU GRES" and "GPU driver options"): the GRES type is derived
  // from the GPU architecture unless named, and topology-required driver options apply regardless.
  const [gresTypeMode, setGresTypeMode] = useState<GPUGresTypeMode>('automatic')
  const [gresType, setGresType] = useState('')
  const [gpuDriverOptionsText, setGPUDriverOptionsText] = useState('')

  const minimumResources = requirementState.status === 'ready' ? requirementState.requirement.minimumResources : null
  const eligibilityByServerId = useMemo<Record<string, ReturnType<typeof evaluateMinimumResourceEligibility>>>(() => {
    if (requirementState.status !== 'ready') return {}
    return Object.fromEntries(servers.map((server) => [server.id, evaluateMinimumResourceEligibility(server.status, minimumResources)]))
  }, [servers, requirementState.status, minimumResources])
  const eligible = (serverId: string) => Boolean(eligibilityByServerId[serverId]?.eligible)
  // More than one controller is HA; Swallow provisions the shared StateSaveLocation automatically.
  const selectedIds = Object.entries(daemons)
    .filter(([serverId, roles]) => (roles.controller || roles.compute || roles.login) && eligible(serverId))
    .map(([serverId]) => serverId)
  const idsRunning = (daemon: SlurmDaemon) => selectedIds.filter((serverId) => daemons[serverId]?.[daemon])
  const controllerIds = idsRunning('controller')
  const computeIds = idsRunning('compute')
  const loginIds = idsRunning('login')
  const highlyAvailable = controllerIds.length > 1
  // Workload storage validity: self-hosted needs a login node to export it; external needs an
  // NFS url shaped host:/path. "none" is always valid.
  const workloadValid =
    workloadMode === 'none' ||
    (workloadMode === 'self-hosted' && loginIds.length > 0) ||
    (workloadMode === 'external' && /^[^\s:]+:\/\S*$/.test(workloadNfsUrl.trim()))
  const topologyValid = controllerIds.length >= 1 && computeIds.length >= 1 && workloadValid
  const gresTypeInvalid = gresTypeMode === 'custom' && !isValidGPUGresType(gresType.trim())
  const gpuDriverOptions = parseGPUDriverModuleOptions(gpuDriverOptionsText)
  const gpuSettingsValid = !gresTypeInvalid && gpuDriverOptions.errors.length === 0
  // The Device Metrics Exporter reports only GPUs, so the backend refuses it without a compute
  // Server whose GPU Inventory lists AMD compute GPUs; the wizard blocks the step for the same rule.
  const hasGPUComputeNode = computeIds.some((serverId) => {
    const server = servers.find((candidate) => candidate.id === serverId)
    return Boolean(server && hasAMDComputeGPU(server.status.gpus))
  })
  const monitoringValid = monitoring !== 'device-metrics-exporter' || hasGPUComputeNode
  // What the Container runtime card sends: nothing while it is off, its choices while it is on. On with
  // nothing chosen is incomplete rather than silently off, so the step asks for a choice.
  const engine = containersEnabled ? containerRuntime : ''
  const ociRequested = containersEnabled && ociContainers
  const containersEmpty = containersEnabled && !ociContainers && containerRuntime === ''
  // The backend makes the first controller in nodeAssignments (which follow selectedIds) the
  // primary; with a GPU monitoring feature it also hosts Prometheus and Grafana.
  const primaryController = servers.find((server) => server.id === controllerIds[0])
  const primaryControllerName = primaryController ? serverDisplayName(primaryController) : 'the primary controller'

  // Toggle one Slurm role on a node. A node keeps whichever roles are checked; a node with none
  // is simply not part of the cluster. Login is a submission/client host (no cluster daemon).
  const toggleDaemon = (serverId: string, daemon: SlurmDaemon, checked: boolean) => {
    const server = servers.find((candidate) => candidate.id === serverId)
    if (server?.status.provisioning?.locked || !eligible(serverId)) return
    setDaemons((current) => {
      const existing = current[serverId] ?? NO_DAEMONS
      return { ...current, [serverId]: { ...existing, [daemon]: checked } }
    })
  }

  const roleCells = (server: Server, surface: 'table' | 'card') => {
    const held = heldBy(server)
    const unavailable = Boolean(held) || !eligible(server.id)
    const reason = held ?? eligibilityByServerId[server.id]?.explanation ?? 'the deployment requirement is unavailable'
    const roles = daemons[server.id] ?? NO_DAEMONS
    const name = serverDisplayName(server)
    const idPrefix = surface === 'card' ? 'mobile-' : ''
    const checkbox = (daemon: SlurmDaemon, unavailableLabel: string, label: string) => (
      <Checkbox
        id={`${idPrefix}slurm-${daemon}-${server.id}`}
        aria-label={unavailable ? `${unavailableLabel} on ${name}, unavailable: ${reason}` : label}
        checked={!unavailable && roles[daemon]}
        disabled={unavailable}
        onCheckedChange={(checked) => toggleDaemon(server.id, daemon, checked)}
      />
    )
    return [
      checkbox('controller', 'slurmctld', `Run slurmctld on ${name}`),
      checkbox('compute', 'slurmd', `Run slurmd on ${name}`),
      checkbox('login', 'login node', `Make ${name} a login (submission) host`),
    ]
  }

  return {
    basicsFields: null,
    basicsExtras: (
      <>
        <AdvancedSettings>
          <div className="sw-form-grid">
            <Field.Root>
              <Field.Label>Cluster name</Field.Label>
              <Input value={clusterName} onChange={(event) => setClusterName(event.target.value)} placeholder={platformName.trim() || 'lab-slurm'} />
              <Field.HelperText>Slurm ClusterName; defaults to a sanitized Platform name.</Field.HelperText>
            </Field.Root>
            <Field.Root>
              <Field.Label>slurmrestd API version</Field.Label>
              <Input value={apiVersion} onChange={(event) => setAPIVersion(event.target.value)} placeholder="Auto-detect (for example, v0.0.42)" />
              <Field.HelperText>Leave blank to detect the endpoint version during deployment.</Field.HelperText>
            </Field.Root>
          </div>
        </AdvancedSettings>
        {requirementState.status === 'loading' && (
          <Alert status="info" title="Loading Slurm deployment requirement">Node selection remains unavailable until the current policy is known.</Alert>
        )}
        {requirementState.status === 'error' && (
          <Alert status="error" title="Slurm deployment requirement is unavailable">
            {requirementState.message}
            <Button size="sm" variant="outline" onClick={reloadRequirement}>Retry</Button>
          </Alert>
        )}
      </>
    ),
    basicsValid: requirementState.status === 'ready',
    basicsBlockedReason: 'Wait for the Slurm deployment policy to load.',
    machines: {
      name: 'Nodes & Roles',
      title: 'Nodes and daemons',
      intro: (
        <>
          <Alert status="info" title={'Minimum resources: ' + formatMinimumResources(minimumResources)}>
            The same observed-hardware floor applies to controller, compute, and login roles. Ineligible Servers remain visible with their shortfalls.
          </Alert>
          <Alert status="info" title="Assign Slurm roles per node">
            A node may run the controller daemon (slurmctld), the compute daemon (slurmd), both, or be a login (submission) host. At least one controller
            and one compute node are required. A login node also hosts the shared storage for HA and self-hosted workload storage.
          </Alert>
          <HStack gap="2" wrap="wrap">
            <Badge colorPalette={controllerIds.length > 0 ? 'green' : 'orange'} variant="subtle">{controllerIds.length} controller</Badge>
            <Badge colorPalette={computeIds.length > 0 ? 'green' : 'orange'} variant="subtle">{computeIds.length} compute</Badge>
            <Badge colorPalette={loginIds.length > 0 ? 'blue' : 'gray'} variant="subtle">{loginIds.length} login</Badge>
            <Badge colorPalette={selectedIds.length > 0 ? 'blue' : 'gray'} variant="subtle">{selectedIds.length} selected</Badge>
          </HStack>
          {highlyAvailable && (
            <Alert status="info" title="Shared controller state is provisioned automatically">
              Multiple controllers require one shared StateSaveLocation. Swallow sets it up for you — a managed NFS export on a selected node, mounted on
              every controller — so there is nothing to enter here. (Lab-grade: the export host is a single storage failure domain.)
            </Alert>
          )}
        </>
      ),
      roleHeaders: ['Controller', 'Compute', 'Login'],
      roleCells,
      eligibility: (server) => eligibilityByServerId[server.id],
      valid: topologyValid,
      blockedReason: 'Assign at least one eligible controller and one eligible compute node.',
    },
    selectedServerIds: selectedIds,
    featureContext: { servers, workloadServerIds: computeIds, platformVersion: null },
    features: {
      title: 'Slurm features',
      content: (
        <SlurmDeploymentFeatures
          workloadMode={workloadMode}
          onWorkloadModeChange={setWorkloadMode}
          workloadMountPath={workloadMountPath}
          onWorkloadMountPathChange={setWorkloadMountPath}
          workloadNfsUrl={workloadNfsUrl}
          onWorkloadNfsUrlChange={setWorkloadNfsUrl}
          workloadMountOptions={workloadMountOptions}
          onWorkloadMountOptionsChange={setWorkloadMountOptions}
          loginNodeCount={loginIds.length}
          containersEnabled={containersEnabled}
          onContainersEnabledChange={(enabled) => {
            setContainersEnabled(enabled)
            // A fresh card offers native OCI only; the engine is an explicit second choice.
            setOCIContainers(enabled)
            setContainerRuntime('')
          }}
          containerRuntime={containerRuntime}
          onContainerRuntimeChange={setContainerRuntime}
          monitoring={monitoring}
          onMonitoringChange={setMonitoring}
          monitoringInstallMethod={monitoringInstallMethod}
          onMonitoringInstallMethodChange={setMonitoringInstallMethod}
          hasGPUComputeNode={hasGPUComputeNode}
          ociContainers={ociContainers}
          onOCIContainersChange={setOCIContainers}
          containersEmpty={containersEmpty}
          pmix={pmix}
          onPMIxChange={setPMIx}
          primaryControllerName={primaryControllerName}
          gresTypeMode={gresTypeMode}
          onGresTypeModeChange={setGresTypeMode}
          gresType={gresType}
          onGresTypeChange={setGresType}
          gresTypeInvalid={gresTypeInvalid && gresType.trim() !== ''}
          driverOptionsText={gpuDriverOptionsText}
          onDriverOptionsTextChange={setGPUDriverOptionsText}
          driverOptionErrors={gpuDriverOptions.errors}
        />
      ),
      valid: workloadValid && gpuSettingsValid && monitoringValid && !containersEmpty,
      blockedReason: !workloadValid
        ? 'Complete the enabled shared-storage settings.'
        : !monitoringValid
          ? 'The Device Metrics Exporter needs a compute node with AMD GPUs.'
          : containersEmpty
            ? 'Choose native OCI containers or a container engine, or turn Container runtime off.'
            : 'Correct the advanced GPU settings.',
    },
    extraSteps: () => [],
    reviewGroups: ({ name, site, preparationSummary }) => [
      {
        title: 'Platform',
        stepId: 'deploy-basics',
        items: [
          { label: 'Name', value: name },
          { label: 'Site', value: site },
          { label: 'Cluster name', value: clusterName.trim() || name.trim() },
          { label: 'Minimum resources', value: formatMinimumResources(minimumResources) },
          ...(apiVersion.trim() ? [{ label: 'slurmrestd API version', value: apiVersion.trim() }] : []),
        ],
      },
      {
        title: 'Nodes and roles',
        stepId: 'deploy-machines',
        items: [
          { label: 'Controllers', value: String(controllerIds.length) },
          { label: 'Compute nodes', value: String(computeIds.length) },
          { label: 'Login nodes', value: String(loginIds.length) },
          { label: 'Server preparation', value: preparationSummary },
          ...(highlyAvailable ? [{ label: 'Shared controller state', value: 'Included automatically (NFS)' }] : []),
        ],
      },
      {
        title: 'Shared workload storage',
        stepId: 'deploy-features',
        items: [{
          label: 'Configuration',
          value: workloadMode === 'self-hosted'
            ? `Self-hosted NFS at ${workloadMountPath.trim() || '/shared'}`
            : workloadMode === 'external'
              ? `External ${workloadNfsUrl.trim()} at ${workloadMountPath.trim() || '/shared'}`
              : 'Not included',
        }],
      },
      {
        title: 'Container runtime',
        stepId: 'deploy-features',
        items: [
          { label: 'Native OCI containers', value: ociRequested ? 'crun with srun --container on compute nodes' : 'Not included' },
          {
            label: 'Container engine',
            value: engine ? `${slurmContainerRuntimeLabel(engine)} on compute and login nodes` : 'Not included',
          },
        ],
      },
      {
        title: 'MPI',
        stepId: 'deploy-features',
        items: [{ label: 'MPI with PMIx', value: pmix ? 'OpenMPI with srun --mpi=pmix_v5' : 'Not included' }],
      },
      {
        title: 'GPU monitoring and scheduling',
        stepId: 'deploy-features',
        items: [
          {
            label: 'GPU monitoring',
            value: monitoring === 'none'
              ? 'Not included'
              : `${monitoring === 'omnistat' ? 'Omnistat' : 'AMD Device Metrics Exporter'}; Prometheus and Grafana on ${primaryControllerName} as ${monitoringInstallMethod === 'native' ? 'native packages' : 'containers'}`,
          },
          {
            label: 'GPU GRES',
            value: gresTypeMode === 'custom'
              ? `Included automatically for AMD GPU compute nodes as type ${gresType.trim()}`
              : 'Included automatically for AMD GPU compute nodes, typed by GPU architecture',
          },
          {
            label: 'GPU driver options',
            value: formatGPUDriverModuleOptions(gpuDriverOptions.options) || 'Only what each node\u2019s GPU topology needs',
          },
        ],
      },
    ],
    reviewGroupsAfterOS: () => [],
    deployModeNotice: () => null,
    input: () => ({
      gpuStackOwner: 'provisioning',
      ...(engine ? { containerRuntime: { kind: engine } } : {}),
      ...(monitoring === 'omnistat' ? { omnistat: { enabled: true, installMethod: monitoringInstallMethod } } : {}),
      ...(monitoring === 'device-metrics-exporter'
        ? { deviceMetricsExporter: { enabled: true, installMethod: monitoringInstallMethod } }
        : {}),
      ...(Object.keys(gpuDriverOptions.options).length > 0 ? { gpuDriver: { moduleOptions: gpuDriverOptions.options } } : {}),
      slurm: {
        ...(gresTypeMode === 'custom' ? { gpuGresType: gresType.trim() } : {}),
        ...(ociRequested ? { ociContainers: true } : {}),
        ...(pmix ? { pmix: true } : {}),
        clusterName: clusterName.trim() || undefined,
        apiVersion: apiVersion.trim() || undefined,
        // stateSaveLocation is intentionally omitted: Swallow provisions the HA shared
        // StateSaveLocation itself, so the wizard does not collect a path.
        nodeAssignments: selectedIds.map((serverId) => ({
          serverId,
          controller: Boolean(daemons[serverId]?.controller),
          compute: Boolean(daemons[serverId]?.compute),
          login: Boolean(daemons[serverId]?.login),
        })),
        // Optional shared workload filesystem; omitted when "none".
        workloadStorage:
          workloadMode === 'none'
            ? undefined
            : {
                mode: workloadMode,
                type: 'nfs',
                mountPath: workloadMountPath.trim() || '/shared',
                nfs: workloadMode === 'external' ? { url: workloadNfsUrl.trim(), mountOptions: workloadMountOptions.trim() || undefined } : undefined,
              },
      },
    }),
    reset: () => {
      setDaemons({})
      setWorkloadMode('none')
      setWorkloadNfsUrl('')
      setContainersEnabled(false)
      setContainerRuntime('')
      setMonitoring('none')
      setOCIContainers(false)
      setPMIx(false)
    },
    refresh: reloadRequirement,
  }
}
