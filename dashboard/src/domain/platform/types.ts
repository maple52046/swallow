/**
 * A platform swallow knows about, and the shape of a request to deploy one.
 *
 * swallow owns a platform's registration and policy, and — for a platform it builds — the
 * intent to deploy it. Membership is read from the platform's own API and appears on each
 * server's membership axis, so a platform's members are servers, not a field here. See
 * docs/development/glossaries/terms/platform.md and the Node Role glossary.
 */

export type PlatformType = "kubernetes" | "slurm";

/**
 * Options for uninstalling a platform. releaseServers additionally returns each member
 * server to the provider after k0s removal; releaseOptions then mirrors the standalone
 * Release action. The field names match the api-server uninstall request contract.
 */
export interface UninstallPlatformOptions {
  releaseServers: boolean;
  releaseOptions: {
    erase: boolean;
    secureErase: boolean;
    quickErase: boolean;
    unbindStaticIps: boolean;
  };
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
  machinePreparation?: PlatformMachinePreparation;
}

/** Which subsystem installs GPU drivers; has no default at creation. */
export type GPUStackOwner = "provisioning" | "gpu-operator";

/** Which subsystem installs this platform's Prometheus exporters. Defaults to `ansible`. */
export type ExporterOwner = "ansible" | "k8s";

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
  /** Which subsystem installs this platform's exporters; `ansible` unless set to `k8s`. */
  exporterOwner: ExporterOwner;
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
    mode: "dhcp" | "static";
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

export interface DeployPlatformInput {
  siteId: string;
  name: string;
  gpuStackOwner: GPUStackOwner;
  k0sVersion: string;
  podCidr?: string;
  serviceCidr?: string;
  apiVip?: string;
  apiVipPrefix?: number;
  roleAssignments: RoleAssignment[];
  machinePreparation?: PlatformMachinePreparation;
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
