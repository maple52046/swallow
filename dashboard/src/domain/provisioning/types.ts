import type { ProvisioningActionResult } from "@/domain/server/types";

/**
 * Swallow-owned writable deployment addressing intent (glossary Network Configuration).
 * `automatic` asks the provisioner to allocate and record an address (provider auto-assign, a
 * stable recorded IP — ADR 018); `dhcp` leaves the address, gateway, and DNS to the network's
 * own DHCP server, the right choice on a subnet whose DHCP the provisioner does not serve
 * (ADR 058); `static` uses a caller-chosen address. `dhcp` is not an alias of `automatic`.
 */
export type DeploymentNetworkMode = "automatic" | "dhcp" | "static";

/** Operator-facing labels of the deployment addressing modes; the key is the domain value sent. */
export const DEPLOYMENT_NETWORK_MODE_LABELS: Record<DeploymentNetworkMode, string> = {
  automatic: "Automatic",
  dhcp: "DHCP",
  static: "Static",
};

/**
 * Where an OS deployment runs. `disk` installs to the machine's disk; `ram` runs from memory
 * (the fact the backend still calls "ephemeral"). This is the canonical operator-facing
 * vocabulary; the dashboard sends `deployTarget` and labels `ram` as "RAM deploy". A `ram`
 * target maps to `ephemeral=true` at the API boundary.
 */
export type DeployTarget = "disk" | "ram";

/**
 * Operator-facing labels for deploy targets. The RAM choice stays concise here; its card
 * description still explains that it runs from memory and leaves Server disks unchanged.
 */
export const DEPLOY_TARGET_LABELS: Record<DeployTarget, string> = {
  disk: "Disk deploy",
  ram: "RAM deploy",
};

/** Maps the deploy target onto the legacy ephemeral boolean the read models still expose. */
export function deployTargetIsEphemeral(target: DeployTarget): boolean {
  return target === "ram";
}

/** Names the deploy target a stored ephemeral flag corresponds to, for rendering a selector. */
export function deployTargetForEphemeral(ephemeral: boolean): DeployTarget {
  return ephemeral ? "ram" : "disk";
}

/** Reusable network intent; target NICs and static addresses are deliberately excluded. */
export interface DeploymentNetworkSettings {
  mode: DeploymentNetworkMode;
  subnetId?: string;
  defaultGateway: boolean;
}

/** Reusable, integration-owned deployment intent. Cloud-init is never readable. */
export interface DeploymentTemplate {
  id: string;
  siteId: string;
  integrationId: string;
  name: string;
  description: string;
  imageId: string;
  ephemeral: boolean;
  network: DeploymentNetworkSettings;
  hasUserData: boolean;
  createdAt: string;
  updatedAt: string;
}

/** Create input is the only template mutation that may carry initial write-only user data. */
export interface CreateDeploymentTemplateInput {
  integrationId: string;
  name: string;
  description?: string;
  imageId: string;
  /**
   * Preferred deploy-mode vocabulary; maps onto the stored ephemeral flag at the API. `ephemeral`
   * remains accepted for backward compatibility; the dashboard sends `deployTarget`.
   */
  deployTarget?: DeployTarget;
  ephemeral?: boolean;
  network?: DeploymentNetworkSettings;
  userData?: string;
}

/** Templates never move integration and cloud-init has dedicated write-only methods. */
export interface UpdateDeploymentTemplateInput {
  name?: string;
  description?: string;
  imageId?: string;
  deployTarget?: DeployTarget;
  ephemeral?: boolean;
  network?: DeploymentNetworkSettings;
}

/** Cloud-init handling for one deployment request. */
export type DeploymentUserDataMode = "inherit" | "replace" | "omit";

/** Target-specific data that cannot be stored in a reusable template. */
export interface DeploymentNetworkAssignment {
  serverId: string;
  interfaceId: string;
  subnetId?: string;
  ipAddress?: string;
}

