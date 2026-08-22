import { ApiServerRepository } from '@/infrastructure/api/ApiServerRepository'
import { ApiSiteRepository } from '@/infrastructure/api/ApiSiteRepository'
import type { ServerRepository } from '@/application/ports/ServerRepository'
import type { SiteRepository } from '@/application/ports/SiteRepository'

/**
 * Repositories are exposed directly rather than behind pass-through use cases.
 *
 * The previous container wrapped every repository call in a use case class that only
 * forwarded the arguments. For a read-mostly client that is ceremony, not architecture:
 * the boundary that matters is the port interface, which is still here. A use case
 * earns its own type when it has logic of its own.
 *
 * Every binding is a real HTTP implementation. There are no mock repositories, and
 * there is no flag to switch to them.
 */
export interface AppContainer {
  servers: ServerRepository
  sites: SiteRepository
}

export function createContainer(): AppContainer {
  return {
    servers: new ApiServerRepository(),
    sites: new ApiSiteRepository(),
  }
}
