import type {
  InstallSoftwareInput,
  SoftwareAssignment,
  SoftwareCatalogEntry,
  SoftwareKind,
  SoftwareOperationReference,
  UninstallSoftwareInput,
} from '@/domain/software/types'

/**
 * Application port for Managed Software (decision 038): read the installable catalog and the
 * swallow-owned Software Assignment records, and install/uninstall a single software kind on
 * deployed Servers. Install and uninstall create durable Workflows; the reference returned is the
 * Workflow the caller navigates to for progress. Implementations map the `api-server` `software.md`
 * contract and must not infer behavior from provider internals.
 */
export interface SoftwareRepository {
  /** The fixed set of installable software kinds and their rules. Small and bounded. */
  listCatalog(): Promise<SoftwareCatalogEntry[]>
  /**
   * Software Assignments, optionally filtered by Server or kind. Absent records are omitted unless
   * `includeAbsent` is set.
   */
  listAssignments(filters?: {
    serverId?: string
    kind?: SoftwareKind
    includeAbsent?: boolean
  }): Promise<SoftwareAssignment[]>
  /** Install one software kind on one or more deployed Servers; resolves once accepted. */
  installSoftware(input: InstallSoftwareInput): Promise<SoftwareOperationReference>
  /** Uninstall one software kind from one or more Servers on which it is installed. */
  uninstallSoftware(input: UninstallSoftwareInput): Promise<SoftwareOperationReference>
}