/** One batch deployment intent for a single provisioner Integration. */
export interface DeployServersInput {
  serverIds: string[];
  templateId?: string;
  settings?: {
    imageId?: string;
    /** Preferred deploy-mode vocabulary; maps onto `ephemeral` at the API boundary. */
    deployTarget?: DeployTarget;
    ephemeral?: boolean;
  };
  userData?: {
    mode: DeploymentUserDataMode;
    value?: string;
  };
  network?: DeploymentNetworkSettings & {
    assignments: DeploymentNetworkAssignment[];
  };
}

/** A post-preflight provider refusal; accepted peers are not rolled back. */
export interface DeploymentFailure {
  serverId: string;
  stage: "network_configuration" | "deployment";
  code: string;
  message: string;
}

/** One local-state or provider-owned prerequisite that currently blocks a target. */
export interface DeploymentTargetIssue {
  serverId: string;
  code: string;
  message: string;
}

/** Side-effect-free target readiness report. */
export interface DeploymentTargetPreflightResult {
  valid: boolean;
  integrationId: string;
  issues: DeploymentTargetIssue[];
}

/** A dispatch summary, not a durable job. */
export interface DeployServersResult {
  requested: number;
  accepted: ProvisioningActionResult[];
  failed: DeploymentFailure[];
}

/** Reference returned after a durable provisioning workflow is accepted. */
export interface ProvisioningOperationReference {
  operationId: string;
}

/**
 * Operator intent to prove one custom OS Image works for one deploy target by deploying it on a
 * chosen ready Server. On success the image is marked verified for the target and the Server is
 * auto-released back to ready. Only custom (uploaded) images need verification.
 */
export interface CreateImageVerificationInput {
  integrationId: string;
  imageId: string;
  architecture: string;
  deployTarget: DeployTarget;
  serverId: string;
  /**
   * Leave the Server deployed after verification instead of returning it to the ready pool.
   * Optional; defaults to false (the verification borrows the Server and gives it back).
   */
  keepServer?: boolean;
}

/** A bounded batch release intent executed by one durable Operation. */
export interface ReleaseServersOperationInput {
  serverIds: string[];
  erase?: boolean;
  secureErase?: boolean;
  quickErase?: boolean;
  comment?: string;
  unbindStaticIPs?: boolean;
  /**
   * Asks the backend to cancel the unfinished Workflows holding the targets and accept the
   * Operation at once (provisioning.md, decision 061). The Operation then waits for those
   * Workflows to end before it acts, so the caller never polls for them.
   */
  cancelActiveWorkflows?: boolean;
}

/**
 * A bounded batch "Return to Ready" intent executed by one durable Operation. Recover
 * converges a failed, broken, or rescue Server back to `ready`; unlike Release it chooses
 * the provider primitive by state, so it carries no disk-erase controls.
 */
export interface RecoverServersOperationInput {
  serverIds: string[];
  comment?: string;
  unbindStaticIPs?: boolean;
  /** Same meaning as on {@link ReleaseServersOperationInput}. */
  cancelActiveWorkflows?: boolean;
}

/** Session-safe result shape. It intentionally has nowhere to store cloud-init. */
export interface StoredDeploymentResult extends DeployServersResult {
  serverIds: string[];
  integrationId: string;
  savedAt: string;
}

/** Provider-neutral observed configuration of one subnet link. */
export type NetworkConfigurationState =
  | "dhcp"
  | "static"
  | "link_only"
  | "unconfigured"
  | "provider_managed"
  | "unknown";

export type PhysicalLinkState = "up" | "down" | "unknown";
/**
 * Manual per-NIC configuration modes. Independent from the deployment intent: manual NIC
 * setup still exposes raw `dhcp` alongside `static` and `link_only`, and does not offer
 * automatic/provider-managed assignment.
 */
export type ManualNetworkMode = "dhcp" | "static" | "link_only";

/**
 * A provisioner subnet a NIC can use, read live from network inspection. `provisionerDhcp` is
 * whether the provisioner itself serves DHCP there; when false any DHCP on it is the site's,
 * which is why the backend suggests `dhcp` deployments on it.
 */
export interface NetworkSubnet {
  id: string;
  name: string;
  cidr: string;
  gatewayAddress: string;
  managed: boolean;
  provisionerDhcp: boolean;
}

