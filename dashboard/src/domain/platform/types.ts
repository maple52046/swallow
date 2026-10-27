/**
 * A platform swallow knows about, and the shape of a request to deploy one.
 *
 * swallow owns a platform's registration and policy, and — for a platform it builds — the
 * intent to deploy it. Membership is read from the platform's own API and appears on each
 * server's membership axis, so a platform's members are servers, not a field here. See
 * docs/development/glossaries/terms/platform.md and the Node Role glossary.
 */

import type { DeploymentNetworkMode } from "@/domain/provisioning/types";

export type PlatformType = "kubernetes" | "slurm";

/** Observed Server hardware floor for Slurm deployment eligibility. */
export interface MinimumResources {
  cpuCores: number;
  memoryMiB: number;
  storageGB: number;
}

/** Current global Slurm deployment requirement; null means resource filtering is disabled. */
export interface SlurmDeploymentRequirement {
  platformType: "slurm";
  minimumResources: MinimumResources | null;
  updatedAt: string | null;
}

/**
 * Options for uninstalling a platform. releaseServers additionally returns each member
 * server to the provider after k0s removal; releaseOptions then mirrors the standalone
 * Release action. cancelActiveWorkflows asks the backend to cancel the Workflows still
 * holding the targets (a failed or running deployment) and start the uninstall once they
 * end (decision 061). The field names match the api-server uninstall request contract.
 */
export interface UninstallPlatformOptions {
  releaseServers: boolean;
  releaseOptions: {
    erase: boolean;
    secureErase: boolean;
    quickErase: boolean;
    unbindStaticIps: boolean;
  };
  cancelActiveWorkflows?: boolean;
}
/** Whether Swallow registered the platform or deployed it through a durable operation. */
export type PlatformOrigin = "registered" | "deployed";

/** Lifecycle derived by the API from durable deploy and uninstall operations. */
export type PlatformLifecycleState =
  | "registered"
  | "deploying"
  | "deploy_failed"
  | "active"
  | "uninstalling"
  | "uninstall_failed"
  | "uninstalled";

/** Deployment topology derived from the original role assignments. */
export type KubernetesTopology =
  "standalone" | "multi-node" | "high-availability";

/** Non-secret deployment intent projected from durable Operation provenance. */
export interface PlatformDeployment {
  topology: KubernetesTopology;
  roleAssignments: RoleAssignment[];
  /**
   * Slurm login (submission) host serverIds. They are neither role assignments nor members, so
   * the Slurm view resolves them to an address to show which host to use to operate the cluster.
   * Empty for Kubernetes and for a Slurm cluster with no login node.
   */
  loginServerIds?: string[];
  machinePreparation?: PlatformMachinePreparation;
}

/**
 * Which subsystem owns the GPU stack. A Kubernetes deploy derives it from the GPU Operator
 * feature (`gpu-operator` when enabled, `provisioning` otherwise), so the dashboard never asks
 * for it directly; `gpu-operator` without a Swallow-managed operator means one managed elsewhere.
 */
export type GPUStackOwner = "provisioning" | "gpu-operator";

/**
 * The GPU Allocation Mode (glossary): how the GPU Operator exposes GPUs to workloads.
 * `device-plugin` registers the `amd.com/gpu` extended resource; `dra` publishes ResourceSlice
 * devices claimed through ResourceClaims. Exactly one is active at a time.
 */
export type GPUAllocationMode = "device-plugin" | "dra";

/**
 * Device plugin resource naming: `single` always names the resource `amd.com/gpu`; `mixed`
 * names partitioned GPUs per partition type.
 */
export type GPUResourceNaming = "single" | "mixed";

/**
 * The GPU Operator State (glossary): where the latest Swallow change to a Platform's GPU
 * Operator stands, projected from the Task applying it. `failed` means the cluster may still run
 * the previous settings; `absent` means Swallow manages no operator there any more.
 */
export type GPUOperatorState = "installing" | "ready" | "failed" | "uninstalling" | "absent";

