import type { ServerGPU } from '@/domain/server/types'

/** Joins the provider-reported vendor and model without repeating an identical vendor prefix. */
function fullGPUIdentity(gpu: ServerGPU): string {
  const vendor = gpu.vendor.trim()
  const model = gpu.model.trim()
  if (!vendor) return model || 'Unknown GPU'
  if (!model) return vendor
  if (model.toLocaleLowerCase().startsWith(`${vendor.toLocaleLowerCase()} `)) return model
  return `${vendor} ${model}`
}

/**
 * Full, stable GPU Inventory profile used by tooltips, detail rows, and grouping. Every observed
 * profile remains available even though the list summary selects only the highest-priority one.
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

/**
 * One-line label for the most important profile within an already-selected GPU kind.
 *
 * Quantity decides between profiles of the same kind. Vendor and model make ties stable so the
 * same inventory cannot switch labels when provider response order changes.
 */
export function gpuInventoryCompactSummary(gpus: readonly ServerGPU[]): string {
  const primary = [...gpus].sort((left, right) => (
    right.count - left.count ||
    left.vendor.localeCompare(right.vendor) ||
    left.model.localeCompare(right.model)
  ))[0]
  return primary ? fullGPUIdentity(primary) : ''
}
