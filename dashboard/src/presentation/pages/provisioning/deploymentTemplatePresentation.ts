import type { Pool } from '@/domain/infrastructure/types'
import type { DeploymentTemplate } from '@/domain/provisioning/types'
import type { OSImage, ProvisionerImageCatalog } from '@/domain/site/types'

/** The live-catalog evidence available for the OS Image one Template references. */
export type TemplateImageResolution =
  | { status: 'available'; label: string; osFamilies: readonly string[]; architectures: readonly string[]; variants: readonly OSImage[] }
  | { status: 'catalog-unavailable'; message: string }
  | { status: 'image-unavailable' }

/** One Pool section in the recipe catalog; legacy records without a Pool are grouped last. */
export interface DeploymentTemplateGroup {
  key: string
  label: string
  templates: readonly DeploymentTemplate[]
}

/** Finds the live image variants without pretending that inconsistent architecture metadata agrees. */
export function resolveTemplateImage(
  template: DeploymentTemplate,
  catalogs: readonly ProvisionerImageCatalog[],
): TemplateImageResolution {
  const catalog = catalogs.find((item) => item.integrationId === template.integrationId)
  if (!catalog) return { status: 'catalog-unavailable', message: 'The provisioner catalog is not available in this scope.' }
  if (catalog.error) return { status: 'catalog-unavailable', message: catalog.error }
  const variants = catalog.images.filter((image) => image.id === template.imageId)
  if (variants.length === 0) return { status: 'image-unavailable' }
  const names = [...new Set(variants.map((image) => image.status.name.trim()).filter(Boolean))]
  const osFamilies = [...new Set(variants.map((image) => image.status.osSystem.trim()).filter(Boolean))].sort((a, b) => a.localeCompare(b))
  const architectures = [...new Set(variants.map((image) => image.architecture))].sort((a, b) => a.localeCompare(b))
  return {
    status: 'available',
    label: names.length === 1 ? names.at(0) ?? template.imageId : template.imageId,
    osFamilies,
    architectures,
    variants,
  }
}

/** Returns the provider display name carried by the catalog any signed-in user may read. */
export function templateIntegrationName(template: DeploymentTemplate, catalogs: readonly ProvisionerImageCatalog[]): string {
  return catalogs.find((item) => item.integrationId === template.integrationId)?.integrationName ?? template.integrationId
}

/** Groups visible Templates by Pool with deterministic, locale-aware ordering. */
export function groupDeploymentTemplates(
  templates: readonly DeploymentTemplate[],
  pools: readonly Pool[],
): DeploymentTemplateGroup[] {
  const poolNames = new Map(pools.map((pool) => [pool.id, pool.name]))
  const grouped = new Map<string, DeploymentTemplate[]>()
  for (const template of templates) {
    const key = template.poolId ?? ''
    grouped.set(key, [...(grouped.get(key) ?? []), template])
  }
  return [...grouped.entries()]
    .map(([key, items]) => ({
      key: key || 'no-pool',
      label: key ? poolNames.get(key) ?? key : 'No Pool',
      templates: [...items].sort((left, right) => left.name.localeCompare(right.name)),
    }))
    .sort((left, right) => {
      if (left.key === 'no-pool') return 1
      if (right.key === 'no-pool') return -1
      return left.label.localeCompare(right.label)
    })
}

/** Matches only facts the catalog or card can explain to a user. */
export function deploymentTemplateMatches(
  template: DeploymentTemplate,
  query: string,
  catalogs: readonly ProvisionerImageCatalog[],
  pools: readonly Pool[],
): boolean {
  const needle = query.trim().toLocaleLowerCase()
  if (!needle) return true
  const image = resolveTemplateImage(template, catalogs)
  const imageFacts = image.status === 'available'
    ? image.variants.flatMap((variant) => [variant.status.name, variant.status.osSystem, variant.status.release, ...variant.spec.tags])
    : []
  const pool = template.poolId ? pools.find((item) => item.id === template.poolId)?.name ?? template.poolId : 'No Pool'
  return [
    template.name,
    template.description,
    template.imageId,
    pool,
    templateIntegrationName(template, catalogs),
    ...template.usedBy.flatMap((use) => [use.platformName, use.group]),
    ...imageFacts,
  ].some((value) => value.toLocaleLowerCase().includes(needle))
}