/**
 * The settings a client sends to install or change the GPU Operator — the deploy request's
 * `gpuOperator` and the `PUT .../gpu-operator` body share it (platforms.md, "Settings"). Omitted
 * fields take the backend defaults: the default release, `device-plugin`, `single`, the node
 * labeller on, and the Device Metrics Exporter off. No field names nodes: the operator manages
 * every node its Node Feature Discovery marks as an AMD GPU node. `{ enabled: false }` uninstalls.
 */
export interface GPUOperatorSettings {
  enabled: boolean;
  version?: string;
  allocationMode?: GPUAllocationMode;
  draDriverVersion?: string;
  resourceNaming?: GPUResourceNaming;
  nodeLabeller?: boolean;
  metricsExporter?: boolean;
}

/**
 * A Platform's Swallow-managed GPU Operator as the API records it: the latest requested settings
 * (normalized, so every field is present) and the state of the Workflow applying them.
 * `draDriverVersion` is null outside DRA mode; `lastWorkflowId` links to that Workflow.
 */
export interface PlatformGPUOperator {
  enabled: boolean;
  version: string;
  allocationMode: GPUAllocationMode;
  draDriverVersion: string | null;
  resourceNaming: GPUResourceNaming;
  nodeLabeller: boolean;
  metricsExporter: boolean;
  state: GPUOperatorState;
  lastWorkflowId: string | null;
  updatedAt: string;
}

/**
 * One AMD GPU Operator release Swallow ships and verified (`GET .../gpu-operator/releases`).
 * Kubernetes versions are `major.minor`; a Platform's k0s version must be at least
 * `minKubernetesVersion`, and `draMinKubernetesVersion` for DRA.
 */
export interface GPUOperatorRelease {
  version: string;
  default: boolean;
  minKubernetesVersion: string;
  draMinKubernetesVersion: string;
  draDriverVersions: string[];
  defaultDraDriverVersion: string;
}

/** The accepted GPU Operator change and the Workflow applying it. */
export interface ConfigureGPUOperatorResult {
  platformId: string;
  workflowId: string;
}

/** Which subsystem installs this platform's Prometheus exporters. Defaults to `ansible`. */
export type ExporterOwner = "ansible" | "k8s";

/**
 * A Monitoring Link (glossary): an operator-registered name and absolute http(s) URL pointing at
 * the runtime's own monitoring UI, such as the Grafana Omnistat brings for Slurm. swallow never
 * queries it; it is a shortcut, not a metrics source. Set through the API/CLI `PATCH`.
 */
export interface MonitoringLink {
  name: string;
  url: string;
}

/**
 * Where the latest Swallow change to a Slurm Platform's Omnistat stands (glossary Omnistat State,
 * platforms.md "Omnistat"): projected from the Workflow Task installing it, never a live probe.
 */
export type OmnistatState = "installing" | "ready" | "failed" | "absent";

/**
 * How a Slurm deploy installs the Prometheus and Grafana Omnistat uses (platforms.md "Omnistat"):
 * `container` (default) on the primary controller's container runtime, or `native` from the
 * official release and package repository.
 */
export type OmnistatInstallMethod = "container" | "native";

/**
 * The container runtime a Slurm deploy installs on its compute and login nodes for containerized
 * workloads (platforms.md "Container Runtime"). It is Managed Software on each Server, not a
 * Platform component; the Platform records only the choice.
 */
export type SlurmContainerRuntime = "docker-ce" | "podman";

/**
 * The Swallow-managed Omnistat of a Slurm Platform: AMD's Slurm-aware GPU monitoring, with its
 * exporter on every compute node and Prometheus and Grafana on `observabilityServerId` (the
 * primary controller), installed by `installMethod`. The Grafana itself is reached through the
 * `Omnistat Grafana` Monitoring Link Swallow registers on success; swallow never queries Omnistat.
 */
export interface PlatformOmnistat {
  enabled: boolean;
  version: string;
  installMethod: OmnistatInstallMethod;
  observabilityServerId: string;
  state: OmnistatState;
  lastWorkflowId: string | null;
  updatedAt: string;
}

