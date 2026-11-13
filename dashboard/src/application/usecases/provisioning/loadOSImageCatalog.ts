import type { ProvisioningRepository } from '@/application/ports/ProvisioningRepository'
import type { OSImage } from '@/domain/site/types'

export interface OSImageCatalogRow extends OSImage {
  integrationId: string
  integrationName: string
  siteId: string
  refreshedAt: string
}

/**
 * A provisioner Integration whose live image catalog could not be read. The other Integrations'
 * images still load, so the catalog shows this beside them instead of failing as a whole.
 */
export interface OSImageCatalogFailure {
  integrationId: string
  integrationName: string
  message: string
  /**
   * The request ID of the failed catalog read, when the API answered with one. The Site-wide read
   * reports a provisioner's failure inside a successful response, so it carries none.
   */
  requestId?: string
}

/**
 * Every provisioner's images of the scope as catalog rows, the provisioners whose catalogs could
 * not be read, and how many provisioners the scope has (zero means none is connected, which is
 * different from provisioners that list no image).
 */
export interface OSImageCatalog {
  images: OSImageCatalogRow[]
  failures: OSImageCatalogFailure[]
  provisionerCount: number
}

/**
 * Loads the OS Image catalog of every provisioner of `siteId` (of every Site when omitted) through
 * the one Site-wide reference read, so every user sees every image whatever Servers it can see.
 * Each image becomes a row that carries its provisioner's id, name, and Site; a provisioner whose
 * catalog could not be read becomes a failure with the API's message. A failed request rejects.
 */
export async function loadOSImageCatalog(
  repository: ProvisioningRepository,
  siteId?: string,
): Promise<OSImageCatalog> {
  const catalogs = await repository.listOSImageCatalogs(siteId)
  const refreshedAt = new Date().toISOString()
  const images: OSImageCatalogRow[] = []
  const failures: OSImageCatalogFailure[] = []
  for (const catalog of catalogs) {
    if (catalog.error !== null) {
      failures.push({ integrationId: catalog.integrationId, integrationName: catalog.integrationName, message: catalog.error })
      continue
    }
    for (const image of catalog.images) {
      images.push({
        ...image,
        integrationId: catalog.integrationId,
        integrationName: catalog.integrationName,
        siteId: catalog.siteId,
        refreshedAt,
      })
    }
  }
  return { images, failures, provisionerCount: catalogs.length }
}
