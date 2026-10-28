import { Badge, Field, Input, RadioCard, Text, Textarea } from '@chakra-ui/react'
import { useState } from 'react'
import type { ObservabilityInstallMethod, SlurmContainerRuntime } from '@/domain/platform/types'
import { Alert } from '@/presentation/components/ui/alert'
import { AdvancedSettings } from './AdvancedSettings'
import { FeatureToggleCard } from './FeatureToggleCard'

type WorkloadStorageMode = 'none' | 'self-hosted' | 'external'

/** Whether the GPU GRES type is derived from the GPU architecture or named by the operator. */
export type GPUGresTypeMode = 'automatic' | 'custom'

/**
 * The wizard's GPU monitoring choice. The API has one request object per feature (`omnistat`,
 * `deviceMetricsExporter`) and refuses both together, so the form keeps a single choice and the
 * page maps it to at most one object.
 */
export type SlurmMonitoringChoice = 'none' | 'device-metrics-exporter' | 'omnistat'

interface SlurmDeploymentFeaturesProps {
  workloadMode: WorkloadStorageMode
  onWorkloadModeChange: (mode: WorkloadStorageMode) => void
  workloadMountPath: string
  onWorkloadMountPathChange: (value: string) => void
  workloadNfsUrl: string
  onWorkloadNfsUrlChange: (value: string) => void
  workloadMountOptions: string
  onWorkloadMountOptionsChange: (value: string) => void
  loginNodeCount: number
  containerRuntime: SlurmContainerRuntime | ''
  onContainerRuntimeChange: (runtime: SlurmContainerRuntime | '') => void
  monitoring: SlurmMonitoringChoice
  onMonitoringChange: (choice: SlurmMonitoringChoice) => void
  monitoringInstallMethod: ObservabilityInstallMethod
  onMonitoringInstallMethodChange: (method: ObservabilityInstallMethod) => void
  /** Whether a selected compute Server lists AMD compute GPUs; the exporter needs one. */
  hasGPUComputeNode: boolean
  ociContainers: boolean
  onOCIContainersChange: (enabled: boolean) => void
  pmix: boolean
  onPMIxChange: (enabled: boolean) => void
  primaryControllerName: string
  gresTypeMode: GPUGresTypeMode
  onGresTypeModeChange: (mode: GPUGresTypeMode) => void
  gresType: string
  onGresTypeChange: (value: string) => void
  gresTypeInvalid: boolean
  driverOptionsText: string
  onDriverOptionsTextChange: (value: string) => void
  driverOptionErrors: string[]
}
interface ChoiceCardProps<T extends string> {
  label: string
  value: T
  onChange: (value: T) => void
  options: ReadonlyArray<{ value: T; label: string; description: string }>
  columns?: 2 | 3
}

/** Mutually exclusive settings use radio cards; Switches are reserved for enablement. */
function ChoiceCards<T extends string>({ label, value, onChange, options, columns = 2 }: ChoiceCardProps<T>) {
  return (
    <Field.Root>
      <Field.Label>{label}</Field.Label>
      <RadioCard.Root
        aria-label={label}
        colorPalette="brand"
        value={value}
        className={`sw-platform-feature-option-grid${columns === 2 ? ' sw-platform-feature-option-grid--two' : ''}`}
        onValueChange={(details) => onChange(details.value as T)}
      >
        {options.map((option) => (
          <RadioCard.Item key={option.value} value={option.value}>
            <RadioCard.ItemHiddenInput />
            <RadioCard.ItemControl className="sw-platform-option-card">
              <RadioCard.ItemContent>
                <RadioCard.ItemText>{option.label}</RadioCard.ItemText>
                <RadioCard.ItemDescription>{option.description}</RadioCard.ItemDescription>
              </RadioCard.ItemContent>
              <RadioCard.ItemIndicator />
            </RadioCard.ItemControl>
          </RadioCard.Item>
        ))}
      </RadioCard.Root>
    </Field.Root>
  )
}

/**
 * Slurm-only deployment features. Each optional capability is independently enabled, while
 * mode-specific settings expand inside the same card so the operator sees consequences before
 * choosing implementation details. GPU monitoring is one switch with a stack choice because the API
 * refuses the Device Metrics Exporter and Omnistat together; the exporter's card warns, without
 * blocking here (the page blocks the step), when no selected compute node has AMD GPUs. Native OCI
 * containers and MPI with PMIx are plain on/off features. GPU node overrides (GRES type name, driver
 * module options) sit in a collapsed disclosure because the defaults are derived from each node's GPUs.
 */
