import type { ProvisioningActionResult } from "@/domain/server/types";

/**
 * Swallow-owned writable deployment addressing intent. `automatic` asks the provisioner to
 * assign an address (realized by provider auto-assign, a stable recorded IP — see ADR 018),
 * `static` uses a caller-chosen address. The backend still accepts the deprecated `dhcp`
 * alias for one release, but the dashboard always sends the canonical values.
 */
export type DeploymentNetworkMode = "automatic" | "static";

/**
 * Where an OS deployment runs. `disk` installs to the machine's disk; `ram` runs from memory
 * (the fact the backend still calls "ephemeral"). This is the canonical operator-facing
 * vocabulary; the dashboard sends `deployTarget` and the label for `ram` is "RAM deploy
 * (ephemeral)". A `ram` target maps to `ephemeral=true` at the API boundary.
 */
export type DeployTarget = "disk" | "ram";

/**
 * Operator-facing labels for the deploy targets. `ram` is the fact the backend still calls
 * "ephemeral" (the OS runs from memory and leaves the disks untouched), surfaced with an explicit
 * "(ephemeral)" hint so the vocabulary shift does not lose the old meaning.
 */
export const DEPLOY_TARGET_LABELS: Record<DeployTarget, string> = {
  disk: "Disk deploy",
  ram: "RAM deploy (ephemeral)",
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

export interface NetworkSubnet {
  id: string;
  name: string;
  cidr: string;
  gatewayAddress: string;
  managed: boolean;
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
 * shows it disabled. See swallow's `Tag` glossary term and decision 031.
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

/** The effective tags of one Server after an edit, so callers can update rows without a re-read. */
export interface ServerTagsResult {
  serverId: string;
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
 * A Boot ISO (glossary Boot ISO, decision 049): an iPXE boot ISO swallow built for one provisioner
 * Integration. It takes an address from the site's own DHCP and chains to that provisioner's MAAS
 * rack (`chainUrl`), so a Server whose network DHCP is not the provisioner's still reaches it. Only
 * a Server of the same Integration may mount it as its Boot Media; the API enforces that.
 *
 * - `script` is what swallow rendered from its fixed template; it is display-only, never edited.
 * - `url` is where BMCs mount the ISO without credentials (and where an operator can download it);
 *   it is empty when the installation has no Boot Media base URL.
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
  script: string;
  ipxeVersion: string;
  sizeBytes: number;
  sha256: string;
  url: string;
  inUseBy: number;
  createdAt: string;
  createdBy?: string;
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
