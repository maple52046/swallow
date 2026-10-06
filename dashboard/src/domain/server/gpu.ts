import type { Server, ServerGPU } from './types'

/** Closed GPU Inventory classification supplied by the Server API. */
export type GPUKind = ServerGPU['kind']

/**
 * Selects one authoritative GPU Inventory kind without inferring from vendor display strings.
 * The backend owns classification; consumers use this helper so list, detail, and monitoring
 * interpret the wire contract identically.
 */
export function serverGPUsByKind(server: Server, kind: GPUKind): ServerGPU[] {
  return server.gpus.filter((gpu) => gpu.kind === kind)
}

/**
 * Chooses the inventory represented by the compact Server List: compute accelerators when any
 * exist, otherwise display controllers. An empty Server stays CPU-only.
 */
export function preferredServerGPUs(server: Server): ServerGPU[] {
  const compute = serverGPUsByKind(server, 'compute')
  return compute.length > 0 ? compute : serverGPUsByKind(server, 'display')
}

/** Physical device count across already-selected GPU Inventory groups. */
export function gpuInventoryCount(gpus: readonly ServerGPU[]): number {
  return gpus.reduce((total, gpu) => total + gpu.count, 0)
}
