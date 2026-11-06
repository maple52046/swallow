import type { Server, ServerGPU } from './types'

/** Closed GPU Inventory classification supplied by the Server API. */
export type GPUKind = ServerGPU['kind']

/**
 * Selects one authoritative GPU Inventory kind without inferring from vendor display strings.
 * The backend owns classification; consumers use this helper so the list and the detail views
 * interpret the wire contract identically.
 */
export function serverGPUsByKind(server: Server, kind: GPUKind): ServerGPU[] {
  return server.status.gpus.filter((gpu) => gpu.kind === kind)
}

/** Physical device count across already-selected GPU Inventory groups. */
export function gpuInventoryCount(gpus: readonly ServerGPU[]): number {
  return gpus.reduce((total, gpu) => total + gpu.count, 0)
}