/**
 * Freshness of the last membership read. `matchedCount` below `memberCount` means the
 * platform contains machines swallow does not manage, which is worth showing rather than
 * hiding.
 */
export interface PlatformSyncState {
  lastStartedAt: string | null;
  lastSucceededAt: string | null;
  lastError: string | null;
  memberCount: number;
  matchedCount: number;
}

export interface Platform {
  id: string;
  siteId: string;
  name: string;
  type: PlatformType;
  origin: PlatformOrigin;
  lifecycleState: PlatformLifecycleState;
  lifecycleOperationId: string | null;
  /** Null for registered Platforms and historical deployments without complete intent. */
  deployment: PlatformDeployment | null;
  /** Null while a platform is registered or declared but not yet reachable. */
  integrationId: string | null;
  gpuStackOwner: GPUStackOwner;
  /** The Swallow-managed GPU Operator; null when Swallow never managed one on this Platform. */
  gpuOperator: PlatformGPUOperator | null;
  /** Which subsystem installs this platform's exporters; `ansible` unless set to `k8s`. */
  exporterOwner: ExporterOwner;
  /** Operator-registered links to the runtime's own monitoring UI; always an array. */
  monitoringLinks: MonitoringLink[];
  /** The Swallow-managed Omnistat of a Slurm Platform; null when Swallow never installed it. */
  omnistat: PlatformOmnistat | null;
  /** The container runtime a Slurm deploy installed on compute and login nodes; null when off. */
  containerRuntime: SlurmContainerRuntime | null;
  /** Operator-set amdgpu module options the deploy recorded; null when none were set. */
  gpuDriver: GPUDriverSettings | null;
  /** The operator's Slurm GPU GRES type name; null when the deploy derived it from the GPUs. */
  gpuGresType: string | null;
  sync: PlatformSyncState;
  createdAt: string;
  updatedAt: string;
}

/** The part a server plays in a platform. The k0s term "controller" never appears here. */
export type NodeRole = "control-plane" | "worker";

/** Desired role and optional workload co-location for one deployment target. */
export interface RoleAssignment {
  serverId: string;
  role: NodeRole;
  /** A control-plane Server also registers as a schedulable Kubernetes node. */
  runWorkloads?: boolean;
}

/**
 * A request to deploy a k0s platform onto already-deployed servers. Optional CIDRs fall
 * back to backend defaults. `apiVip` is required only when role
 * assignments infer a highly available control plane; one-control-plane deployments use
 * that Server's observed address.
 */
export interface PlatformMachinePreparation {
  mode: "existing_os" | "provision_os";
  templateId?: string;
  settings?: { imageId?: string; ephemeral?: boolean };
  userData?: { mode: "inherit" | "replace" | "omit"; value?: string };
  network?: {
    // Deployment addressing intent, the provisioning contract's network.mode: "automatic" is
    // provider auto-assign, "dhcp" the network's own DHCP, "static" a per-target address.
    mode: DeploymentNetworkMode;
    subnetId?: string;
    defaultGateway: boolean;
    assignments: Array<{
      serverId: string;
      interfaceId: string;
      subnetId?: string;
      ipAddress?: string;
    }>;
  };
}

/**
 * One Slurm deployment target's per-daemon roles. Unlike Kubernetes these are not mutually
 * exclusive: a Server may run the controller daemon (slurmctld), the compute daemon (slurmd),
 * or both. A Server running neither is not part of the cluster.
 */
export interface SlurmNodeAssignment {
  serverId: string;
  controller: boolean;
  compute: boolean;
  login: boolean;
}

/**
 * The optional shared workload filesystem (user/job data), distinct from controller state.
 * `mode` is "self-hosted" (the login node exports NFS) or "external" (an operator NFS URL);
 * `type` is "nfs"; `mountPath` is a non-overlapping path (never /home). `nfs.url` is required
 * for external mode. Omitting the whole object means no shared workload filesystem.
 */
export interface SlurmWorkloadStorageSpec {
  mode: 'self-hosted' | 'external';
  type: 'nfs';
  mountPath: string;
  nfs?: { url: string; mountOptions?: string };
}