export interface NetworkLink {
  id: string;
  configurationState: NetworkConfigurationState;
  rawProviderMode: string;
  subnetId: string;
  subnetName: string;
  cidr: string;
  ipAddress: string;
  defaultGateway: boolean;
}

export interface NetworkInterface {
  id: string;
  name: string;
  macAddress: string;
  boot: boolean;
  physicalState: PhysicalLinkState;
  configurationState: NetworkConfigurationState;
  rawProviderMode: string;
  links: NetworkLink[];
  availableSubnets: NetworkSubnet[];
}

/** Swallow-owned deploy defaults derived from the selected NIC's live state. */
export interface DeploymentNetworkSuggestion {
  mode: DeploymentNetworkMode;
  interfaceId: string;
  subnetId: string;
  ipAddress: string;
  defaultGateway: boolean;
}

export interface NetworkTarget {
  serverId: string;
  editable: boolean;
  disabledReason: string;
  suggestion: DeploymentNetworkSuggestion;
  network: {
    interfaces: NetworkInterface[];
  };
}

export interface NetworkInspectionResult {
  targets: NetworkTarget[];
  issues: DeploymentTargetIssue[];
}

/** Explicit manual NIC mutation. AUTO and keep-current are intentionally absent. */
export interface NetworkLinkInput {
  mode: ManualNetworkMode;
  subnetId: string;
  ipAddress?: string;
  defaultGateway: boolean;
}

/**
 * One tag known for a Site, as offered by the tag editor. `editable` is false for a
 * provider-computed automatic tag (a MAAS tag with a definition, e.g. one that may back
 * `amd-gpu`): it is real and meaningful but swallow cannot assign or unassign it, so the editor
 * shows it disabled. See swallow's `Tag` glossary term and decision 071.
 */
export interface ServerTagOption {
  name: string;
  editable: boolean;
}

/**
 * A tri-state tag edit applied to one or more Servers, like email labels. `add` is the set of tags
 * to apply to every listed Server and `remove` the set to unapply from every listed Server; any tag
 * not named is left untouched on each Server. The editor computes this diff from each tag's
 * all/some/none state across the selection, so only changed tags are sent.
 */
export interface EditServerTagsInput {
  serverIds: string[];
  add: string[];
  remove: string[];
}

/**
 * One Server's spec after a tag edit (`ServerTagSet`, server-tags.md), so callers can update rows
 * without a re-read. The edit only writes the spec: the provisioner converges afterwards, and the
 * Server's `realized` condition reports when it has for this `generation`.
 */
export interface ServerTagsResult {
  serverId: string;
  /** The Server's spec generation after the edit. */
  generation: number;
  /** The complete manual-tag spec after the edit; `[]` when none. */
  tags: string[];
}

/** Durable release follow-up shown in Server Activity. */
export interface ProvisioningTask {
  id: string;
  kind: "release_network_cleanup";
  serverId: string;
  status: "pending" | "running" | "succeeded" | "failed";
  phase:
    | "waiting_for_release"
    | "waiting_for_ready"
    | "cleaning_network"
    | "complete";
  attempt: number;
  error?: string;
  requestId?: string;
  retryable: boolean;
  createdAt: string;
  updatedAt: string;
}

/**
 * A Boot ISO (glossary Boot ISO, decisions 049 and 057): a network boot ISO swallow built for one
 * provisioner Integration. It takes an address from the site's own DHCP and hands off to that
 * provisioner's MAAS rack, so a Server whose network DHCP is not the provisioner's still reaches
 * it. Only a Server of the same Integration may mount it as its Boot Media; the API enforces that.
 *
 * - BIOS firmware runs iPXE with `script`, which chains `chainUrl` (the rack's `ipxe.cfg`). UEFI
 *   firmware runs the ISO's own GRUB with `grubConfig`: it takes a DHCP lease and loads
 *   `uefiChainUrl` (the rack's `grub/grub.cfg-<mac>`, `<mac>` being the leasing interface), MAAS's
 *   own UEFI path, which is what makes MAAS give a UEFI Machine its EFI system partition. An empty
 *   `uefiChainUrl` marks an ISO built before decision 057 that sends UEFI firmware through iPXE and
 *   needs a rebuild (see `bootISONeedsRebuild`).
 * - `script` and `grubConfig` are what swallow rendered from its fixed templates; display-only.
 * - `url` is where BMCs mount the ISO without credentials (and where an operator can download it);
 *   it is empty when the installation has no Boot Media base URL. A rebuild keeps it.
 * - `inUseBy` counts Servers whose enabled Boot Media uses it; while non-zero it cannot be deleted.
 */
