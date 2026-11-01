import type { ModuleRepository } from '@/application/ports/ModuleRepository'
import type { ExtensionModule } from '@/domain/module/types'
import { apiRequest } from './client'

/**
 * The `{ items: [...] }` envelope the module list returns. Exported only so
 * `contractCompatibility.ts` can prove the response reads as it.
 */
export interface ModuleList {
  items: ExtensionModule[]
}

/**
 * Maps the Active Modules contract (`GET /api/v1/modules`, `modules.md`) onto the ModuleRepository
 * port. The contract's JSON already matches the domain shape. The list cannot change while the
 * build runs, so one successful read is shared; a failed read is not kept, so the next call retries.
 */
export class ApiModuleRepository implements ModuleRepository {
  private modules: Promise<ExtensionModule[]> | null = null

  listModules(): Promise<ExtensionModule[]> {
    if (!this.modules) {
      const request = apiRequest<ModuleList>('/api/v1/modules').then((response) => response.items)
      request.catch(() => {
        if (this.modules === request) this.modules = null
      })
      this.modules = request
    }
    return this.modules
  }
}
