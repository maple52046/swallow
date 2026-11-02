import type { ProvisioningRepository } from '@/application/ports/ProvisioningRepository'
import type { RequestFailure } from '@/application/ports/RequestFailure'
import type { Integration, OSImage } from '@/domain/site/types'

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
  /** The request ID of the failed catalog read, when the API answered with one. */
  requestId?: string
}

export interface OSImageCatalog {
  images: OSImageCatalogRow[]
  failures: OSImageCatalogFailure[]
}

/** Loads live provider catalogs with at most two requests in flight. */
export async function loadOSImageCatalog(
  repository: ProvisioningRepository,
  integrations: Integration[],
): Promise<OSImageCatalog> {
  const images: OSImageCatalogRow[] = []
  const failures: OSImageCatalogFailure[] = []
  let cursor = 0

  const worker = async () => {
    for (;;) {
      const index = cursor
      cursor += 1
      if (index >= integrations.length) return
      const integration = integrations[index]
      try {
        const providerImages = await repository.listOSImages(integration.id)
        const refreshedAt = new Date().toISOString()
        for (const image of providerImages) {
          images.push({
            ...image,
            integrationId: integration.id,
            integrationName: integration.name,
            siteId: integration.siteId,
            refreshedAt,
          })
        }
      } catch (error) {
        failures.push({
          integrationId: integration.id,
          integrationName: integration.name,
          message: error instanceof Error ? error.message : 'Provider unavailable.',
          // Structural read through the port: the adapter's error class is not visible here.
          requestId: error instanceof Error ? (error as Partial<RequestFailure>).requestId : undefined,
        })
      }
    }
  }

  await Promise.all(Array.from(
    { length: Math.min(2, integrations.length) },
    () => worker(),
  ))
  return { images, failures }
}