export interface BootISO {
  id: string;
  name: string;
  siteId: string;
  integrationId: string;
  /** The MAAS rack as the operator gave it: an IPv4 address or hostname, optionally `:port`. */
  rackAddress: string;
  chainUrl: string;
  uefiChainUrl: string;
  script: string;
  /** The UEFI path's GRUB configuration; empty for an ISO that predates it. */
  grubConfig: string;
  ipxeVersion: string;
  sizeBytes: number;
  sha256: string;
  url: string;
  inUseBy: number;
  createdAt: string;
  createdBy?: string;
  /** When and by whom the ISO was last rebuilt in place; absent when it never was. */
  rebuiltAt?: string;
  rebuiltBy?: string;
}

/**
 * Whether a Boot ISO predates the UEFI hand-off (decision 057): UEFI Servers booting it reach MAAS
 * through `ipxe.cfg`, MAAS treats them as BIOS, and their disk deployment fails installing GRUB.
 * Rebuilding fixes it in place. A missing field (an API older than this console) reads as needing
 * a rebuild, the safe answer.
 */
export function bootISONeedsRebuild(iso: Pick<BootISO, "uefiChainUrl">): boolean {
  return !iso.uefiChainUrl;
}

/**
 * Whether this installation can build Boot ISOs. `reason` (present when `available` is false) is
 * the API's operator-facing explanation — a missing Boot Media base URL, an unwritable directory,
 * or iPXE assets or packaging tools missing from the image — shown verbatim.
 */
export interface BootISOBuilderStatus {
  available: boolean;
  reason?: string;
}

/** The Boot ISO list: the builder's availability, and the Boot ISOs ordered by name. */
export interface BootISOCatalog {
  builder: BootISOBuilderStatus;
  items: BootISO[];
}

/**
 * A build request. The operator chooses the provisioner and names the MAAS rack; the script is
 * always swallow's template, so there is no script input.
 */
export interface CreateBootISOInput {
  name: string;
  integrationId: string;
  rackAddress: string;
}

/**
 * What a host that keeps its operating system needs to enroll itself into one provisioner
 * (Server Enrollment, contract server-enrollment.md). `command` is one line the operator runs on
 * the host: it downloads the enrollment script from swallow and runs it as root, which downloads
 * the swallow CLI and enrolls the host. The host then appears as a `deployed` Server after the
 * next inventory sync.
 *
 * `token` is the provisioner's own credential (for MAAS its API key), which the provider's
 * registration requires. It is a secret: keep it only in component memory, never in storage,
 * logs, or URLs. `command` embeds it.
 */
export interface HostEnrollmentBundle {
  integrationId: string;
  providerKind: string;
  endpoint: string;
  token: string;
  command: string;
}

/**
 * A request to enroll libvirt virtual machines of a Hypervisor into a provisioner by domain name
 * (contract server-enrollment.md "Virtual machines (libvirt)", decision 055). The Hypervisor is a
 * deployed swallow Server. `bootIsoId` names a Boot ISO of the same provisioner for a network its
 * DHCP does not serve; `account` overrides the Hypervisor's Server Default User; `powerOffRunning`
 * lets swallow hard-stop a running domain instead of asking for attention.
 */
export interface VirtualMachineEnrollmentInput {
  integrationId: string;
  hypervisorServerId: string;
  domains: string[];
  bootIsoId?: string;
  account?: string;
  powerOffRunning?: boolean;
}

