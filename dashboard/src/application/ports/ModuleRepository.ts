import type { ExtensionModule } from '@/domain/module/types'

/**
 * Application port for the Extension Modules compiled into the build (contract `modules.md`).
 * The list is fixed for the running build and needs the `admin` role; a module's `spec` is the
 * schema a generic settings form and details are rendered from.
 */
export interface ModuleRepository {
  /** Every module of the build, ordered by kind and then id. */
  listModules(): Promise<ExtensionModule[]>
}
