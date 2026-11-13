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

/**
 * An Integration's role (sites-integrations.md). `repository` is a Site's Local Repository (provider
 * kind `pulp`, at most one per Site); `platform` Integrations are created only by deploying a Platform.
 */
export type IntegrationKind = 'provisioner' | 'metrics' | 'platform' | 'repository'

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
  /**
   * Whether the Integration also stores an operator credential: a Kubernetes Platform's deployment
   * records it for the cluster explorer's writes (sites-integrations.md). Like `hasCredential` it
   * reports presence only. `false` for every other Integration, including a Kubernetes Platform's
   * that still has the single credential of a deployment before 2026-11-05.
   */
  hasOperatorCredential: boolean
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
 * The `repository` Integration setting naming where the Site's Servers read the Local Repository's
 * content, which may differ from the `endpoint` swallow calls the provider's API at (contract
 * sites-integrations.md, Repository settings). Without a valid one the worker cannot converge the
 * Local Repository and browsing its content is refused.
 */
export const CONTENT_ORIGIN_SETTING = 'contentOrigin'

// A scheme, a host (a name, an IPv4 address, or a bracketed IPv6 address), an explicit port, and
// no path, query, fragment, or user info; a trailing slash is the empty path.
const CONTENT_ORIGIN_PATTERN = /^https?:\/\/(\[[0-9A-Fa-f:.]+\]|[^\s/?#@:[\]]+):\d{1,5}\/?$/

/**
 * Whether `value` has the shape the API requires of `contentOrigin`. Mirrored only so a form can
 * flag a typo before saving; the API remains the validator of record.
 */
export function isValidContentOrigin(value: string): boolean {
  return CONTENT_ORIGIN_PATTERN.test(value)
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
  /** swallow-owned labels for organizing and searching images; no provider counterpart. */
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
   * for an uploaded custom image until a user sets one.
   */
  defaultUser?: string
  /** The provider's own values, shown when reverting a label. */
  providerName: string
  providerOsSystem: string
  providerRelease: string
  /** Provider-reported bytes for the current complete artifact; absent when unavailable. */
  sizeBytes?: number
  /**
   * Deploy targets ("disk"/"ram") a swallow verification has proven this image can deploy in.
   * Always present (possibly empty). Empty means the image has not been verified for any target;
   * custom (uploaded) images must be verified for a target before a normal deploy in that target
   * is allowed, while synced provider images are trusted and never require verification.
   */
  verifiedDeployTargets: string[]
  /**
   * Deploy targets whose most recent swallow verification run failed. Always present (possibly
   * empty). A target here was proven not to deploy in that mode, as opposed to never attempted — it
   * lets the UI show a failed verification distinctly from an unverified one. A target is in
   * verifiedDeployTargets or failedDeployTargets but never both (recording one outcome clears the
   * other); a failed target is still gate-blocked exactly like an unverified one.
   */
  failedDeployTargets: string[]
}

/**
 * One provisioner Integration's OS Image catalog as the Site-wide read returns it
 * (provisioning.md "Image catalogs of a Site"): the Integration's id, name, and Site only — any user
 * may read it, while the Integration itself is an admin read — and its images, or why they could not
 * be read (`error`, a sentence for a person; `null` when they were, `images` then empty).
 */
export interface ProvisionerImageCatalog {
  integrationId: string
  integrationName: string
  siteId: string
  images: OSImage[]
  error: string | null
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
