/**
 * Sites and integrations are the only part of the world gdcm defines rather than
 * observes. See docs/glossaries/site.md.
 */

export interface Site {
  id: string
  name: string
  description: string
  createdAt: string
  updatedAt: string
}

export type IntegrationKind = 'provisioner' | 'automation' | 'metrics' | 'cluster'

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

/** An operating system a provisioner can currently deploy. */
export interface OSImage {
  /** Pass this back as `distroSeries` when deploying. */
  id: string
  name: string
  osSystem: string
  release: string
  architecture: string
}
