/**
 * Install methods of Managed Software kinds that offer more than one (root glossary: Managed
 * Software; decision 063). The `software.md` contract owns the values: the catalog entry's
 * `installMethods` lists them with the default first, and `containerRuntimes` lists the runtime
 * kinds the `container` method can run on. This module holds no transport or React detail.
 */
import type { SoftwareAssignment, SoftwareCatalogEntry, SoftwareKind } from './types'

/**
 * How a kind is installed: `container` runs the official image on the Server's container runtime;
 * `native` installs the official release binary or package with no runtime.
 */
export type SoftwareInstallMethod = 'container' | 'native'

/** The method a new install uses when the operator picks none: the catalog's first, if any. */
export function defaultInstallMethod(entry: SoftwareCatalogEntry): SoftwareInstallMethod | null {
  return entry.installMethods[0] ?? null
}

/**
 * Which container runtime a `container`-method install would use on the chosen targets.
 *
 * - `resolved`: every target has the same runtime `installed`, which the install will use.
 * - `missing`: these targets have none of the entry's runtimes `installed`.
 * - `mixed`: the targets run different runtimes, so one install cannot serve them all.
 *
 * It mirrors the API's standalone rule for immediate feedback; the API re-checks and answers 409.
 */
export type ContainerRuntimeResolution =
  | { status: 'resolved'; runtime: SoftwareKind }
  | { status: 'missing'; serverIds: string[] }
  | { status: 'mixed'; runtimes: SoftwareKind[] }

/** Resolves the container runtime for a standalone install on `serverIds` (see the type above). */
export function resolveContainerRuntime(
  entry: SoftwareCatalogEntry,
  serverIds: readonly string[],
  assignments: readonly SoftwareAssignment[],
): ContainerRuntimeResolution {
  const missing: string[] = []
  const runtimes = new Set<SoftwareKind>()
  for (const serverId of serverIds) {
    const installed = entry.containerRuntimes.find((runtime) => assignments.some((assignment) =>
      assignment.serverId === serverId && assignment.kind === runtime && assignment.state === 'installed'
    ))
    if (installed) runtimes.add(installed)
    else missing.push(serverId)
  }
  if (missing.length > 0) return { status: 'missing', serverIds: missing }
  if (runtimes.size > 1) return { status: 'mixed', runtimes: [...runtimes] }
  const [runtime] = runtimes
  return runtime ? { status: 'resolved', runtime } : { status: 'missing', serverIds: [] }
}
