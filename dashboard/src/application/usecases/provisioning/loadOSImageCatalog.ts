import type { ProvisioningRepository } from '@/application/ports/ProvisioningRepository'
import type { Integration, OSImage } from '@/domain/site/types'

export interface OSImageCatalogRow extends OSImage {
  integrationId: string
  integrationName: string
  siteId: string
  refreshedAt: string
}

export interface OSImageCatalogFailure {
  integrationId: string
  integrationName: string
  message: string
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
