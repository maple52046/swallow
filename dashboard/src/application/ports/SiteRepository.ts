import type { Integration, IntegrationKind, OSImage, Site } from '@/domain/site/types'

/**
 * Read-only for now. Registering a site or an integration involves a credential and is
 * done by an operator through the API; adding those forms to the dashboard is a
 * separate piece of work from showing what is registered.
 */
export interface SiteRepository {
  listSites(): Promise<Site[]>
  listIntegrations(filters?: { siteId?: string; kind?: IntegrationKind }): Promise<Integration[]>
  /** Images differ per provisioner, so the integration must be named. */
  listOSImages(integrationId: string): Promise<OSImage[]>
}