export function SlurmDeploymentFeatures({
  workloadMode,
  onWorkloadModeChange,
  workloadMountPath,
  onWorkloadMountPathChange,
  workloadNfsUrl,
  onWorkloadNfsUrlChange,
  workloadMountOptions,
  onWorkloadMountOptionsChange,
  loginNodeCount,
  containerRuntime,
  onContainerRuntimeChange,
  monitoring,
  onMonitoringChange,
  monitoringInstallMethod,
  onMonitoringInstallMethodChange,
  hasGPUComputeNode,
  ociContainers,
  onOCIContainersChange,
  pmix,
  onPMIxChange,
  primaryControllerName,
  gresTypeMode,
  onGresTypeModeChange,
  gresType,
  onGresTypeChange,
  gresTypeInvalid,
  driverOptionsText,
  onDriverOptionsTextChange,
  driverOptionErrors,
}: SlurmDeploymentFeaturesProps) {
  const storageEnabled = workloadMode !== 'none'
  const runtimeEnabled = containerRuntime !== ''
  const monitoringEnabled = monitoring !== 'none'
  // Opens on return to this step when a setting is in effect, but never collapses while editing.
  const [advancedInitiallyOpen] = useState(() => gresTypeMode === 'custom' || driverOptionsText.trim() !== '')

  return (
    <div className="sw-platform-feature-list">
      <FeatureToggleCard
        title="Shared workload storage"
        description="Mount one shared filesystem for user and job data on every selected Slurm node. Controller state is managed separately."
        checked={storageEnabled}
        onCheckedChange={(enabled) => onWorkloadModeChange(enabled ? 'self-hosted' : 'none')}
        status={<Badge variant="subtle" colorPalette={storageEnabled ? 'blue' : 'gray'}>{storageEnabled ? 'Included' : 'Not included'}</Badge>}
      >
        <div className="sw-platform-feature-stack">
          <ChoiceCards
            label="Storage source"
            value={workloadMode === 'none' ? 'self-hosted' : workloadMode}
            onChange={onWorkloadModeChange}
            options={[
              { value: 'self-hosted', label: 'Self-hosted', description: 'Export NFS from a selected login node.' },
              { value: 'external', label: 'External NFS', description: 'Mount an operator-provided host:/path export.' },
            ]}
          />
          {workloadMode === 'self-hosted' && loginNodeCount === 0 && (
            <Alert status="warning" title="A login node is required">Assign Login to at least one Server in Nodes & Roles.</Alert>
          )}
          <div className="sw-form-grid">
            <Field.Root required>
              <Field.Label>Mount path</Field.Label>
              <Input value={workloadMountPath} onChange={(event) => onWorkloadMountPathChange(event.target.value)} placeholder="/shared" />
              <Field.HelperText>Mounted on every node. Do not use /home because that would hide the SSH user&apos;s home.</Field.HelperText>
            </Field.Root>
            {workloadMode === 'external' && (
              <Field.Root required invalid={Boolean(workloadNfsUrl.trim()) && !/^[^\s:]+:\/\S*$/.test(workloadNfsUrl.trim())}>
                <Field.Label>NFS URL</Field.Label>
                <Input value={workloadNfsUrl} onChange={(event) => onWorkloadNfsUrlChange(event.target.value)} placeholder="nfs-server:/export/data" />
                <Field.HelperText>Use host:/path, for example 10.0.0.9:/export/data.</Field.HelperText>
                <Field.ErrorText>Use host:/path, for example 10.0.0.9:/export/data.</Field.ErrorText>
              </Field.Root>
            )}
            {workloadMode === 'external' && (
              <Field.Root>
                <Field.Label>Mount options</Field.Label>
                <Input value={workloadMountOptions} onChange={(event) => onWorkloadMountOptionsChange(event.target.value)} placeholder="rw,_netdev,hard,timeo=600,retrans=2" />
              </Field.Root>
            )}
          </div>
        </div>
      </FeatureToggleCard>

      <FeatureToggleCard
        title="Container runtime"
        description="Install a runtime on every compute and login node for containerized workloads. Slurm itself does not require one."
        checked={runtimeEnabled}
        onCheckedChange={(enabled) => onContainerRuntimeChange(enabled ? 'docker-ce' : '')}
        status={<Badge variant="subtle" colorPalette={runtimeEnabled ? 'blue' : 'gray'}>{runtimeEnabled ? 'Included' : 'Not included'}</Badge>}
      >
        <ChoiceCards
          label="Runtime"
          value={containerRuntime || 'docker-ce'}
          onChange={onContainerRuntimeChange}
          options={[
            { value: 'docker-ce', label: 'Docker CE', description: 'Install Docker Engine on every target node.' },
            { value: 'podman', label: 'Podman', description: 'Install the daemonless Podman runtime.' },
          ]}
        />
      </FeatureToggleCard>

      <FeatureToggleCard
        title="GPU monitoring"
        description={`Collect GPU metrics attributed to Slurm jobs, with Prometheus and a read-only Grafana on ${primaryControllerName}.`}
        checked={monitoringEnabled}
        onCheckedChange={(enabled) => onMonitoringChange(enabled ? 'device-metrics-exporter' : 'none')}
        status={<Badge variant="subtle" colorPalette={monitoringEnabled ? 'blue' : 'gray'}>{monitoringEnabled ? 'Included' : 'Not included'}</Badge>}
      >
        <div className="sw-platform-feature-stack">
          <ChoiceCards
            label="Monitoring stack"
            value={monitoring === 'none' ? 'device-metrics-exporter' : monitoring}
            onChange={onMonitoringChange}
            options={[
              {
                value: 'device-metrics-exporter',
                label: 'AMD Device Metrics Exporter',
                description: 'Per-GPU metrics labelled with the job on each GPU. Jobs keep sharing nodes.',
              },
              {
                value: 'omnistat',
                label: 'Omnistat',
                description: 'Node-level job metrics and reports. Each compute node runs one job at a time.',
              },
            ]}
          />
          {monitoring === 'device-metrics-exporter' && !hasGPUComputeNode && (
            <Alert status="warning" title="No compute node has AMD GPUs">
              The Device Metrics Exporter runs on GPU compute nodes. Select a compute Server whose GPU inventory lists AMD compute GPUs, or choose Omnistat.
            </Alert>
          )}
          <ChoiceCards
            label="Prometheus and Grafana installation"
            value={monitoringInstallMethod}
            onChange={onMonitoringInstallMethodChange}
            options={[
              { value: 'container', label: 'Containers', description: `Run both as containers on ${primaryControllerName}.` },
              { value: 'native', label: 'Native', description: 'Use the official Prometheus release and Grafana package repository.' },
            ]}
          />
          <Text color="fg.muted" fontSize="sm">
            {monitoring === 'omnistat'
              ? 'Each compute node runs one job at a time so Omnistat metrics map to a single job.'
              : 'Hardware profiler counters stay off. Where GPUs report the same id (for example MI308X virtual functions), the Platform reports the GPU identity as collapsed: metrics flow, but per-job GPU attribution is not reliable.'}
          </Text>
        </div>
      </FeatureToggleCard>

      <FeatureToggleCard
        title="Native OCI containers"
        description="Run job steps in OCI bundles with srun --container: crun and a rootless runtime on every compute node. Bundles must exist at the same path on every node."
        checked={ociContainers}
        onCheckedChange={onOCIContainersChange}
        status={<Badge variant="subtle" colorPalette={ociContainers ? 'blue' : 'gray'}>{ociContainers ? 'Included' : 'Not included'}</Badge>}
      />

      <FeatureToggleCard
        title="MPI with PMIx"
        description="Install OpenMPI with PMIx on compute and login nodes so srun --mpi=pmix_v5 launches MPI jobs. The cluster's default MPI stays none."
        checked={pmix}
        onCheckedChange={onPMIxChange}
        status={<Badge variant="subtle" colorPalette={pmix ? 'blue' : 'gray'}>{pmix ? 'Included' : 'Not included'}</Badge>}
      />

      <div className="sw-platform-included-card">
        <div>
          <Text fontWeight="semibold">AMD GPU scheduling</Text>
          <Text color="fg.muted" fontSize="sm">Compute nodes with AMD GPUs are configured as Slurm GRES. Jobs request GPUs explicitly and see only their allocation. Every GPU must allocate and free memory before its node joins.</Text>
        </div>
        <Badge variant="subtle" colorPalette="green">Included automatically</Badge>
      </div>

      <AdvancedSettings label="Advanced GPU settings" open={advancedInitiallyOpen}>
        <div className="sw-platform-feature-stack">
          <ChoiceCards
            label="GPU GRES type"
            value={gresTypeMode}
            onChange={onGresTypeModeChange}
            options={[
              { value: 'automatic', label: 'GPU architecture', description: 'Name GPUs by their architecture, for example gfx942.' },
              { value: 'custom', label: 'Custom name', description: 'Use the name existing job scripts request, for example mi308x_vf.' },
            ]}
          />
          {gresTypeMode === 'custom' && (
            <Field.Root required invalid={gresTypeInvalid}>
              <Field.Label>GRES type name</Field.Label>
              <Input value={gresType} onChange={(event) => onGresTypeChange(event.target.value)} placeholder="mi308x_vf" />
              <Field.HelperText>Jobs request it as --gres=gpu:{gresType.trim() || 'name'}:N. Letters, digits, and underscores; start with a letter.</Field.HelperText>
              <Field.ErrorText>Use letters, digits, and underscores, starting with a letter (at most 64 characters).</Field.ErrorText>
            </Field.Root>
          )}
          <Field.Root invalid={driverOptionErrors.length > 0}>
            <Field.Label>GPU driver module options</Field.Label>
            <Textarea
              value={driverOptionsText}
              onChange={(event) => onDriverOptionsTextChange(event.target.value)}
              placeholder="pcie_p2p=0"
              rows={3}
              fontFamily="mono"
              fontSize="sm"
            />
            <Field.HelperText>
              One amdgpu option per line as name=value. Options a node&apos;s GPU topology needs are applied automatically; an option set here wins. A node whose loaded driver differs is rebooted once.
            </Field.HelperText>
            {driverOptionErrors.map((error) => <Field.ErrorText key={error}>{error}</Field.ErrorText>)}
          </Field.Root>
        </div>
      </AdvancedSettings>
    </div>
  )
}
