/**
 * Managed Software domain types for the dashboard.
 *
 * These mirror the swallow-owned Software Deployment model (root glossary: Managed Software and
 * Software Assignment; decision 038): the deploy target is a single piece of host software and its
 * variants, deliberately distinct from a Platform. The API contract that owns these shapes is
 * `api-server` `software.md`; this module holds no transport or React detail.
 */

/** A single installable software. Values are domain language owned by swallow's glossary. */
export type SoftwareKind = 'docker-ce' | 'podman' | 'nfs'

/** A variant of one software. NFS is the only first-cut kind with roles. */
export type SoftwareRole = 'server' | 'client'

/**
 * The lifecycle of one Software Assignment. `absent` means swallow no longer considers the software
 * present (for example the Server left `deployed`); it is not a Server status axis.
 */
export type SoftwareAssignmentState =
  | 'pending'
  | 'installed'
  | 'failed'
  | 'uninstalling'
  | 'absent'

/** One installable software kind and the rules the install form must honor. */
export interface SoftwareCatalogEntry {
  kind: SoftwareKind
  label: string
  /** Variant roles; empty for a role-less kind (a container runtime). */
  roles: SoftwareRole[]
  /** Kinds that cannot coexist with this one on one Server. */
  mutuallyExclusiveWith: SoftwareKind[]
  /** Whether installing this kind is refused on a Kubernetes Platform member. */
  refusedForKubernetesMembers: boolean
  /** Kind-specific spec keys the API accepts, for the install form. */
  specFields: string[]
}

/**
 * A swallow-owned record of one Managed Software kind on one Server, keyed by `(serverId, kind)`.
 * It is the single source of truth for what software swallow installed where; it is not Platform
 * membership and not a live package scan.
 */
export interface SoftwareAssignment {
  serverId: string
  kind: SoftwareKind
  roles: SoftwareRole[]
  spec: Record<string, unknown> | null
  state: SoftwareAssignmentState
  lastWorkflowId: string
  /** ISO 8601, or null when never successfully applied. */
  lastAppliedAt: string | null
  createdAt: string
  updatedAt: string
}

/** One install target: a Server and the roles it should take for the software kind. */
export interface InstallSoftwareTarget {
  serverId: string
  roles: SoftwareRole[]
}

/** Intent to install one software kind on one or more deployed Servers. */
export interface InstallSoftwareInput {
  kind: SoftwareKind
  assignments: InstallSoftwareTarget[]
  /** Kind-specific spec (NFS export/mount, runtime version); optional per field. */
  spec?: Record<string, unknown>
}

/** Intent to uninstall one software kind from one or more Servers. */
export interface UninstallSoftwareInput {
  kind: SoftwareKind
  serverIds: string[]
}

/** The accepted-Workflow reference returned by an install/uninstall command. */
export interface SoftwareOperationReference {
  operationId: string
}
