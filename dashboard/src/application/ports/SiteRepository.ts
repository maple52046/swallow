import type { Integration, IntegrationKind, OSImage, Site } from '@/domain/site/types'

/** Values accepted when registering a Swallow-owned Site boundary. */
export interface CreateSiteInput {
  name: string
  description: string
}

/** Mutable Site fields; identity and ownership references remain stable. */
export interface UpdateSiteInput {
  name: string
  description: string
}

/**
 * Values accepted when registering one external provider connection under a Site.
 * Credential is write-only and implementations must never return or log it.
 */
export interface CreateIntegrationInput {
  siteId: string
  kind: IntegrationKind
  providerKind: string
  name: string
  endpoint: string
  credential: string
  settings: Record<string, string>
  enabled: boolean
}

/** Mutable Integration fields; an existing Integration cannot move to another Site. */
export interface UpdateIntegrationInput {
  name: string
  endpoint: string
  settings: Record<string, string>
  enabled: boolean
}

/**
 * Admin registry port for the Site-to-Integration hierarchy.
 *
 * Credentials cross this boundary only on create or explicit replacement. Read methods
 * expose presence through `hasCredential`, never secret material.
 */
export interface SiteRepository {
  listSites(): Promise<Site[]>
  createSite(input: CreateSiteInput): Promise<Site>
  updateSite(id: string, input: UpdateSiteInput): Promise<Site>
  deleteSite(id: string): Promise<void>
  listIntegrations(filters?: { siteId?: string; kind?: IntegrationKind }): Promise<Integration[]>
  createIntegration(input: CreateIntegrationInput): Promise<Integration>
  updateIntegration(id: string, input: UpdateIntegrationInput): Promise<Integration>
  replaceIntegrationCredential(id: string, credential: string): Promise<void>
  deleteIntegration(id: string): Promise<void>
  /** Images differ per provisioner, so the integration must be named. */
  listOSImages(integrationId: string): Promise<OSImage[]>
}