/**
 * A request to deploy a Slurm platform. `clusterName` defaults to a sanitized platform name.
 * `apiVersion` pins the slurmrestd endpoint version recorded in the credential.
 * `stateSaveLocation` is an optional override of the slurmctld state directory (the backend
 * provisions HA shared state automatically). A node may be controller, compute, login, or a
 * combination. `workloadStorage` is optional shared user-data storage.
 */
export interface SlurmDeploymentSpec {
  clusterName?: string;
  apiVersion?: string;
  stateSaveLocation?: string;
  nodeAssignments: SlurmNodeAssignment[];
  workloadStorage?: SlurmWorkloadStorageSpec;
  /** Names the GPU GRES type (for example `mi308x_vf`); omitted derives it from the GPUs. */
  gpuGresType?: string;
}

/**
 * amdgpu kernel module options for every GPU node of a deploy, by option name. Options a node's
 * GPU topology needs are applied without being listed here; an option named here wins.
 */
export interface GPUDriverSettings {
  moduleOptions: Record<string, string>;
}

/**
 * A request to deploy a platform onto Ready or already-deployed servers. `type` selects the
 * platform: "kubernetes" (default) reads the k0s fields; "slurm" reads `slurm`. Optional CIDRs
 * fall back to backend defaults. `apiVip` is required only when role assignments infer a highly
 * available control plane; one-control-plane deployments use that Server's observed address.
 * `gpuOperator` installs the AMD GPU Operator with a Kubernetes deploy; `gpuStackOwner` is then
 * derived by the backend and may be omitted. `containerRuntime` installs Docker CE or Podman on
 * every compute and login node, and `omnistat` installs Omnistat (with Prometheus and Grafana on
 * the primary controller, by `installMethod`); both are Slurm-only and rejected for Kubernetes.
 * `gpuDriver` applies to the GPU nodes of either type.
 */
export interface DeployPlatformInput {
  siteId: string;
  name: string;
  type?: PlatformType;
  gpuStackOwner?: GPUStackOwner;
  gpuOperator?: GPUOperatorSettings;
  gpuDriver?: GPUDriverSettings;
  containerRuntime?: { kind: SlurmContainerRuntime };
  omnistat?: { enabled: boolean; installMethod?: OmnistatInstallMethod };
  k0sVersion?: string;
  podCidr?: string;
  serviceCidr?: string;
  apiVip?: string;
  apiVipPrefix?: number;
  roleAssignments?: RoleAssignment[];
  slurm?: SlurmDeploymentSpec;
  machinePreparation?: PlatformMachinePreparation;
}

/**
 * One slurmctld controller as reported by slurmrestd ping, in SlurmctldHost failover order.
 * `status` is the controller RPC liveness (`up | down | unknown`), never monitoring health.
 */
export interface SlurmController {
  hostname: string
  primary: boolean
  status: string
}

/** A Slurm scheduling partition and its configured node set. */
export interface SlurmPartition {
  name: string
  state: string
  nodeSpec: string
  totalNodes: number
}

/**
 * One compute node's live Slurm scheduler state (idle/allocated/mixed/down/drain/...),
 * richer than the membership Member. `state` is scheduler state, not monitoring health.
 */
export interface SlurmClusterNode {
  name: string
  state: string
  cpus: number
  realMemoryMiB: number
  gres: string
  partitions: string[]
  address: string
}

/**
 * A Slurm platform's live cluster state, read on demand from slurmrestd. It is Slurm-native
 * and deliberately separate from the generic membership axis (compute only) and the
 * deployment intent; it powers the Slurm-specific management view.
 */
export interface SlurmCluster {
  controllers: SlurmController[]
  partitions: SlurmPartition[]
  nodes: SlurmClusterNode[]
}

/** The accepted deployment: the created platform and the operation building it. */
export interface DeployPlatformResult {
  platformId: string;
  operationId: string;
}

/** The report a membership sync returns; `unmatched` names members with no server. */
export interface MembershipReport {
  platformId: string;
  platformName: string;
  members: number;
  matched: number;
  cleared: number;
  unmatched: string[];
  error: string | null;
}
