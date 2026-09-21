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
 * An operating system a provisioner can currently deploy.
 *
 * `name`, `osSystem`, and `release` are the effective display values: the swallow overlay value
 * when one is set, otherwise the provider value. Each `provider*` field always carries the
 * provider's own value (shown when reverting an override), and each `custom*` field is present
 * only when that field has a swallow override. `id` and `architecture` identify the deployable
 * artifact and are never overridable. See ADR 025 (Provider Data Overlay).
 */
export interface OSImage {
  /** Pass this back as `distroSeries` when deploying. */
  id: string
  name: string
  providerName: string
  customName?: string
  osSystem: string
  providerOsSystem: string
  customOsSystem?: string
  release: string
  providerRelease: string
  customRelease?: string
  /** Swallow-owned labels for organizing and searching images; no provider counterpart. */
  tags: string[]
  architecture: string
  /** Provider-reported bytes for the current complete artifact; absent when unavailable. */
  sizeBytes?: number
  /**
   * Deploy targets ("disk"/"ram") a Swallow verification has proven this image can deploy in.
   * Always present (possibly empty). Empty means the image has not been verified for any target;
   * custom (uploaded) images must be verified for a target before a normal deploy in that target
   * is allowed, while synced provider images are trusted and never require verification.
   */
  verifiedDeployTargets: string[]
}
