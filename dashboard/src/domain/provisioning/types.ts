import type { ProvisioningActionResult } from "@/domain/server/types";

/**
 * Swallow-owned writable deployment addressing intent. `automatic` asks the provisioner to
 * assign an address (realized by provider auto-assign, a stable recorded IP — see ADR 018),
 * `static` uses a caller-chosen address. The backend still accepts the deprecated `dhcp`
 * alias for one release, but the dashboard always sends the canonical values.
 */
export type DeploymentNetworkMode = "automatic" | "static";

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
  ephemeral: boolean;
  network?: DeploymentNetworkSettings;
  userData?: string;
}

/** Templates never move integration and cloud-init has dedicated write-only methods. */
export interface UpdateDeploymentTemplateInput {
  name?: string;
  description?: string;
  imageId?: string;
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

/** A bounded batch release intent executed by one durable Operation. */
export interface ReleaseServersOperationInput {
  serverIds: string[];
  erase?: boolean;
  secureErase?: boolean;
  quickErase?: boolean;
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