/** The enroll-virtual-machines Workflow an enrollment request started (`202 { workflowId }`). */
export interface VirtualMachineEnrollmentAccepted {
  workflowId: string;
}

/**
 * The closed mode set of a Provisioner HTTP Proxy (glossary Provisioner HTTP Proxy, contract
 * provisioner-http-proxy.md): which proxy a provisioner hands to the machines it deploys. The
 * lowercase value is the domain term sent to and read from the API; UI labels ("Don't use a
 * proxy", "MAAS built-in", …) belong to the presentation.
 *
 * - `none`: machines reach package repositories directly.
 * - `builtin`: machines use the provisioner's own proxy, which goes out directly.
 * - `external`: machines, and the provisioner, use `httpProxy` directly.
 * - `peer`: machines use the provisioner's own proxy, which forwards to `httpProxy`.
 */
export type ProvisionerHttpProxyMode = "none" | "builtin" | "external" | "peer";

/** Every Provisioner HTTP Proxy mode, in the order the operator chooses between them. */
export const PROVISIONER_HTTP_PROXY_MODES: readonly ProvisionerHttpProxyMode[] = ["none", "builtin", "external", "peer"];

/**
 * One provisioner Integration's Provisioner HTTP Proxy, read live from the provisioner (decision
 * 056). swallow stores none of it, and it is not part of the Integration's `settings`. `httpProxy`
 * is `""` unless the mode routes through it, so a value read can be written back unchanged.
 */
export interface ProvisionerHttpProxy {
  integrationId: string;
  mode: ProvisionerHttpProxyMode;
  httpProxy: string;
}

/**
 * A replacement Provisioner HTTP Proxy. The write replaces mode and URL together; `httpProxy` must
 * be `""` for `none` and `builtin`, and an `http(s)://host[:port]` URL otherwise.
 */
export interface SetProvisionerHttpProxyInput {
  mode: ProvisionerHttpProxyMode;
  httpProxy: string;
}

/** Whether `mode` routes deployed machines through an operator-given proxy URL. */
export function httpProxyModeNeedsUrl(mode: ProvisionerHttpProxyMode): boolean {
  return mode === "external" || mode === "peer";
}

/** The most DNS servers one subnet's Subnet DNS may list (contract provisioner-subnet-dns.md). */
export const MAX_SUBNET_DNS_SERVERS = 8;

/**
 * One subnet of a provisioner Integration with its Subnet DNS (glossary Subnet DNS, decision 058),
 * read live from the provisioner; swallow stores none of it. `dnsServers` is in the provisioner's
 * order and empty when the subnet has none, in which case `automatic` and `static` deployments on
 * it use the provisioner's own DNS. `dhcp` deployments take DNS from the DHCP server instead.
 *
 * `attached` marks a real network by the provisioner's own markers (a controller interface, its
 * DHCP, a machine's boot interface, or a NIC with hardware identity is on it). The rest are
 * host-internal networks — container, Kubernetes, or VM bridges an enrolled host reported — that
 * deployments never use; the Dashboard lists them apart rather than hiding them.
 */
export interface ProvisionerSubnet {
  id: string;
  name: string;
  cidr: string;
  gatewayAddress: string;
  managed: boolean;
  provisionerDhcp: boolean;
  attached: boolean;
  dnsServers: string[];
}

/** The port the MAAS rack serves `ipxe.cfg` on when `rackAddress` names none (contract boot-isos.md). */
export const DEFAULT_MAAS_RACK_PORT = 5248;

/**
 * The URL a Boot ISO built for `rackAddress` chains to, for previewing a build before it is sent.
 * It mirrors the API's derivation (`http://<host>:<port or 5248>/ipxe.cfg`) without validating:
 * the API is the authority and rejects an invalid address. Returns an empty string for a blank
 * address.
 */
export function bootISOChainURL(rackAddress: string): string {
  const address = rackAddress.trim();
  if (!address) return "";
  const hasPort = /:\d+$/.test(address);
  return `http://${hasPort ? address : `${address}:${DEFAULT_MAAS_RACK_PORT}`}/ipxe.cfg`;
}
