/**
 * Sites and integrations are the only part of the world swallow defines rather than
 * observes. See docs/development/glossaries/terms/site.md.
 */

export interface Site {
  id: string
  name: string
  description: string
  createdAt: string
  updatedAt: string
}

export type IntegrationKind = 'provisioner' | 'metrics' | 'platform'

/**
 * Freshness of whatever an integration feeds. Part of the API, not an implementation
 * detail: a reader must be able to tell "14 minutes old" from "current".
 *
 * A failed refresh keeps `lastSucceededAt`, so both facts are available: that the last
 * attempt failed, and how old the data is.
 */
export interface SyncState {
  lastStartedAt: string | null
  lastSucceededAt: string | null
  lastError: string | null
}

export interface Integration {
  id: string
  siteId: string
  kind: IntegrationKind
  /** The product implementing the kind, e.g. "maas". */
  providerKind: string
  name: string
  endpoint: string
  /** A paused integration keeps its configuration but is not used. */
  enabled: boolean
  settings: Record<string, string>
  /**
   * Whether a credential is stored. The credential itself is never returned by any
   * endpoint, in any form.
   */
  hasCredential: boolean
  sync: SyncState
  createdAt: string
  updatedAt: string
}

/**
 * The provisioner setting that turns automatic hardware inspection of newly enrolled Servers off
 * (contract sites-integrations.md, decision 053). Only the value `"false"` turns it off; absent,
 * which is every Integration created before the setting existed, means on.
 */
export const AUTO_INSPECT_SETTING = 'autoInspect'

/** Whether a provisioner Integration inspects newly enrolled Servers automatically. */
export function autoInspectEnabled(integration: Pick<Integration, 'settings'>): boolean {
  return integration.settings[AUTO_INSPECT_SETTING] !== 'false'
}

/**
 * The metadata swallow keeps for one OS image: display labels, tags, and default user (decision
 * 071). It is desired state that needs no provider realization, so it has no generation or
 * conditions. An empty label means "use the provider value"; an empty `defaultUser` means the
 * built-in default (if any) applies. The backend replaces the whole metadata on every write, so a
 * caller editing one field must send the current values of the others.
 */
export interface OSImageMetadata {
  name: string
  osSystem: string
  release: string
  /** Swallow-owned labels for organizing and searching images; no provider counterpart. */
  tags: string[]
  defaultUser: string
}

/** What the catalog observed for one OS image, with the metadata merged over the provider values. */
export interface OSImageStatus {
  /** The effective display values: the metadata value when set, otherwise the provider value. */
  name: string
  osSystem: string
  release: string
  /**
   * The effective default login user swallow automation uses on Servers deployed with this image
   * (decision 039): the metadata `defaultUser` when set, otherwise swallow's built-in for the
   * provider OS family (e.g. `ubuntu` → `ubuntu`). Absent when neither applies, which is typical
   * for an uploaded custom image until an operator sets one.
   */
  defaultUser?: string
  /** The provider's own values, shown when reverting a label. */
  providerName: string
  providerOsSystem: string
  providerRelease: string
  /** Provider-reported bytes for the current complete artifact; absent when unavailable. */
  sizeBytes?: number
  /**
   * Deploy targets ("disk"/"ram") a Swallow verification has proven this image can deploy in.
   * Always present (possibly empty). Empty means the image has not been verified for any target;
   * custom (uploaded) images must be verified for a target before a normal deploy in that target
   * is allowed, while synced provider images are trusted and never require verification.
   */
  verifiedDeployTargets: string[]
  /**
   * Deploy targets whose most recent Swallow verification run failed. Always present (possibly
   * empty). A target here was proven not to deploy in that mode, as opposed to never attempted — it
   * lets the UI show a failed verification distinctly from an unverified one. A target is in
   * verifiedDeployTargets or failedDeployTargets but never both (recording one outcome clears the
   * other); a failed target is still gate-blocked exactly like an unverified one.
   */
  failedDeployTargets: string[]
}

/**
 * An operating system a provisioner can currently deploy, in the shared spec/status shape.
 * `id` and `architecture` identify the provider-owned deployable artifact and are never
 * overridable.
 */
export interface OSImage {
  /** Pass this back as `distroSeries` when deploying. */
  id: string
  architecture: string
  spec: OSImageMetadata
  status: OSImageStatus
}

/**
 * The POSIX login-name shape the backend accepts for an OS Image default user (provisioning.md):
 * lowercase letter or underscore first, then up to 31 lowercase letters, digits, `_` or `-`.
 * Mirrored here only so forms can flag a typo before a request (or a multi-gigabyte upload) is
 * sent; the backend remains the validator of record.
 */
const DEFAULT_USER_PATTERN = /^[a-z_][a-z0-9_-]{0,31}$/

/** Whether `value` is an acceptable OS Image default user. An empty value is not; callers treat blank as "not set". */
export function isValidDefaultUser(value: string): boolean {
  return DEFAULT_USER_PATTERN.test(value)
}
