import type { ServerGPU } from '@/domain/server/types'

/**
 * Full, stable GPU Inventory profile used by tooltips, detail rows, and grouping. Every observed
 * profile remains available even though the list summary shows only the brands and the count.
 */
export function gpuInventoryProfile(gpus: readonly ServerGPU[]): string {
  return [...gpus]
    .sort((left, right) => (
      left.vendor.localeCompare(right.vendor) ||
      left.model.localeCompare(right.model) ||
      left.count - right.count
    ))
    .map((gpu) => `${gpu.count} × ${[gpu.vendor, gpu.model].filter(Boolean).join(' ') || 'Unknown GPU'}`)
    .join(' + ')
}
